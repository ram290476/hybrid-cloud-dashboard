package api

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// Alert is one current or recently cleared condition.
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

type alertPage struct {
	Alerts []Alert `json:"alerts"`
	Firing int     `json:"firing"`
}

type alertBook struct {
	mu    sync.Mutex
	items map[string]Alert
	order []string
}

func newAlertBook() *alertBook {
	return &alertBook{items: map[string]Alert{}}
}

func (b *alertBook) sync(firing []Alert, now time.Time) alertPage {
	b.mu.Lock()
	defer b.mu.Unlock()
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
		prev.Source = alert.Source
		prev.Message = alert.Message
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
		drop := b.order[:len(b.order)-200]
		b.order = b.order[len(b.order)-200:]
		for _, id := range drop {
			delete(b.items, id)
		}
	}
	return pageOf(b.snapshot())
}

func (b *alertBook) snapshot() []Alert {
	out := make([]Alert, 0, len(b.order))
	for _, id := range b.order {
		if alert, ok := b.items[id]; ok {
			out = append(out, alert)
		}
	}
	return out
}

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	if s.alertBook == nil {
		s.alertBook = newAlertBook()
	}
	writeJSON(w, http.StatusOK, s.alertBook.sync(alertsFrom(s.buildDataHealth(r.Context())), s.now()))
}

func alertsFrom(report dataHealth) []Alert {
	var out []Alert
	switch report.Freshness {
	case "stale":
		out = append(out, Alert{ID: "freshness:" + report.Backend, Severity: "warning", Source: "data-freshness", Backend: report.Backend, Message: report.Backend + " serving data is stale"})
	case "missing":
		out = append(out, Alert{ID: "freshness:" + report.Backend, Severity: "warning", Source: "data-freshness", Backend: report.Backend, Message: report.Backend + " serving data is missing"})
	}
	for _, job := range report.Collectors {
		if !healthyStatus(job.Status) {
			out = append(out, Alert{
				ID: "collector:" + report.Backend + ":" + job.Name, Severity: "warning", Source: "collector", Backend: report.Backend,
				Message: "collector " + job.Name + " status is " + job.Status,
			})
		}
		limit := FreshWithinSeconds
		if strings.Contains(strings.ToLower(job.Name), "hourly") {
			limit = 2 * 60 * 60
		}
		if job.AgeSeconds == nil || *job.AgeSeconds > limit {
			out = append(out, Alert{
				ID: "collector-age:" + report.Backend + ":" + job.Name, Severity: "warning", Source: "collector", Backend: report.Backend,
				Message: "collector " + job.Name + " last run is older than its interval",
			})
		}
	}
	return out
}

func pageOf(items []Alert) alertPage {
	firing := 0
	for _, alert := range items {
		if alert.State == "firing" {
			firing++
		}
	}
	if items == nil {
		items = []Alert{}
	}
	return alertPage{Alerts: items, Firing: firing}
}
