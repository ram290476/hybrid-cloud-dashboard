// Package alerts collects conditions for the Operations page.
package alerts

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/facade/logs"
)

// Alert is one firing or recently resolved condition.
type Alert struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Source    string `json:"source"`
	Backend   string `json:"backend"`
	Message   string `json:"message"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	State     string `json:"state"`
}

// Page is the /api/v1/alerts payload.
type Page struct {
	Alerts []Alert `json:"alerts"`
	Firing int     `json:"firing"`
}

// Snap is the part of a data-health report used to see disagreement.
type Snap struct {
	Backend   string `json:"backend"`
	Freshness string `json:"freshness"`
	Counts    struct {
		Chart int `json:"chart"`
		Stock int `json:"stock"`
		Funds int `json:"funds"`
	} `json:"counts"`
	Files struct {
		Dashboard struct {
			GeneratedAt *string `json:"generated_at"`
		} `json:"dashboard"`
		Status struct {
			GeneratedAt *string `json:"generated_at"`
		} `json:"status"`
	} `json:"files"`
}

// Book remembers alerts after they clear so the page can show them resolved.
type Book struct {
	mu    sync.Mutex
	items map[string]Alert
	order []string
}

func NewBook() *Book {
	return &Book{items: map[string]Alert{}}
}

// Sync marks the given alerts firing and anything previously firing as resolved.
func (b *Book) Sync(firing []Alert, now time.Time) []Alert {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.items == nil {
		b.items = map[string]Alert{}
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	seen := map[string]bool{}
	for _, alert := range firing {
		seen[alert.ID] = true
		prev, ok := b.items[alert.ID]
		if !ok {
			alert.FirstSeen = stamp
			alert.LastSeen = stamp
			alert.State = "firing"
			b.items[alert.ID] = alert
			b.order = append(b.order, alert.ID)
			continue
		}
		prev.Severity = alert.Severity
		prev.Message = alert.Message
		prev.Source = alert.Source
		prev.LastSeen = stamp
		prev.State = "firing"
		b.items[alert.ID] = prev
	}
	for id, alert := range b.items {
		if seen[id] || alert.State == "resolved" {
			continue
		}
		alert.State = "resolved"
		alert.LastSeen = stamp
		b.items[id] = alert
	}
	if len(b.order) > 200 {
		for _, id := range b.order[:len(b.order)-200] {
			delete(b.items, id)
		}
		b.order = b.order[len(b.order)-200:]
	}
	out := make([]Alert, 0, len(b.order))
	for _, id := range b.order {
		if alert, ok := b.items[id]; ok {
			out = append(out, alert)
		}
	}
	return out
}

// Disagreements returns one warning per field the backends do not share.
func Disagreements(snaps []Snap) []Alert {
	if len(snaps) < 2 {
		return nil
	}
	var out []Alert
	if differ(snaps, func(s Snap) string { return value(s.Files.Dashboard.GeneratedAt) }) {
		out = append(out, Alert{ID: "disagree:dashboard-generated-at", Severity: "warning", Source: "data-freshness", Backend: "facade", Message: "backends disagree on dashboard generated_at"})
	}
	if differ(snaps, func(s Snap) string { return value(s.Files.Status.GeneratedAt) }) {
		out = append(out, Alert{ID: "disagree:status-generated-at", Severity: "warning", Source: "data-freshness", Backend: "facade", Message: "backends disagree on status generated_at"})
	}
	for _, field := range []struct {
		id  string
		get func(Snap) string
	}{
		{"chart", func(s Snap) string { return strconv.Itoa(s.Counts.Chart) }},
		{"stock", func(s Snap) string { return strconv.Itoa(s.Counts.Stock) }},
		{"funds", func(s Snap) string { return strconv.Itoa(s.Counts.Funds) }},
	} {
		if differ(snaps, field.get) {
			out = append(out, Alert{ID: "disagree:" + field.id, Severity: "warning", Source: "data-freshness", Backend: "facade", Message: "backends disagree on " + field.id + " count"})
		}
	}
	return out
}

func differ(snaps []Snap, get func(Snap) string) bool {
	first := get(snaps[0])
	for _, snap := range snaps[1:] {
		if get(snap) != first {
			return true
		}
	}
	return false
}

func value(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

// Filter keeps alerts that match the logs-style query plus severity and state.
func Filter(items []Alert, q logs.Query, severity, state string) Page {
	severity = strings.ToLower(strings.TrimSpace(severity))
	state = strings.ToLower(strings.TrimSpace(state))
	text := strings.ToLower(q.Text)
	want := map[string]bool{}
	for _, name := range q.Backends {
		want[name] = true
	}
	var kept []Alert
	for _, alert := range items {
		if len(want) > 0 && !want[alert.Backend] {
			continue
		}
		if severity != "" && alert.Severity != severity {
			continue
		}
		if state != "" && alert.State != state {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(alert.Message+" "+alert.Source+" "+alert.ID), text) {
			continue
		}
		if !overlaps(alert, q.From, q.To) {
			continue
		}
		kept = append(kept, alert)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if (kept[i].State == "firing") != (kept[j].State == "firing") {
			return kept[i].State == "firing"
		}
		if rank(kept[i].Severity) != rank(kept[j].Severity) {
			return rank(kept[i].Severity) < rank(kept[j].Severity)
		}
		return kept[i].LastSeen > kept[j].LastSeen
	})
	firing := 0
	for _, alert := range kept {
		if alert.State == "firing" {
			firing++
		}
	}
	if kept == nil {
		kept = []Alert{}
	}
	return Page{Alerts: kept, Firing: firing}
}

func overlaps(alert Alert, from, to time.Time) bool {
	first, firstOK := parse(alert.FirstSeen)
	last, lastOK := parse(alert.LastSeen)
	if !lastOK {
		last, lastOK = first, firstOK
	}
	if !firstOK {
		return true
	}
	if !to.IsZero() && first.After(to) {
		return false
	}
	if !from.IsZero() && last.Before(from) {
		return false
	}
	return true
}

func parse(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func rank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}
