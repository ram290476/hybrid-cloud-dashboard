package otellog

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEmitWritesRequiredFieldsAndPostsOTLP(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" {
			t.Errorf("path %s", r.URL.Path)
		}
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	var stdout bytes.Buffer
	oldOut, oldEnd, oldNow := Output, Endpoint, Now
	Output, Endpoint = &stdout, srv.URL
	Now = func() time.Time { return time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { Output, Endpoint, Now = oldOut, oldEnd, oldNow })

	Emit(Event{
		Level: "info", Backend: "gcp-go", Cloud: "gcp", Color: "blue",
		Route: "/api/v1/stock/TSLA", Status: 200, TraceID: "abc", Message: "GET /api/v1/stock/TSLA 200",
	})
	// The export runs on a goroutine. Give the test server a moment.
	deadline := time.Now().Add(2 * time.Second)
	for body == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	var line Event
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &line); err != nil {
		t.Fatal(err, stdout.String())
	}
	if line.Backend != "gcp-go" || line.Route != "/api/v1/stock/TSLA" || line.Status != 200 || line.TraceID != "abc" || line.Cloud != "gcp" || line.Color != "blue" || !strings.Contains(line.Message, "TSLA") {
		t.Fatalf("%#v", line)
	}
	if !strings.Contains(string(body), `"service.name"`) || !strings.Contains(string(body), "TSLA") {
		t.Fatalf("otlp %s", body)
	}
}

func TestTraceIDPrefersTraceparent(t *testing.T) {
	if got := TraceID("00-0123456789abcdef0123456789abcdef-0123456789abcdef-01", "req"); got != "0123456789abcdef0123456789abcdef" {
		t.Fatal(got)
	}
	if got := TraceID("", "req"); got != "req" {
		t.Fatal(got)
	}
}
