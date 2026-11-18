using System.Net;
using System.Net.Http.Headers;
using System.Text.Json.Nodes;
using Microsoft.AspNetCore.Mvc.Testing;
using Xunit;

namespace DashboardApi.Tests;

public class ApiTests : IClassFixture<WebApplicationFactory<Program>>
{
    readonly HttpClient _client;

    public ApiTests(WebApplicationFactory<Program> factory)
    {
        _client = factory.CreateClient();
        var token = DevJwt.Mint("local-dev-secret-change-me", "user-1", "http://localhost/dev-auth", "hcd-local", 1_800_000_000);
        _client.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", token);
    }

    [Fact]
    public async Task DataHealthShapeAndWarningStates()
    {
        var response = await _client.GetAsync("/api/v1/data-health");
        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        var body = JsonNode.Parse(await response.Content.ReadAsStringAsync())!.AsObject();
        Assert.Equal("azure-dotnet", body["backend"]!.GetValue<string>());
        Assert.Equal("azure", body["cloud"]!.GetValue<string>());
        Assert.Equal(DataHealth.FreshWithinSeconds, body["fresh_within_seconds"]!.GetValue<long>());
        Assert.Contains(body["freshness"]!.GetValue<string>(), new[] { "fresh", "stale", "missing" });
        Assert.Equal("2026-10-09T22:00:00Z", body["files"]!["dashboard"]!["generated_at"]!.GetValue<string>());
        Assert.True(body["files"]!["dashboard"]!["present"]!.GetValue<bool>());
        Assert.Equal("prices_daily", body["collectors"]![0]!["name"]!.GetValue<string>());
        var age = body["files"]!["dashboard"]!["age_seconds"]!.GetValue<long>();
        Assert.Equal(age > DataHealth.FreshWithinSeconds ? "stale" : "fresh", body["freshness"]!.GetValue<string>());

        var freshNow = DateTimeOffset.Parse("2026-10-10T10:00:00Z");
        var memory = new MemoryStore();
        foreach (var key in new[] { "serving/dashboard.json", "serving/status.json", "serving/chart/TSLA.json", "serving/stock/TSLA.json", "serving/funds/ARKK.json" })
        {
            var path = Path.Combine(FindRoot(), "contract/fixtures", key.Replace('/', Path.DirectorySeparatorChar));
            await memory.PutObject(key, await File.ReadAllBytesAsync(path));
        }
        var fresh = await DataHealth.Build(memory, "azure-dotnet", "dotnet", "app-service", "blue", "dev", "fixture-2026.10.09", freshNow);
        Assert.Equal("fresh", fresh["freshness"]!.GetValue<string>());
        Assert.Equal(43200, fresh["files"]!["dashboard"]!["age_seconds"]!.GetValue<long>());
        Assert.Equal(1, fresh["counts"]!["chart"]!.GetValue<int>());
        Assert.Contains("TSLA", fresh["tickers"]!.AsArray().Select(n => n!.GetValue<string>()));
        Assert.Null(fresh["import"]);

        await memory.PutObject("serving/_import.json", System.Text.Encoding.UTF8.GetBytes("{\"source\":\"local-seed\",\"origin\":\"contract-fixtures\",\"at\":\"2026-10-10T09:00:00Z\"}"));
        var marked = await DataHealth.Build(memory, "azure-dotnet", "dotnet", "app-service", "blue", "dev", "fixture-2026.10.09", freshNow);
        Assert.Equal("local-seed", marked["import"]!["source"]!.GetValue<string>());
        Assert.Equal(3600, marked["import"]!["age_seconds"]!.GetValue<long>());

        var stale = await DataHealth.Build(memory, "azure-dotnet", "dotnet", "app-service", "blue", "dev", "fixture-2026.10.09", DateTimeOffset.Parse("2026-11-18T20:00:00Z"));
        Assert.Equal("stale", stale["freshness"]!.GetValue<string>());
        Assert.Contains(stale["warnings"]!.AsArray().Select(n => n!.GetValue<string>()), warning => warning.Contains("stale"));

        var missing = await DataHealth.Build(new MemoryStore(), "azure-dotnet", "dotnet", "app-service", "blue", "dev", "fixture-2026.10.09", freshNow);
        Assert.Equal("missing", missing["freshness"]!.GetValue<string>());
        Assert.Empty(missing["collectors"]!.AsArray());
        Assert.Contains(missing["warnings"]!.AsArray().Select(n => n!.GetValue<string>()), warning => warning.Contains("serving/dashboard.json is missing"));
    }

    [Fact]
    public async Task DashboardMatchesFixture()
    {
        var response = await _client.GetAsync("/api/v1/dashboard");
        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal("dotnet", response.Headers.GetValues("x-backend-lang").Single());
        var root = FindRoot();
        var want = JsonNode.Parse(await File.ReadAllTextAsync(Path.Combine(root, "contract/fixtures/serving/dashboard.json")))!;
        var got = JsonNode.Parse(await response.Content.ReadAsStringAsync())!;
        Assert.Equal(want.ToJsonString(), got.ToJsonString());
    }

