using System.Collections.Concurrent;
using Azure.Storage.Blobs;

namespace DashboardApi;

public interface IStore
{
    Task Ready();
    Task<byte[]?> GetObject(string key);
    Task PutObject(string key, byte[] body);
    Task<byte[]?> GetPrefs(string sub);
    Task PutPrefs(string sub, byte[] body);
}

public sealed class MemoryStore : IStore
{
    readonly ConcurrentDictionary<string, byte[]> _objects = new();
    readonly ConcurrentDictionary<string, byte[]> _prefs = new();

    public Task Ready() => Task.CompletedTask;
    public Task<byte[]?> GetObject(string key) => Task.FromResult(_objects.TryGetValue(key, out var b) ? b : null);
    public Task PutObject(string key, byte[] body) { _objects[key] = body; return Task.CompletedTask; }
    public Task<byte[]?> GetPrefs(string sub) => Task.FromResult(_prefs.TryGetValue(sub, out var b) ? b : null);
    public Task PutPrefs(string sub, byte[] body) { _prefs[sub] = body; return Task.CompletedTask; }
}

public sealed class BlobMemoryStore : IStore
{
    readonly MemoryStore _memory;
    readonly BlobContainerClient? _container;

    public BlobMemoryStore(MemoryStore memory, string? connection, string container)
    {
        _memory = memory;
        if (!string.IsNullOrEmpty(connection))
            _container = new BlobContainerClient(connection, container);
    }

    public async Task Ready()
    {
        if (_container is null) return;
        await _container.CreateIfNotExistsAsync();
    }

    public async Task<byte[]?> GetObject(string key)
    {
        if (_container is null) return await _memory.GetObject(key);
        var blob = _container.GetBlobClient(key);
        if (!await blob.ExistsAsync()) return await _memory.GetObject(key);
        var downloaded = await blob.DownloadContentAsync();
        return downloaded.Value.Content.ToArray();
    }

    public async Task PutObject(string key, byte[] body)
    {
        if (_container is null) { await _memory.PutObject(key, body); return; }
        await _container.CreateIfNotExistsAsync();
        await _container.GetBlobClient(key).UploadAsync(new BinaryData(body), overwrite: true);
    }

    public async Task<byte[]?> GetPrefs(string sub)
    {
        if (_container is not null)
        {
            var blob = _container.GetBlobClient($"prefs/{sub}.json");
            if (await blob.ExistsAsync())
            {
                var downloaded = await blob.DownloadContentAsync();
                return downloaded.Value.Content.ToArray();
            }
        }
        return await _memory.GetPrefs(sub);
    }

    public async Task PutPrefs(string sub, byte[] body)
    {
        await _memory.PutPrefs(sub, body);
        if (_container is null) return;
        await _container.CreateIfNotExistsAsync();
        await _container.GetBlobClient($"prefs/{sub}.json").UploadAsync(new BinaryData(body), overwrite: true);
    }
}
