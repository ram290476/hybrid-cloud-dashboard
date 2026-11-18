using System.Text;
using System.Text.Json;

namespace DashboardApi;

public static class RequestLog
{
    public static void Emit(string level, string backend, string cloud, string color, string route, int status, string traceId, string message)
    {
        var entry = new Dictionary<string, object?>
        {
            ["timestamp"] = DateTime.UtcNow.ToString("yyyy-MM-ddTHH:mm:ss.fffffffZ"),
            ["level"] = string.IsNullOrEmpty(level) ? "info" : level,
            ["backend"] = backend,
            ["cloud"] = cloud,
            ["color"] = color,
            ["route"] = route,
            ["status"] = status,
            ["trace_id"] = traceId,
            ["message"] = message,
        };
        Console.WriteLine(JsonSerializer.Serialize(entry));
        var endpoint = Environment.GetEnvironmentVariable("OTEL_EXPORTER_OTLP_ENDPOINT");
        if (string.IsNullOrWhiteSpace(endpoint)) return;
        _ = Task.Run(() => Post(endpoint.TrimEnd('/'), entry));
    }

    static void Post(string endpoint, Dictionary<string, object?> entry)
    {
        var line = JsonSerializer.Serialize(entry);
        var payload = new
        {
            resourceLogs = new[]
            {
                new
                {
                    resource = new
                    {
                        attributes = new[]
                        {
                            Attr("service.name", entry["backend"]?.ToString() ?? ""),
                            Attr("cloud", entry["cloud"]?.ToString() ?? ""),
                            Attr("color", entry["color"]?.ToString() ?? ""),
                        }
                    },
                    scopeLogs = new[]
                    {
                        new
                        {
                            logRecords = new[]
                            {
                                new
                                {
                                    timeUnixNano = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() * 1_000_000,
                                    severityText = (entry["level"]?.ToString() ?? "info").ToUpperInvariant(),
                                    body = new { stringValue = line },
                                }
                            }
                        }
                    }
                }
            }
        };
        try
        {
            using var client = new HttpClient { Timeout = TimeSpan.FromMilliseconds(300) };
            using var body = new StringContent(JsonSerializer.Serialize(payload), Encoding.UTF8, "application/json");
            client.PostAsync(endpoint + "/v1/logs", body).GetAwaiter().GetResult();
        }
        catch (Exception)
        {
            // A missing collector must not fail the request.
        }
    }

    static object Attr(string key, string value) => new { key, value = new { stringValue = value } };
}
