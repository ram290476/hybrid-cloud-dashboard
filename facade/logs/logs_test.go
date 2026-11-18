package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMemoryFiltersSearchBackendAndPages(t *testing.T) {
	mem := NewMemory(10)
	base := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	mem.Add(entryAt(base.Add(-2*time.Minute), "gcp-go", "/api/v1/stock/TSLA", "GET /api/v1/stock/TSLA 200"))
	mem.Add(entryAt(base.Add(-time.Minute), "aws-python", "/api/v1/status", "GET /api/v1/status 200"))
	mem.Add(entryAt(base, "facade", "/api/v1/stock/TSLA", "proxied GET /api/v1/stock/TSLA 200"))
	page, err := mem.Query(context.Background(), Query{
		Backends: []string{"gcp-go"}, From: base.Add(-15 * time.Minute), To: base.Add(time.Second), Text: "TSLA", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Backend != "gcp-go" || page.NextCursor != "" {
		t.Fatalf("%#v", page)
	}
	all, err := mem.Query(context.Background(), Query{From: base.Add(-15 * time.Minute), To: base.Add(time.Second), Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 2 || all.Entries[0].Backend != "facade" || all.NextCursor == "" {
		t.Fatalf("%#v", all)
	}
	cursor, _ := time.Parse(time.RFC3339Nano, all.NextCursor)
	next, err := mem.Query(context.Background(), Query{From: base.Add(-15 * time.Minute), To: base.Add(time.Second), Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 1 || next.Entries[0].Route != "/api/v1/stock/TSLA" {
		t.Fatalf("%#v", next)
	}
}

func TestLokiQueryBuildsLogQLAndParsesLines(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("query")
		line, _ := json.Marshal(entryAt(time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC), "gcp-go", "/api/v1/stock/TSLA", "GET /api/v1/stock/TSLA 200"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []any{map[string]any{
				"stream": map[string]string{"service_name": "gcp-go"},
				"values": [][]string{{"1763498400000000000", string(line)}},
			}}},
		})
	}))
	defer srv.Close()
	page, err := (&Loki{URL: srv.URL}).Query(context.Background(), Query{
		Backends: []string{"gcp-go"}, From: time.Unix(0, 0), To: time.Unix(1_800_000_000, 0), Text: "TSLA", Level: "info", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `service_name=~"gcp-go"`) || !strings.Contains(got, "TSLA") || !strings.Contains(got, `level="info"`) {
		t.Fatalf("query %s", got)
	}
	if len(page.Entries) != 1 || page.Entries[0].Route != "/api/v1/stock/TSLA" || page.Source != "loki" {
		t.Fatalf("%#v", page)
	}
}

func TestLokiFailureFallsBackToMemory(t *testing.T) {
	mem := NewMemory(5)
	mem.Add(entryAt(time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC), "facade", "/ops/logs", "down"))
	page, err := (&Loki{URL: "http://127.0.0.1:1", Fallback: mem}).Query(context.Background(), Query{
		From: time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 18, 21, 0, 0, 0, time.UTC), Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Source != "memory" || len(page.Entries) != 1 || !strings.Contains(page.Warning, "Loki") {
		t.Fatalf("%#v", page)
	}
}

func TestParseQueryAcceptsAliasesAndRelativeRanges(t *testing.T) {
	now := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	values := url.Values{}
	values.Add("backend", "go-gcp")
	values.Add("backend", "python-aws")
	values.Set("from", "1h")
	values.Set("q", "TSLA")
	values.Set("level", "error")
	values.Set("limit", "10")
	q, err := ParseQuery(values, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(q.Backends, ",") != "gcp-go,aws-python" || q.Text != "TSLA" || q.Level != "error" || q.Limit != 10 {
		t.Fatalf("%#v", q)
	}
	if q.From.Sub(now) != -time.Hour {
		t.Fatalf("from %s", q.From)
	}
	if _, err := ParseQuery(url.Values{"backend": {"nope"}}, now); err == nil {
		t.Fatal("expected unknown backend")
	}
	if _, err := ParseQuery(url.Values{"from": {"yesterday"}}, now); err == nil {
		t.Fatal("expected bad from")
	}
}

func entryAt(when time.Time, backend, route, message string) Entry {
	return Entry{Timestamp: when.UTC().Format(time.RFC3339Nano), Level: "info", Backend: backend, Cloud: "gcp", Color: "blue", Route: route, Status: 200, TraceID: "trace", Message: message}
}
