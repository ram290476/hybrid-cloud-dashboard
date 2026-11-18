// Package logs is the facade's log source. Loki is the local store. A memory
// source stands in for tests and for a laptop that has not started the lgtm stack.
package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Entry is one structured line.
type Entry struct {
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

// Query is the /ops/logs filter.
type Query struct {
	Backends []string
	From     time.Time
	To       time.Time
	Text     string
	Level    string
	Limit    int
	Cursor   time.Time
}

// Page is one window, newest first.
type Page struct {
	Source     string  `json:"source"`
	Warning    string  `json:"warning,omitempty"`
	Entries    []Entry `json:"entries"`
	NextCursor string  `json:"next_cursor"`
}

// Source reads log lines.
type Source interface {
	Query(ctx context.Context, q Query) (Page, error)
}

// Memory keeps the newest lines the facade recorded.
type Memory struct {
	mu   sync.Mutex
	cap  int
	rows []Entry
}

func NewMemory(cap int) *Memory {
	if cap <= 0 {
		cap = 500
	}
	return &Memory{cap: cap}
}

func (m *Memory) Add(ev Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, ev)
	if len(m.rows) > m.cap {
		m.rows = m.rows[len(m.rows)-m.cap:]
	}
}

func (m *Memory) Query(_ context.Context, q Query) (Page, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	m.mu.Lock()
	rows := append([]Entry(nil), m.rows...)
	m.mu.Unlock()
	rows = filter(rows, q)
	return finish("memory", "", rows, q.Limit, len(rows) > q.Limit), nil
}

// Loki queries a Loki HTTP API. Fallback is used when Loki cannot be reached.
type Loki struct {
	URL      string
	Client   *http.Client
	Fallback *Memory
}

func (l *Loki) Query(ctx context.Context, q Query) (Page, error) {
	page, err := l.query(ctx, q)
	if err != nil && l.Fallback != nil {
		fallback, ferr := l.Fallback.Query(ctx, q)
		if ferr != nil {
			return Page{}, ferr
		}
		fallback.Warning = "Loki is not reachable; showing logs recorded by the facade. " + err.Error()
		return fallback, nil
	}
	return page, err
}

func (l *Loki) query(ctx context.Context, q Query) (Page, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	end := q.To
	if !q.Cursor.IsZero() && (end.IsZero() || q.Cursor.Before(end)) {
		end = q.Cursor.Add(-time.Nanosecond)
	}
	values := url.Values{}
	values.Set("query", logQL(q))
	values.Set("start", strconv.FormatInt(q.From.UnixNano(), 10))
	values.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	values.Set("limit", strconv.Itoa(q.Limit))
	values.Set("direction", "backward")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(l.URL, "/")+"/loki/api/v1/query_range?"+values.Encode(), nil)
	if err != nil {
		return Page{}, err
	}
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return Page{}, fmt.Errorf("loki status %d", resp.StatusCode)
	}
	entries, err := parseLoki(body)
	if err != nil {
		return Page{}, err
	}
	more := len(entries) >= q.Limit
	return finish("loki", "", entries, q.Limit, more), nil
}

func logQL(q Query) string {
	names := q.Backends
	if len(names) == 0 {
		names = []string{"gcp-go", "aws-python", "azure-dotnet", "facade"}
	}
	query := fmt.Sprintf("{service_name=~%s}", strconv.Quote(strings.Join(names, "|")))
	if q.Text != "" {
		query += " |= " + strconv.Quote(q.Text)
	}
	if q.Level != "" {
		query += " | json | level=" + strconv.Quote(q.Level)
	}
	return query
}

func parseLoki(body []byte) ([]Entry, error) {
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var rows []Entry
	for _, stream := range payload.Data.Result {
		for _, pair := range stream.Values {
			if len(pair) < 2 {
				continue
			}
			rows = append(rows, entryFromLine(pair[0], pair[1], stream.Stream))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Timestamp > rows[j].Timestamp })
	return rows, nil
}

func entryFromLine(nanos, line string, stream map[string]string) Entry {
	var ev Entry
	if json.Unmarshal([]byte(line), &ev) != nil || ev.Timestamp == "" {
		ev.Message = line
		ev.Backend = stream["service_name"]
		if n, err := strconv.ParseInt(nanos, 10, 64); err == nil {
			ev.Timestamp = time.Unix(0, n).UTC().Format(time.RFC3339Nano)
		}
		if ev.Level == "" {
			ev.Level = "info"
		}
	}
	if ev.Backend == "" {
		ev.Backend = stream["service_name"]
	}
	return ev
}

