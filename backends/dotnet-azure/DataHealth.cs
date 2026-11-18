using System.Globalization;
using System.Text.Json.Nodes;

namespace DashboardApi;

public static class DataHealth
{
    public const long FreshWithinSeconds = 36 * 60 * 60;

    public static async Task<JsonObject> Build(IStore store, string backend, string lang, string runtime, string color, string version, string dataVersion, DateTimeOffset now)
    {
        var warnings = new JsonArray();
        IReadOnlyList<ObjectMeta> listed;
        try
        {
            listed = await store.ListObjects("serving/");
        }
        catch (Exception ex)
        {
            warnings.Add($"object store unavailable: {ex.Message}");
            listed = Array.Empty<ObjectMeta>();
        }
        var counts = new Dictionary<string, int> { ["chart"] = 0, ["stock"] = 0, ["funds"] = 0 };
        var modified = new Dictionary<string, DateTimeOffset?>();
        var tickers = new SortedSet<string>(StringComparer.Ordinal);
        DateTimeOffset? newest = null;
        DateTimeOffset? oldest = null;
        foreach (var item in listed)
        {
            modified[item.Key] = item.Modified;
            var kind = Prefix(item.Key);
            if (kind is not null)
            {
                counts[kind]++;
                var name = item.Key[(item.Key.LastIndexOf('/') + 1)..];
                if (name.EndsWith(".json", StringComparison.Ordinal))
                    tickers.Add(name[..^5]);
            }
            if (item.Modified is { } stamp)
            {
                if (newest is null || stamp > newest) newest = stamp;
                if (oldest is null || stamp < oldest) oldest = stamp;
            }
        }
        var (dashboard, dashStale, _) = await ReadFile(store, now, "serving/dashboard.json", Modified(modified, "serving/dashboard.json"), warnings, false);
        var (status, statusStale, statusBody) = await ReadFile(store, now, "serving/status.json", Modified(modified, "serving/status.json"), warnings, true);
        var freshness = !dashboard["present"]!.GetValue<bool>() || !status["present"]!.GetValue<bool>()
            ? "missing"
            : dashStale || statusStale ? "stale" : "fresh";
        var tickerArray = new JsonArray();
        foreach (var ticker in tickers)
            tickerArray.Add(ticker);
        return new JsonObject
        {
            ["backend"] = backend,
            ["lang"] = lang,
            ["runtime"] = runtime,
            ["color"] = color,
            ["version"] = version,
            ["data_version"] = dataVersion,
            ["cloud"] = CloudOf(backend),
            ["checked_at"] = Iso(now),
            ["freshness"] = freshness,
            ["fresh_within_seconds"] = FreshWithinSeconds,
            ["counts"] = new JsonObject { ["chart"] = counts["chart"], ["stock"] = counts["stock"], ["funds"] = counts["funds"] },
            ["files"] = new JsonObject { ["dashboard"] = dashboard, ["status"] = status },
            ["tickers"] = tickerArray,
            ["newest_at"] = newest is null ? null : Iso(newest.Value),
            ["oldest_at"] = oldest is null ? null : Iso(oldest.Value),
            ["collectors"] = Collectors(statusBody, now, warnings),
            ["import"] = await ReadImport(store, now, warnings),
            ["warnings"] = warnings,
        };
    }

    static async Task<(JsonObject File, bool Stale, byte[]? Body)> ReadFile(IStore store, DateTimeOffset now, string key, DateTimeOffset? modified, JsonArray warnings, bool keepBody)
    {
        byte[]? body;
        try
        {
            body = await store.GetObject(key);
        }
        catch (Exception ex)
        {
            warnings.Add($"could not read {key}: {ex.Message}");
            return (Empty(key), false, null);
        }
        if (body is null)
        {
            warnings.Add($"{key} is missing");
            return (Empty(key), false, null);
        }
        string? generated = null;
        try
        {
            if (JsonNode.Parse(body) is JsonObject doc && doc["generated_at"] is JsonValue value && value.TryGetValue<string>(out var text) && text.Length > 0)
                generated = text;
        }
        catch (System.Text.Json.JsonException)
        {
        }
        DateTimeOffset? when = Parse(generated) ?? modified;
        var file = new JsonObject
        {
            ["key"] = key,
            ["present"] = true,
            ["generated_at"] = generated,
            ["last_modified"] = modified is null ? null : Iso(modified.Value),
            ["age_seconds"] = null,
        };
        var stale = true;
        if (when is null)
            warnings.Add($"{key} has no timestamp");
        else
        {
            var age = Age(now, when.Value);
            file["age_seconds"] = age;
            stale = age > FreshWithinSeconds;
            if (stale)
                warnings.Add($"{key} is stale");
        }
        return (file, stale, keepBody ? body : null);
    }

