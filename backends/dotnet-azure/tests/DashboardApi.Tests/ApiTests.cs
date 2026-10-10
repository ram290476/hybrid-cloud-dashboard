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
    public async Task PrefsKeepsUnrelatedField()
    {
        var current = JsonNode.Parse(await (await _client.GetAsync("/api/v1/prefs")).Content.ReadAsStringAsync())!.AsObject();
        current["display"]!["theme"] = "charting-navy";
        var saved = await _client.PutAsync("/api/v1/prefs", new StringContent(current.ToJsonString(), System.Text.Encoding.UTF8, "application/json"));
        Assert.Equal(HttpStatusCode.OK, saved.StatusCode);
        var body = JsonNode.Parse(await saved.Content.ReadAsStringAsync())!;
        Assert.Equal("charting-navy", body["display"]!["theme"]!.GetValue<string>());
        Assert.Equal("America/New_York", body["display"]!["time_zone"]!.GetValue<string>());
        Assert.Contains("TSLA", body["tickers"]!.AsArray().Select(n => n!.GetValue<string>()));
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
