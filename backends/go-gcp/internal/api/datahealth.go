package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp/internal/store"
)

// FreshWithinSeconds is how old dashboard.json and status.json may be and
// still count as fresh. Daily collectors run about once a day; 36 hours
// leaves a margin past one missed daytime run.
const FreshWithinSeconds int64 = 36 * 60 * 60

type fileHealth struct {
	Key          string  `json:"key"`
	Present      bool    `json:"present"`
	GeneratedAt  *string `json:"generated_at"`
	LastModified *string `json:"last_modified"`
	AgeSeconds   *int64  `json:"age_seconds"`
}

type collectorHealth struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	LastRun    string `json:"last_run"`
	AgeSeconds *int64 `json:"age_seconds"`
}

type importHealth struct {
	Source     string `json:"source"`
	Origin     string `json:"origin"`
	At         string `json:"at"`
	AgeSeconds *int64 `json:"age_seconds"`
}

type countHealth struct {
	Chart int `json:"chart"`
	Stock int `json:"stock"`
	Funds int `json:"funds"`
}

type dataHealth struct {
	Backend            string      `json:"backend"`
	Lang               string      `json:"lang"`
	Runtime            string      `json:"runtime"`
	Color              string      `json:"color"`
	Version            string      `json:"version"`
	DataVersion        string      `json:"data_version"`
	Cloud              string      `json:"cloud"`
	CheckedAt          string      `json:"checked_at"`
	Freshness          string      `json:"freshness"`
	FreshWithinSeconds int64       `json:"fresh_within_seconds"`
	Counts             countHealth `json:"counts"`
	Files              struct {
		Dashboard fileHealth `json:"dashboard"`
		Status    fileHealth `json:"status"`
	} `json:"files"`
	Tickers    []string          `json:"tickers"`
	NewestAt   *string           `json:"newest_at"`
	OldestAt   *string           `json:"oldest_at"`
	Collectors []collectorHealth `json:"collectors"`
	Import     *importHealth     `json:"import"`
	Warnings   []string          `json:"warnings"`
}

func (s *Server) dataHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.buildDataHealth(r.Context()))
}

func (s *Server) buildDataHealth(ctx context.Context) dataHealth {
	now := s.now().UTC()
	report := dataHealth{
		Backend: s.Backend, Lang: s.Lang, Runtime: s.Runtime, Color: s.Color,
		Version: s.Version, DataVersion: s.DataVersion, Cloud: cloudOf(s.Backend),
		CheckedAt: now.Format(time.RFC3339), Freshness: "missing",
		FreshWithinSeconds: FreshWithinSeconds,
		Tickers:            []string{}, Collectors: []collectorHealth{}, Warnings: []string{},
	}
	listed, err := s.Store.ListObjects(ctx, "serving/")
	if err != nil {
		report.Warnings = append(report.Warnings, "object store unavailable: "+err.Error())
		listed = nil
	}
	modified := map[string]time.Time{}
	var newest, oldest time.Time
	tickers := map[string]struct{}{}
	for _, item := range listed {
		modified[item.Key] = item.Modified
		switch objectPrefix(item.Key) {
		case "chart":
			report.Counts.Chart++
		case "stock":
			report.Counts.Stock++
		case "funds":
			report.Counts.Funds++
		}
		if ticker := objectTicker(item.Key); ticker != "" {
			tickers[ticker] = struct{}{}
		}
		if !item.Modified.IsZero() {
			if newest.IsZero() || item.Modified.After(newest) {
				newest = item.Modified
			}
			if oldest.IsZero() || item.Modified.Before(oldest) {
				oldest = item.Modified
			}
		}
	}
	for ticker := range tickers {
		report.Tickers = append(report.Tickers, ticker)
	}
	sort.Strings(report.Tickers)
	if !newest.IsZero() {
		text := newest.UTC().Format(time.RFC3339)
		report.NewestAt = &text
	}
	if !oldest.IsZero() {
		text := oldest.UTC().Format(time.RFC3339)
		report.OldestAt = &text
	}

	var dashStale, statusStale bool
	var statusBody []byte
	report.Files.Dashboard, dashStale, _ = s.readFile(ctx, now, "serving/dashboard.json", modified["serving/dashboard.json"], &report.Warnings)
	report.Files.Status, statusStale, statusBody = s.readFile(ctx, now, "serving/status.json", modified["serving/status.json"], &report.Warnings)
	report.Collectors = collectorsFrom(statusBody, now, &report.Warnings)
	report.Import = s.readImport(ctx, now, &report.Warnings)
	switch {
	case !report.Files.Dashboard.Present || !report.Files.Status.Present:
		report.Freshness = "missing"
	case dashStale || statusStale:
		report.Freshness = "stale"
	default:
		report.Freshness = "fresh"
	}
	return report
}

