using System.Text.Json;
using System.Text.Json.Nodes;

namespace DashboardApi;

public sealed class Hlc
{
    public long WallMs { get; set; }
    public long Counter { get; set; }
    public string Origin { get; set; } = "";

    public int Cmp(Hlc o)
    {
        if (WallMs != o.WallMs) return WallMs < o.WallMs ? -1 : 1;
        if (Counter != o.Counter) return Counter < o.Counter ? -1 : 1;
        return string.CompareOrdinal(Origin, o.Origin);
    }
}

public static class PrefsMerge
{
    public const int MaxTickers = 25;
    public const int MaxPinned = 6;

    public static JsonObject Empty(string sub) => new()
    {
        ["user_sub"] = sub,
        ["fields"] = new JsonObject(),
        ["tickers"] = Set(),
        ["pinned"] = Set(),
        ["version"] = 0,
    };

    static JsonObject Set() => new() { ["adds"] = new JsonObject(), ["removes"] = new JsonObject() };

    public static JsonObject MergeDocs(IEnumerable<JsonObject> docs)
    {
        var output = Empty("");
        foreach (var raw in docs)
        {
            var doc = JsonNode.Parse(raw.ToJsonString())!.AsObject();
            Norm(doc);
            if (doc["user_sub"]?.GetValue<string>() is { Length: > 0 } sub) output["user_sub"] = sub;
            output["version"] = Math.Max(Num(output["version"]), Num(doc["version"]));
            foreach (var field in doc["fields"]!.AsObject())
            {
                var cur = output["fields"]![field.Key];
                if (cur is null || Cmp(field.Value!["hlc"]!.AsObject(), cur["hlc"]!.AsObject()) > 0)
                    output["fields"]![field.Key] = JsonNode.Parse(field.Value!.ToJsonString());
            }
            MergeSet(output["tickers"]!.AsObject(), doc["tickers"]!.AsObject());
            MergeSet(output["pinned"]!.AsObject(), doc["pinned"]!.AsObject());
        }
        return output;
    }

    static void Norm(JsonObject doc)
    {
        doc["fields"] ??= new JsonObject();
        doc["tickers"] ??= Set();
        doc["pinned"] ??= Set();
        doc["tickers"]!["adds"] ??= new JsonObject();
        doc["tickers"]!["removes"] ??= new JsonObject();
        doc["pinned"]!["adds"] ??= new JsonObject();
        doc["pinned"]!["removes"] ??= new JsonObject();
        doc["version"] ??= 0;
        doc["user_sub"] ??= "";
    }

    static void MergeSet(JsonObject dst, JsonObject src)
    {
        foreach (var add in src["adds"]!.AsObject())
            dst["adds"]!.AsObject()[add.Key] ??= JsonNode.Parse(add.Value!.ToJsonString());
        foreach (var rem in src["removes"]!.AsObject())
        {
            var cur = dst["removes"]![rem.Key];
            if (cur is null || Cmp(rem.Value!.AsObject(), cur.AsObject()) > 0)
                dst["removes"]![rem.Key] = JsonNode.Parse(rem.Value!.ToJsonString());
        }
    }

    public static long Num(JsonNode? node)
    {
        if (node is not JsonValue value) return 0;
        if (value.TryGetValue<int>(out var i)) return i;
        if (value.TryGetValue<long>(out var l)) return l;
        if (value.TryGetValue<double>(out var d)) return (long)d;
        return 0;
    }

    static int Cmp(JsonObject a, JsonObject b)
    {
        var aw = Num(a["wall_ms"]);
        var bw = Num(b["wall_ms"]);
        if (aw != bw) return aw < bw ? -1 : 1;
        var ac = Num(a["counter"]);
        var bc = Num(b["counter"]);
        if (ac != bc) return ac < bc ? -1 : 1;
        return string.CompareOrdinal(a["origin"]?.GetValue<string>() ?? "", b["origin"]?.GetValue<string>() ?? "");
    }

    sealed record Visible(string Tag, string Value, JsonObject Hlc);

    static List<Visible> Present(JsonObject orset)
    {
        var items = new List<Visible>();
        foreach (var add in orset["adds"]!.AsObject())
        {
            if (orset["removes"]![add.Key] is not null) continue;
            items.Add(new Visible(add.Key, add.Value!["value"]!.GetValue<string>(), add.Value["hlc"]!.AsObject()));
        }
        items.Sort(CompareVisible);
        var best = new Dictionary<string, Visible>();
        foreach (var item in items)
        {
            if (!best.TryGetValue(item.Value, out var cur) || Cmp(item.Hlc, cur.Hlc) > 0)
                best[item.Value] = item;
        }
        var output = best.Values.ToList();
        output.Sort(CompareVisible);
        return output;
    }

    static int CompareVisible(Visible a, Visible b)
    {
        var c = Cmp(a.Hlc, b.Hlc);
        return c != 0 ? c : string.CompareOrdinal(a.Tag, b.Tag);
    }

    public static JsonObject Project(JsonObject doc)
    {
        Norm(doc);
        var tickers = Present(doc["tickers"]!.AsObject());
        if (tickers.Count > MaxTickers) tickers = tickers.Skip(tickers.Count - MaxTickers).ToList();
        var allowed = tickers.Select(t => t.Value).ToHashSet();
        var pins = Present(doc["pinned"]!.AsObject()).Where(p => allowed.Contains(p.Value)).ToList();
        if (pins.Count > MaxPinned) pins = pins.Skip(pins.Count - MaxPinned).ToList();
        var display = new JsonObject();
        var charts = new JsonObject();
        foreach (var field in doc["fields"]!.AsObject())
        {
            if (field.Value?["tombstone"]?.GetValue<bool>() == true) continue;
            var value = Decode(field.Value?["value"]);
            if (field.Key.StartsWith("display.")) display[field.Key[8..]] = value;
            else if (field.Key.StartsWith("chart.") && value is JsonObject obj) charts[field.Key[6..]] = obj;
        }
        display["theme"] ??= "industrial-dark";
        display["updown_palette"] ??= "green-red";
        display["chart_period"] ??= "1M";
        display["time_zone"] ??= "America/New_York";
        return new JsonObject
        {
            ["tickers"] = new JsonArray(tickers.Select(t => JsonValue.Create(t.Value)).ToArray<JsonNode?>()),
            ["pinned"] = new JsonArray(pins.Select(t => JsonValue.Create(t.Value)).ToArray<JsonNode?>()),
            ["display"] = display,
            ["chart_settings"] = charts,
            ["version"] = Num(doc["version"]),
        };
    }

    static JsonNode? Decode(JsonNode? value)
    {
        if (value is JsonValue v && v.TryGetValue<string>(out var s))
        {
            try { return JsonNode.Parse(s); }
            catch { return JsonValue.Create(s); }
        }
        return value is null ? null : JsonNode.Parse(value.ToJsonString());
    }
}
