package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
)

// ErrNotFound is a missing object or prefs document. Callers may seed defaults
// only for this error. A transport or auth failure must not look like "no prefs yet".
var ErrNotFound = errors.New("not found")

// Store is the snapshot plus prefs contract every backend implements.
// Object keys live in the serving/ and reference/ prefixes. Prefs are the
// internal merge document JSON, keyed by Cognito sub.
type Store interface {
	Ready(ctx context.Context) error
	GetObject(ctx context.Context, key string) ([]byte, error)
	PutObject(ctx context.Context, key string, body []byte) error
	GetPrefs(ctx context.Context, sub string) ([]byte, error)
	PutPrefs(ctx context.Context, sub string, body []byte) error
}

// Memory is the default store for tests and for a backend whose emulator is down.
type Memory struct {
	mu    sync.Mutex
	objs  map[string][]byte
	prefs map[string][]byte
}

func NewMemory() *Memory {
	return &Memory{objs: map[string][]byte{}, prefs: map[string][]byte{}}
}

func (m *Memory) Ready(context.Context) error { return nil }

func (m *Memory) GetObject(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.objs[key]
	if !ok {
		return nil, fmt.Errorf("missing object %s: %w", key, ErrNotFound)
	}
	return append([]byte(nil), body...), nil
}

func (m *Memory) PutObject(_ context.Context, key string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = append([]byte(nil), body...)
	return nil
}

func (m *Memory) GetPrefs(_ context.Context, sub string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.prefs[sub]
	if !ok {
		return nil, fmt.Errorf("missing prefs %s: %w", sub, ErrNotFound)
	}
	return append([]byte(nil), body...), nil
}

func (m *Memory) PutPrefs(_ context.Context, sub string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefs[sub] = append([]byte(nil), body...)
	return nil
}

// GCS talks to fake-gcs-server or Cloud Storage JSON API.
type GCS struct {
	Host   string
	Bucket string
	Client *http.Client
}

func (g GCS) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url("/storage/v1/b/"+url.PathEscape(g.Bucket)), nil)
	if err != nil {
		return err
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gcs status %d", resp.StatusCode)
	}
	return nil
}

func (g GCS) GetObject(ctx context.Context, key string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url("/storage/v1/b/"+url.PathEscape(g.Bucket)+"/o/"+url.PathEscape(key)+"?alt=media"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("missing object %s: %w", key, ErrNotFound)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gcs get %d", resp.StatusCode)
	}
	return body, nil
}

func (g GCS) PutObject(ctx context.Context, key string, body []byte) error {
	endpoint := g.url("/upload/storage/v1/b/" + url.PathEscape(g.Bucket) + "/o?uploadType=media&name=" + url.QueryEscape(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := g.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcs put %d %s", resp.StatusCode, raw)
	}
	return nil
}

func (g GCS) GetPrefs(ctx context.Context, sub string) ([]byte, error) {
	// Firestore is the cloud KV. Until that client is wired, the document lives at prefs/<sub>.json
	// so hcd-sync can merge it. A later Firestore adapter replaces this method only.
	return g.GetObject(ctx, "prefs/"+sub+".json")
}
func (g GCS) PutPrefs(ctx context.Context, sub string, body []byte) error {
	return g.PutObject(ctx, "prefs/"+sub+".json", body)
}

func (g GCS) url(path string) string {
	host := g.Host
	if host == "" {
		host = "http://127.0.0.1:4443"
	}
	if len(host) < 4 || host[:4] != "http" {
		host = "http://" + host
	}
	return host + path
}

func (g GCS) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

// Pair serves objects from one store and prefs from another.
type Pair struct {
	Objects Store
	Prefs   Store
}

func (p Pair) Ready(ctx context.Context) error {
	if err := p.Objects.Ready(ctx); err != nil {
		return err
	}
	return p.Prefs.Ready(ctx)
}
func (p Pair) GetObject(ctx context.Context, key string) ([]byte, error) {
	return p.Objects.GetObject(ctx, key)
}
func (p Pair) PutObject(ctx context.Context, key string, body []byte) error {
	return p.Objects.PutObject(ctx, key, body)
}
func (p Pair) GetPrefs(ctx context.Context, sub string) ([]byte, error) {
	return p.Prefs.GetPrefs(ctx, sub)
}
func (p Pair) PutPrefs(ctx context.Context, sub string, body []byte) error {
	return p.Prefs.PutPrefs(ctx, sub, body)
}