    static JsonObject Empty(string key) => new()
    {
        ["key"] = key,
        ["present"] = false,
        ["generated_at"] = null,
        ["last_modified"] = null,
        ["age_seconds"] = null,
    };

    static JsonArray Collectors(byte[]? body, DateTimeOffset now, JsonArray warnings)
    {
        var rows = new JsonArray();
        if (body is null || body.Length == 0) return rows;
        JsonObject? doc;
        try { doc = JsonNode.Parse(body) as JsonObject; }
        catch (System.Text.Json.JsonException) { return rows; }
        if (doc?["jobs"] is not JsonArray jobs) return rows;
        foreach (var node in jobs)
        {
            if (node is not JsonObject job) continue;
            var status = job["status"]?.GetValue<string>() ?? "";
            var lastRun = job["last_run"]?.GetValue<string>() ?? "";
            var name = job["name"]?.GetValue<string>() ?? "";
            var when = Parse(lastRun);
            rows.Add(new JsonObject
            {
                ["name"] = name,
                ["status"] = status,
                ["last_run"] = lastRun,
                ["age_seconds"] = when is null ? null : Age(now, when.Value),
            });
            var normalized = status.ToLowerInvariant();
            if (normalized != "ok" && normalized != "success" && normalized != "ready")
                warnings.Add($"collector {name} status is {status}");
        }
        return rows;
    }

    static async Task<JsonNode?> ReadImport(IStore store, DateTimeOffset now, JsonArray warnings)
    {
        byte[]? body;
        try { body = await store.GetObject("serving/_import.json"); }
        catch (Exception ex)
        {
            warnings.Add($"could not read serving/_import.json: {ex.Message}");
            return null;
        }
        if (body is null) return null;
        JsonObject? doc;
        try { doc = JsonNode.Parse(body) as JsonObject; }
        catch (System.Text.Json.JsonException)
        {
            warnings.Add("serving/_import.json is not valid JSON");
            return null;
        }
        if (doc is null)
        {
            warnings.Add("serving/_import.json is not valid JSON");
            return null;
        }
        var source = doc["source"]?.GetValue<string>() ?? "";
        var origin = doc["origin"]?.GetValue<string>() ?? "";
        var at = doc["at"]?.GetValue<string>() ?? "";
        if (source.Length == 0)
            warnings.Add("serving/_import.json has no source");
        var when = Parse(at);
        return new JsonObject
        {
            ["source"] = source,
            ["origin"] = origin,
            ["at"] = at,
            ["age_seconds"] = when is null ? null : Age(now, when.Value),
        };
    }

    static DateTimeOffset? Modified(Dictionary<string, DateTimeOffset?> modified, string key) =>
        modified.TryGetValue(key, out var when) ? when : null;

    static string? Prefix(string key)
    {
        foreach (var (name, prefix) in new[] { ("chart", "serving/chart/"), ("stock", "serving/stock/"), ("funds", "serving/funds/") })
        {
            if (!key.StartsWith(prefix, StringComparison.Ordinal) || !key.EndsWith(".json", StringComparison.Ordinal))
                continue;
            var rest = key[prefix.Length..];
            if (rest.Length > 0 && !rest.Contains('/'))
                return name;
        }
        return null;
    }

    static string CloudOf(string backend) =>
        backend.StartsWith("gcp", StringComparison.Ordinal) ? "gcp" :
        backend.StartsWith("aws", StringComparison.Ordinal) ? "aws" :
        backend.StartsWith("azure", StringComparison.Ordinal) ? "azure" : "unknown";

    static DateTimeOffset? Parse(string? value)
    {
        if (string.IsNullOrEmpty(value)) return null;
        if (DateTimeOffset.TryParse(value, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind, out var parsed))
            return parsed;
        return null;
    }

    static long Age(DateTimeOffset now, DateTimeOffset ts)
    {
        var seconds = (long)(now - ts).TotalSeconds;
        return seconds < 0 ? 0 : seconds;
    }

    static string Iso(DateTimeOffset value) => value.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ", CultureInfo.InvariantCulture);
}