func (s *Server) readFile(ctx context.Context, now time.Time, key string, modified time.Time, warnings *[]string) (fileHealth, bool, []byte) {
	file := fileHealth{Key: key}
	body, err := s.Store.GetObject(ctx, key)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			*warnings = append(*warnings, "could not read "+key+": "+err.Error())
		} else {
			*warnings = append(*warnings, key+" is missing")
		}
		return file, false, nil
	}
	file.Present = true
	if !modified.IsZero() {
		text := modified.UTC().Format(time.RFC3339)
		file.LastModified = &text
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) == nil {
		if text, ok := doc["generated_at"].(string); ok && text != "" {
			file.GeneratedAt = &text
		}
	}
	when, ok := fileTime(file.GeneratedAt, modified)
	if !ok {
		*warnings = append(*warnings, key+" has no timestamp")
		return file, true, body
	}
	age := ageSeconds(now, when)
	file.AgeSeconds = &age
	if age > FreshWithinSeconds {
		*warnings = append(*warnings, key+" is stale")
		return file, true, body
	}
	return file, false, body
}

func (s *Server) readImport(ctx context.Context, now time.Time, warnings *[]string) *importHealth {
	body, err := s.Store.GetObject(ctx, "serving/_import.json")
	if err != nil {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		*warnings = append(*warnings, "serving/_import.json is not valid JSON")
		return nil
	}
	info := &importHealth{Source: str(doc["source"]), Origin: str(doc["origin"]), At: str(doc["at"])}
	if info.Source == "" {
		*warnings = append(*warnings, "serving/_import.json has no source")
	}
	if when, ok := parseTime(info.At); ok {
		age := ageSeconds(now, when)
		info.AgeSeconds = &age
	}
	return info
}

func collectorsFrom(body []byte, now time.Time, warnings *[]string) []collectorHealth {
	out := []collectorHealth{}
	if len(body) == 0 {
		return out
	}
	var doc struct {
		Jobs []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			LastRun string `json:"last_run"`
		} `json:"jobs"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return out
	}
	for _, job := range doc.Jobs {
		item := collectorHealth{Name: job.Name, Status: job.Status, LastRun: job.LastRun}
		if when, ok := parseTime(job.LastRun); ok {
			age := ageSeconds(now, when)
			item.AgeSeconds = &age
		}
		if !healthyStatus(job.Status) {
			*warnings = append(*warnings, "collector "+job.Name+" status is "+job.Status)
		}
		out = append(out, item)
	}
	return out
}

func healthyStatus(status string) bool {
	switch strings.ToLower(status) {
	case "ok", "success", "ready":
		return true
	default:
		return false
	}
}

func cloudOf(backend string) string {
	switch {
	case strings.HasPrefix(backend, "gcp"):
		return "gcp"
	case strings.HasPrefix(backend, "aws"):
		return "aws"
	case strings.HasPrefix(backend, "azure"):
		return "azure"
	default:
		return "unknown"
	}
}

func objectPrefix(key string) string {
	for _, name := range []string{"chart", "stock", "funds"} {
		prefix := "serving/" + name + "/"
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, ".json") {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		if rest != "" && !strings.Contains(rest, "/") {
			return name
		}
	}
	return ""
}

func objectTicker(key string) string {
	if objectPrefix(key) == "" {
		return ""
	}
	base := key[strings.LastIndex(key, "/")+1:]
	return strings.TrimSuffix(base, ".json")
}

func fileTime(generated *string, modified time.Time) (time.Time, bool) {
	if generated != nil {
		if ts, ok := parseTime(*generated); ok {
			return ts, true
		}
	}
	if !modified.IsZero() {
		return modified, true
	}
	return time.Time{}, false
}

func parseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func ageSeconds(now, ts time.Time) int64 {
	seconds := int64(now.Sub(ts).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

func str(value any) string {
	text, _ := value.(string)
	return text
}
