using System.Text.Json;
using System.Text.Json.Nodes;
using DashboardApi;

var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

var backend = Env("APP_BACKEND", "azure-dotnet");
var lang = Env("APP_LANG", "dotnet");
var runtime = Env("APP_RUNTIME", "app-service");
var color = Env("APP_COLOR", "blue");
var version = Env("APP_VERSION", "dev");
var dataVersion = Env("DATA_VERSION", "fixture-2026.10.09");
var authMode = Env("AUTH_MODE", "dev");
var secret = Env("DEV_JWT_SECRET", "local-dev-secret-change-me");
var issuer = Env("DEV_JWT_ISSUER", "http://localhost/dev-auth");
var audience = Env("DEV_JWT_AUDIENCE", "hcd-local");
var facadeKey = Environment.GetEnvironmentVariable("FACADE_KEY") ?? "";
var fixtures = Env("FIXTURES_DIR", FindFixtures());

var memory = new MemoryStore();
foreach (var key in new[] { "serving/dashboard.json", "serving/status.json", "serving/chart/TSLA.json", "serving/chart/SPCX.json" })
{
    var path = Path.Combine(fixtures, key.Replace('/', Path.DirectorySeparatorChar));
    if (File.Exists(path)) await memory.PutObject(key, await File.ReadAllBytesAsync(path));
}
IStore store = new BlobMemoryStore(memory, Environment.GetEnvironmentVariable("AZURE_STORAGE_CONNECTION"), Env("BLOB_CONTAINER", "hcd-serving"));

app.Use(async (ctx, next) =>
{
    void Head()
    {
        ctx.Response.Headers["x-backend-env"] = backend;
        ctx.Response.Headers["x-backend-lang"] = lang;
        ctx.Response.Headers["x-backend-runtime"] = runtime;
        ctx.Response.Headers["x-app-color"] = color;
        ctx.Response.Headers["x-app-version"] = version;
        ctx.Response.Headers["x-data-version"] = dataVersion;
        ctx.Response.Headers["access-control-allow-origin"] = "*";
        ctx.Response.Headers["access-control-allow-headers"] = "authorization,content-type,x-facade-key,x-request-id,traceparent";
    }
    Head();
    if (HttpMethods.IsOptions(ctx.Request.Method)) { ctx.Response.StatusCode = 204; return; }
    await next();
    if (!ctx.Response.HasStarted) Head();
});

app.MapGet("/healthz", () => Identity("ok"));
app.MapGet("/version", () => Identity("ok"));
app.MapGet("/readyz", async () =>
{
    try { await store.Ready(); return Results.Json(new { status = "ready" }); }
    catch (Exception ex) { return Problem(503, ex.Message); }
});

app.MapGet("/api/v1/status", (HttpRequest req) => Object(req, "serving/status.json"));
app.MapGet("/api/v1/dashboard", (HttpRequest req) => Object(req, "serving/dashboard.json"));
app.MapGet("/api/v1/chart/{ticker}", (HttpRequest req, string ticker) => Object(req, $"serving/chart/{ticker}.json"));
app.MapGet("/api/v1/prefs", async (HttpRequest req) =>
{
    var guard = Guard(req);
    if (guard.Error is not null) return guard.Error;
    var doc = await Load(guard.Sub!);
    return Results.Json(PrefsMerge.Project(doc));
});
app.MapPut("/api/v1/prefs", async (HttpRequest req) =>
{
    var guard = Guard(req);
    if (guard.Error is not null) return guard.Error;
    var incoming = await JsonNode.ParseAsync(req.Body) as JsonObject ?? new JsonObject();
    var doc = await Load(guard.Sub!);
    if (PrefsMerge.Num(incoming["version"]) != PrefsMerge.Num(doc["version"]))
        return Problem(409, "version conflict");
    var current = PrefsMerge.Project(doc);
    doc["version"] = PrefsMerge.Num(doc["version"]) + 1;
    var wall = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
    var stamp = new JsonObject { ["wall_ms"] = wall, ["counter"] = 0, ["origin"] = backend };
    if (incoming["display"] is JsonObject display)
    {
        foreach (var key in new[] { "theme", "time_zone", "updown_palette", "chart_period", "panels" })
        {
            if (display[key] is null) continue;
            if (display[key]!.ToJsonString() == current["display"]?[key]?.ToJsonString()) continue;
            doc["fields"]![$"display.{key}"] = new JsonObject { ["value"] = JsonNode.Parse(display[key]!.ToJsonString()), ["hlc"] = stamp.DeepClone() };
        }
    }
    ApplySet(doc["tickers"]!.AsObject(), current["tickers"]!.AsArray(), incoming["tickers"] as JsonArray, stamp, backend, wall);
    ApplySet(doc["pinned"]!.AsObject(), current["pinned"]!.AsArray(), incoming["pinned"] as JsonArray, stamp, backend, wall);
    await store.PutPrefs(guard.Sub!, System.Text.Encoding.UTF8.GetBytes(doc.ToJsonString()));
    return Results.Json(PrefsMerge.Project(doc));
});