    [Fact]
    public async Task StockAndFundMatchChart()
    {
        var chart = await _client.GetAsync("/api/v1/chart/TSLA");
        var stock = await _client.GetAsync("/api/v1/stock/TSLA");
        var fund = await _client.GetAsync("/api/v1/fund/ARKK");
        Assert.Equal(HttpStatusCode.OK, chart.StatusCode);
        Assert.Equal(HttpStatusCode.OK, stock.StatusCode);
        Assert.Equal(HttpStatusCode.OK, fund.StatusCode);
        foreach (var name in new[] { "x-backend-env", "x-backend-lang", "x-backend-runtime", "x-app-color", "x-app-version", "x-data-version", "access-control-allow-origin", "access-control-allow-headers" })
        {
            Assert.Equal(chart.Headers.GetValues(name).Single(), stock.Headers.GetValues(name).Single());
            Assert.Equal(chart.Headers.GetValues(name).Single(), fund.Headers.GetValues(name).Single());
        }
        Assert.Equal("application/json", chart.Content.Headers.ContentType?.MediaType);
        Assert.Equal(chart.Content.Headers.ContentType?.MediaType, stock.Content.Headers.ContentType?.MediaType);
        Assert.Equal(chart.Content.Headers.ContentType?.MediaType, fund.Content.Headers.ContentType?.MediaType);
        var root = FindRoot();
        var wantStock = JsonNode.Parse(await File.ReadAllTextAsync(Path.Combine(root, "contract/fixtures/serving/stock/TSLA.json")))!;
        var wantFund = JsonNode.Parse(await File.ReadAllTextAsync(Path.Combine(root, "contract/fixtures/serving/funds/ARKK.json")))!;
        Assert.Equal(wantStock.ToJsonString(), JsonNode.Parse(await stock.Content.ReadAsStringAsync())!.ToJsonString());
        Assert.Equal(wantFund.ToJsonString(), JsonNode.Parse(await fund.Content.ReadAsStringAsync())!.ToJsonString());
        foreach (var path in new[] { "/api/v1/stock/NOPE", "/api/v1/fund/NOPE" })
        {
            var missing = await _client.GetAsync(path);
            Assert.Equal(HttpStatusCode.NotFound, missing.StatusCode);
            Assert.Equal("application/problem+json", missing.Content.Headers.ContentType?.MediaType);
            var problem = JsonNode.Parse(await missing.Content.ReadAsStringAsync())!;
            Assert.Equal("about:blank", problem["type"]!.GetValue<string>());
            Assert.Equal(404, problem["status"]!.GetValue<int>());
            Assert.False(string.IsNullOrEmpty(problem["title"]!.GetValue<string>()));
            Assert.False(string.IsNullOrEmpty(problem["detail"]!.GetValue<string>()));
        }
    }

    [Fact]
    public async Task PrefsKeepsUnrelatedField()
    {
        var current = JsonNode.Parse(await (await _client.GetAsync("/api/v1/prefs")).Content.ReadAsStringAsync())!.AsObject();
        current["display"]!["theme"] = "charting-navy";
        current["display"]!["panel_mode"] = "all";
        current["chart_settings"] = new JsonObject
        {
            ["TSLA"] = new JsonObject
            {
                ["overlays"] = new JsonArray("MA20"),
                ["lanes"] = new JsonArray("VOL"),
            },
        };
        var saved = await _client.PutAsync("/api/v1/prefs", new StringContent(current.ToJsonString(), System.Text.Encoding.UTF8, "application/json"));
        Assert.Equal(HttpStatusCode.OK, saved.StatusCode);
        var body = JsonNode.Parse(await saved.Content.ReadAsStringAsync())!;
        Assert.Equal("charting-navy", body["display"]!["theme"]!.GetValue<string>());
        Assert.Equal("all", body["display"]!["panel_mode"]!.GetValue<string>());
        Assert.Equal("America/New_York", body["display"]!["time_zone"]!.GetValue<string>());
        Assert.Equal("MA20", body["chart_settings"]!["TSLA"]!["overlays"]![0]!.GetValue<string>());
        Assert.Contains("TSLA", body["tickers"]!.AsArray().Select(n => n!.GetValue<string>()));
        body["chart_settings"] = new JsonObject();
        var cleared = await _client.PutAsync("/api/v1/prefs", new StringContent(body.ToJsonString(), System.Text.Encoding.UTF8, "application/json"));
        Assert.Equal(HttpStatusCode.OK, cleared.StatusCode);
        var after = JsonNode.Parse(await cleared.Content.ReadAsStringAsync())!;
        Assert.Empty(after["chart_settings"]!.AsObject());
        Assert.Equal("all", after["display"]!["panel_mode"]!.GetValue<string>());
    }

    [Fact]
    public void MergeVectorsMatch()
    {
        var root = FindRoot();
        var vectors = JsonNode.Parse(File.ReadAllText(Path.Combine(root, "contract/fixtures/merge/vectors.json")))!.AsArray();
        foreach (var vector in vectors)
        {
            var docs = vector!["docs"]!.AsArray().Select(d => d!.AsObject()).ToList();
            var got = PrefsMerge.MergeDocs(docs);
            var want = vector["want"]!.AsObject();
            Assert.Equal(Canonical(want), Canonical(got));
        }
    }

    static string Canonical(JsonNode? node) => Sort(node).ToJsonString();

    static JsonNode? Sort(JsonNode? node)
    {
        if (node is JsonObject obj)
        {
            var sorted = new JsonObject();
            foreach (var key in obj.Select(p => p.Key).OrderBy(k => k, StringComparer.Ordinal))
                sorted[key] = Sort(obj[key]);
            return sorted;
        }
        if (node is JsonArray arr)
        {
            var copy = new JsonArray();
            foreach (var item in arr) copy.Add(Sort(item));
            return copy;
        }
        return node is null ? null : JsonNode.Parse(node.ToJsonString());
    }

    static string FindRoot()
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null)
        {
            if (Directory.Exists(Path.Combine(dir.FullName, "contract", "fixtures"))) return dir.FullName;
            dir = dir.Parent;
        }
        throw new DirectoryNotFoundException("repo root");
    }
}
