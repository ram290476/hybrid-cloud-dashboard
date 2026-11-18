// Package otellog writes one structured JSON log line to stdout and, when
// OTEL_EXPORTER_OTLP_ENDPOINT is set, posts the same line to the OTLP/HTTP
// logs endpoint. The local collector forwards that to Loki.
package otellog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is the line every backend and the facade emit.
type Event struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Backend   string `json:"backend"`
	Cloud     string `json:"cloud"`
	Color     string `json:"color"`
	Route     string `json:"route"`
	Status    int    `json:"status"`
	TraceID   string `json:"trace_id"`
	Message   string `json:"message"`
}

// Now is replaced in tests.
var Now = time.Now

// Endpoint is the OTLP base URL. Empty skips the export. Tests set it directly.
var Endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

// Output receives the JSON line. Tests replace it.
var Output io.Writer = os.Stdout

var (
	client = &http.Client{Timeout: 300 * time.Millisecond}
	queue  = make(chan Event, 64)
	once   sync.Once
)

// Emit writes one line. A full export queue drops the OTLP copy and still writes stdout.
func Emit(ev Event) {
	if ev.Timestamp == "" {
		ev.Timestamp = Now().UTC().Format(time.RFC3339Nano)
	}
	if ev.Level == "" {
		ev.Level = "info"
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = Output.Write(append(raw, '\n'))
	if Endpoint == "" {
		return
	}
	once.Do(func() { go drain() })
	select {
	case queue <- ev:
	default:
	}
}

func drain() {
	for ev := range queue {
		post(ev)
	}
}

func post(ev Event) {
	body := otlpBody(ev)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, strings.TrimRight(Endpoint, "/")+"/v1/logs", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("content-type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func otlpBody(ev Event) []byte {
	line, _ := json.Marshal(ev)
	nanos := strconv.FormatInt(Now().UnixNano(), 10)
	payload := map[string]any{
		"resourceLogs": []any{map[string]any{
			"resource": map[string]any{"attributes": attrs(
				"service.name", ev.Backend,
				"cloud", ev.Cloud,
				"color", ev.Color,
			)},
			"scopeLogs": []any{map[string]any{
				"logRecords": []any{map[string]any{
					"timeUnixNano": nanos,
					"severityText": strings.ToUpper(ev.Level),
					"body":         map[string]any{"stringValue": string(line)},
					"attributes": attrs(
						"backend", ev.Backend,
						"cloud", ev.Cloud,
						"color", ev.Color,
						"route", ev.Route,
						"status", strconv.Itoa(ev.Status),
						"trace_id", ev.TraceID,
						"level", ev.Level,
						"message", ev.Message,
					),
				}},
			}},
		}},
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func attrs(pairs ...string) []map[string]any {
	out := make([]map[string]any, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, map[string]any{
			"key":   pairs[i],
			"value": map[string]any{"stringValue": pairs[i+1]},
		})
	}
	return out
}

// TraceID reads a W3C traceparent or the request id.
func TraceID(traceparent, requestID string) string {
	parts := strings.Split(traceparent, "-")
	if len(parts) >= 2 && parts[1] != "" {
		return parts[1]
	}
	return requestID
}
