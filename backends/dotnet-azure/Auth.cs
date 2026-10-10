using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace DashboardApi;

public static class DevJwt
{
    public static string Mint(string secret, string sub, string issuer, string audience, long exp)
    {
        var header = B64(Encoding.UTF8.GetBytes("""{"alg":"HS256","typ":"JWT"}"""));
        var payload = B64(Encoding.UTF8.GetBytes(JsonSerializer.Serialize(new Dictionary<string, object>
        {
            ["sub"] = sub,
            ["iss"] = issuer,
            ["aud"] = audience,
            ["exp"] = exp,
            ["iat"] = exp - 3600,
        })));
        var unsigned = header + "." + payload;
        return unsigned + "." + B64(Sign(unsigned, secret));
    }

    public static string Parse(string secret, string token, string issuer, string audience, DateTimeOffset? now = null)
    {
        var parts = token.Split('.');
        if (parts.Length != 3) throw new InvalidOperationException("malformed token");
        var unsigned = parts[0] + "." + parts[1];
        var expected = Sign(unsigned, secret);
        byte[] got;
        try { got = Unb64(parts[2]); }
        catch { throw new InvalidOperationException("bad signature"); }
        if (!CryptographicOperations.FixedTimeEquals(expected, got))
            throw new InvalidOperationException("bad signature");
        using var doc = JsonDocument.Parse(Unb64(parts[1]));
        var root = doc.RootElement;
        var sub = root.TryGetProperty("sub", out var s) ? s.GetString() : null;
        var iss = root.TryGetProperty("iss", out var i) ? i.GetString() : null;
        var aud = root.TryGetProperty("aud", out var a) ? a.GetString() : null;
        long exp = root.TryGetProperty("exp", out var e) && e.TryGetInt64(out var n) ? n : 0;
        if (!string.IsNullOrEmpty(issuer) && iss != issuer) throw new InvalidOperationException("bad issuer");
        if (!string.IsNullOrEmpty(audience) && aud != audience) throw new InvalidOperationException("bad audience");
        var clock = now ?? DateTimeOffset.UtcNow;
        if (exp != 0 && clock.ToUnixTimeSeconds() >= exp) throw new InvalidOperationException("expired");
        if (string.IsNullOrEmpty(sub)) throw new InvalidOperationException("missing sub");
        return sub;
    }

    static byte[] Sign(string msg, string secret)
    {
        using var hmac = new HMACSHA256(Encoding.UTF8.GetBytes(secret));
        return hmac.ComputeHash(Encoding.UTF8.GetBytes(msg));
    }

    static string B64(byte[] raw) => Convert.ToBase64String(raw).TrimEnd('=').Replace('+', '-').Replace('/', '_');

    static byte[] Unb64(string raw)
    {
        var s = raw.Replace('-', '+').Replace('_', '/');
        s = s.PadRight(s.Length + (4 - s.Length % 4) % 4, '=');
        return Convert.FromBase64String(s);
    }
}