app.Run();

IResult Identity(string status) => Results.Json(new
{
    status, backend, lang, runtime, color, version, data_version = dataVersion
});

async Task<IResult> Object(HttpRequest req, string key)
{
    var guard = Guard(req);
    if (guard.Error is not null) return guard.Error;
    var body = await store.GetObject(key) ?? await memory.GetObject(key);
    if (body is null) return Problem(404, $"missing {key}");
    return Results.Bytes(body, "application/json");
}

(string? Sub, IResult? Error) Guard(HttpRequest req)
{
    if (!string.IsNullOrEmpty(facadeKey) && req.Headers["x-facade-key"] != facadeKey)
        return (null, Problem(403, "missing facade key"));
    var header = req.Headers.Authorization.ToString();
    if (!header.StartsWith("Bearer ")) return (null, Problem(401, "missing bearer token"));
    if (authMode is not ("" or "dev"))
        return (null, Problem(401, "cognito validation needs COGNITO_ISSUER; local mode is AUTH_MODE=dev"));
    try { return (DevJwt.Parse(secret, header["Bearer ".Length..], issuer, audience), null); }
    catch (Exception ex) { return (null, Problem(401, ex.Message)); }
}

async Task<JsonObject> Load(string sub)
{
    var raw = await store.GetPrefs(sub);
    if (raw is not null) return JsonNode.Parse(raw)!.AsObject();
    var doc = PrefsMerge.Empty(sub);
    JsonObject Scalar(string value, long wall) => new()
    {
        ["value"] = value,
        ["hlc"] = new JsonObject { ["wall_ms"] = wall, ["counter"] = 0, ["origin"] = "seed" },
    };
    doc["fields"] = new JsonObject
    {
        ["display.theme"] = Scalar("industrial-dark", 1),
        ["display.time_zone"] = Scalar("America/New_York", 1),
        ["display.updown_palette"] = Scalar("green-red", 1),
        ["display.chart_period"] = Scalar("1M", 1),
    };
    doc["tickers"]!["adds"] = new JsonObject
    {
        ["seed-tsla"] = new JsonObject { ["value"] = "TSLA", ["hlc"] = new JsonObject { ["wall_ms"] = 1, ["counter"] = 0, ["origin"] = "seed" } },
        ["seed-spcx"] = new JsonObject { ["value"] = "SPCX", ["hlc"] = new JsonObject { ["wall_ms"] = 2, ["counter"] = 0, ["origin"] = "seed" } },
    };
    doc["pinned"]!["adds"] = new JsonObject
    {
        ["seed-tsla"] = new JsonObject { ["value"] = "TSLA", ["hlc"] = new JsonObject { ["wall_ms"] = 1, ["counter"] = 0, ["origin"] = "seed" } },
        ["seed-spcx"] = new JsonObject { ["value"] = "SPCX", ["hlc"] = new JsonObject { ["wall_ms"] = 2, ["counter"] = 0, ["origin"] = "seed" } },
    };
    await store.PutPrefs(sub, System.Text.Encoding.UTF8.GetBytes(doc.ToJsonString()));
    return doc;
}

static void ApplySet(JsonObject orset, JsonArray before, JsonArray? after, JsonObject stamp, string origin, long wall)
{
    var prev = before.Select(n => n!.GetValue<string>()).ToHashSet();
    var next = (after ?? new JsonArray()).Select(n => n!.GetValue<string>()).ToHashSet();
    foreach (var value in prev.Except(next))
    {
        foreach (var add in orset["adds"]!.AsObject())
        {
            if (add.Value!["value"]!.GetValue<string>() == value && orset["removes"]![add.Key] is null)
                orset["removes"]![add.Key] = stamp.DeepClone();
        }
    }
    foreach (var value in next.Except(prev))
        orset["adds"]![$"{origin}-{wall}-{value}"] = new JsonObject { ["value"] = value, ["hlc"] = stamp.DeepClone() };
}

static IResult Problem(int status, string detail) => Results.Json(
    new { type = "about:blank", title = detail, status, detail },
    statusCode: status, contentType: "application/problem+json");

static string Env(string key, string fallback) =>
    Environment.GetEnvironmentVariable(key) is { Length: > 0 } v ? v : fallback;

static string FindFixtures()
{
    var dir = new DirectoryInfo(Directory.GetCurrentDirectory());
    while (dir is not null)
    {
        var candidate = Path.Combine(dir.FullName, "contract", "fixtures");
        if (Directory.Exists(candidate)) return candidate;
        dir = dir.Parent;
    }
    return "contract/fixtures";
}

public partial class Program;
