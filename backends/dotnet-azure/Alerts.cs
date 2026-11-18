using System.Collections.Concurrent;
using System.Text.Json.Nodes;

namespace DashboardApi;

public static class Alerts
{
    static readonly ConcurrentDictionary<string, JsonObject> Items = new();
    static readonly ConcurrentQueue<string> Order = new();

    public static async Task<JsonObject> Page(IStore store, string backend, string lang, string runtime, string color, string version, string dataVersion, DateTimeOffset now)
    {
        var report = await DataHealth.Build(store, backend, lang, runtime, color, version, dataVersion, now);
        var firing = Firing(report, backend);
        var stamp = now.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ss.fffffffZ");
        var seen = new HashSet<string>();
        foreach (var alert in firing)
        {
            var id = alert["id"]!.GetValue<string>();
            seen.Add(id);
            if (!Items.TryGetValue(id, out var prev))
            {
                alert["first_seen"] = stamp;
                alert["last_seen"] = stamp;
                alert["state"] = "firing";
                Items[id] = alert;
                Order.Enqueue(id);
            }
            else
            {
                prev["severity"] = alert["severity"]!.GetValue<string>();
                prev["message"] = alert["message"]!.GetValue<string>();
                prev["last_seen"] = stamp;
                prev["state"] = "firing";
            }
        }
        foreach (var pair in Items)
        {
            if (seen.Contains(pair.Key) || pair.Value["state"]?.GetValue<string>() == "resolved") continue;
            pair.Value["state"] = "resolved";
            pair.Value["last_seen"] = stamp;
        }
        while (Order.Count > 200 && Order.TryDequeue(out var old))
            Items.TryRemove(old, out _);
        var alerts = new JsonArray();
        var count = 0;
        foreach (var id in Order.ToArray())
        {
            if (!Items.TryGetValue(id, out var alert)) continue;
            alerts.Add(alert.DeepClone());
            if (alert["state"]?.GetValue<string>() == "firing") count++;
        }
        return new JsonObject { ["alerts"] = alerts, ["firing"] = count };
    }

    static List<JsonObject> Firing(JsonObject report, string backend)
    {
        var rows = new List<JsonObject>();
        var freshness = report["freshness"]?.GetValue<string>();
        if (freshness == "stale")
            rows.Add(Row($"freshness:{backend}", "warning", "data-freshness", backend, $"{backend} serving data is stale"));
        else if (freshness == "missing")
            rows.Add(Row($"freshness:{backend}", "warning", "data-freshness", backend, $"{backend} serving data is missing"));
        if (report["collectors"] is JsonArray jobs)
        {
            foreach (var node in jobs)
            {
                if (node is not JsonObject job) continue;
                var name = job["name"]?.GetValue<string>() ?? "";
                var status = job["status"]?.GetValue<string>() ?? "";
                var normalized = status.ToLowerInvariant();
                if (normalized != "ok" && normalized != "success" && normalized != "ready")
                    rows.Add(Row($"collector:{backend}:{name}", "warning", "collector", backend, $"collector {name} status is {status}"));
                var limit = name.Contains("hourly", StringComparison.OrdinalIgnoreCase) ? 2 * 60 * 60 : DataHealth.FreshWithinSeconds;
                var age = job["age_seconds"] is JsonValue value && value.TryGetValue<long>(out var parsed) ? parsed : (long?)null;
                if (age is null || age > limit)
                    rows.Add(Row($"collector-age:{backend}:{name}", "warning", "collector", backend, $"collector {name} last run is older than its interval"));
            }
        }
        return rows;
    }

    static JsonObject Row(string id, string severity, string source, string backend, string message) => new()
    {
        ["id"] = id,
        ["severity"] = severity,
        ["source"] = source,
        ["backend"] = backend,
        ["message"] = message,
    };
}