func filter(rows []Entry, q Query) []Entry {
	want := map[string]bool{}
	for _, name := range q.Backends {
		want[name] = true
	}
	text := strings.ToLower(q.Text)
	var out []Entry
	for _, row := range rows {
		when, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err != nil {
			when, err = time.Parse(time.RFC3339, row.Timestamp)
		}
		if err == nil {
			if !q.From.IsZero() && when.Before(q.From) {
				continue
			}
			if !q.Cursor.IsZero() {
				if !when.Before(q.Cursor) {
					continue
				}
			} else if !q.To.IsZero() && when.After(q.To) {
				continue
			}
		}
		if len(want) > 0 && !want[row.Backend] {
			continue
		}
		if q.Level != "" && !strings.EqualFold(row.Level, q.Level) {
			continue
		}
		if text != "" {
			blob := strings.ToLower(row.Message + " " + row.Route + " " + row.TraceID + " " + row.Backend)
			if !strings.Contains(blob, text) {
				continue
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

func finish(source, warning string, rows []Entry, limit int, more bool) Page {
	if limit <= 0 {
		limit = 50
	}
	page := Page{Source: source, Warning: warning, Entries: rows}
	if len(rows) > limit {
		page.Entries = append([]Entry(nil), rows[:limit]...)
		more = true
	}
	if more && len(page.Entries) > 0 {
		page.NextCursor = page.Entries[len(page.Entries)-1].Timestamp
	}
	if page.Entries == nil {
		page.Entries = []Entry{}
	}
	return page
}

// ParseQuery reads the /ops/logs query string. Relative from values are 15m, 1h, 24h, and 7d.
func ParseQuery(values url.Values, now time.Time) (Query, error) {
	q := Query{To: now.UTC(), Limit: 50}
	for _, raw := range values["backend"] {
		for _, part := range strings.Split(raw, ",") {
			name, ok := CanonicalBackend(strings.TrimSpace(part))
			if part != "" && !ok {
				return Query{}, fmt.Errorf("unknown backend %s", part)
			}
			if ok {
				q.Backends = append(q.Backends, name)
			}
		}
	}
	from := strings.TrimSpace(values.Get("from"))
	switch from {
	case "", "15m":
		q.From = now.Add(-15 * time.Minute)
	case "1h":
		q.From = now.Add(-time.Hour)
	case "24h":
		q.From = now.Add(-24 * time.Hour)
	case "7d":
		q.From = now.Add(-7 * 24 * time.Hour)
	default:
		parsed, err := parseTime(from)
		if err != nil {
			return Query{}, fmt.Errorf("from must be 15m, 1h, 24h, 7d, or an ISO time")
		}
		q.From = parsed
	}
	if to := strings.TrimSpace(values.Get("to")); to != "" {
		parsed, err := parseTime(to)
		if err != nil {
			return Query{}, fmt.Errorf("to must be an ISO time")
		}
		q.To = parsed
	}
	if cursor := strings.TrimSpace(values.Get("cursor")); cursor != "" {
		parsed, err := parseTime(cursor)
		if err != nil {
			return Query{}, fmt.Errorf("cursor must be an ISO time")
		}
		q.Cursor = parsed
	}
	q.Text = values.Get("q")
	q.Level = strings.ToLower(strings.TrimSpace(values.Get("level")))
	if q.Level != "" && q.Level != "debug" && q.Level != "info" && q.Level != "warn" && q.Level != "error" {
		return Query{}, fmt.Errorf("level must be debug, info, warn, or error")
	}
	if raw := values.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return Query{}, fmt.Errorf("limit must be a positive integer")
		}
		if n > 200 {
			n = 200
		}
		q.Limit = n
	}
	return q, nil
}

// CanonicalBackend accepts the process name and the directory name.
func CanonicalBackend(name string) (string, bool) {
	switch name {
	case "gcp-go", "go-gcp":
		return "gcp-go", true
	case "aws-python", "python-aws":
		return "aws-python", true
	case "azure-dotnet", "dotnet-azure":
		return "azure-dotnet", true
	case "facade":
		return "facade", true
	default:
		return "", false
	}
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time")
}
