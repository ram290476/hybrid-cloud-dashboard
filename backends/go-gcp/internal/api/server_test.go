package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp/internal/store"
	"github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt"
)

func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	mem := store.NewMemory()
	root := filepath.Join("..", "..", "..", "..", "contract", "fixtures")
	for _, key := range []string{"serving/dashboard.json", "serving/status.json", "serving/chart/TSLA.json", "serving/stock/TSLA.json", "serving/funds/ARKK.json"} {
		body, err := os.ReadFile(filepath.Join(root, key))
		if err != nil {
			t.Fatal(err)
		}
		if err := mem.PutObject(nil, key, body); err != nil {
			t.Fatal(err)
		}
	}
	secret := "local-dev-secret-change-me"
	now := time.Unix(1_700_000_000, 0)
	token, err := devjwt.Mint(secret, devjwt.Claims{Sub: "user-1", Iss: "http://localhost/dev-auth", Aud: "hcd-local", Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		Store: mem, Backend: "gcp-go", Lang: "go", Runtime: "cloud-run", Color: "blue",
		Version: "dev", DataVersion: "fixture-2026.10.09", AuthMode: "dev", DevSecret: secret,
		Issuer: "http://localhost/dev-auth", Audience: "hcd-local", FacadeKey: "local-facade-key",
		Now: func() time.Time { return now },
	}, token
}

func tokenAt(t *testing.T, srv *Server, now time.Time) string {
	t.Helper()
	srv.Now = func() time.Time { return now }
	token, err := devjwt.Mint(srv.DevSecret, devjwt.Claims{
		Sub: "user-1", Iss: srv.Issuer, Aud: srv.Audience, Iat: now.Unix(), Exp: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestDataHealthFreshStaleAndMissing(t *testing.T) {
	srv, _ := newServer(t)
	token := tokenAt(t, srv, time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC))
	rec := call(t, srv, http.MethodGet, "/api/v1/data-health", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var fresh map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh["freshness"] != "fresh" || fresh["cloud"] != "gcp" || fresh["backend"] != "gcp-go" {
		t.Fatalf("fresh report %#v", fresh)
	}
	if fresh["fresh_within_seconds"] != float64(FreshWithinSeconds) {
		t.Fatalf("threshold %#v", fresh["fresh_within_seconds"])
	}
	counts := fresh["counts"].(map[string]any)
	if counts["chart"] != float64(1) || counts["stock"] != float64(1) || counts["funds"] != float64(1) {
		t.Fatalf("counts %#v", counts)
	}
	tickers := fresh["tickers"].([]any)
	if len(tickers) != 2 || tickers[0] != "ARKK" || tickers[1] != "TSLA" {
		t.Fatalf("tickers %#v", tickers)
	}
	files := fresh["files"].(map[string]any)
	dash := files["dashboard"].(map[string]any)
	if dash["generated_at"] != "2026-10-09T22:00:00Z" || dash["age_seconds"] != float64(43200) || dash["present"] != true {
		t.Fatalf("dashboard %#v", dash)
	}
	jobs := fresh["collectors"].([]any)
	if len(jobs) != 1 || jobs[0].(map[string]any)["name"] != "prices_daily" {
		t.Fatalf("collectors %#v", jobs)
	}
	if fresh["import"] != nil {
		t.Fatalf("import %#v", fresh["import"])
	}
	if fresh["newest_at"] == nil || fresh["oldest_at"] == nil {
		t.Fatal("file timestamps missing")
	}

	marker := []byte(`{"source":"local-seed","origin":"contract-fixtures","at":"2026-10-10T09:00:00Z"}`)
	if err := srv.Store.PutObject(context.Background(), "serving/_import.json", marker); err != nil {
		t.Fatal(err)
	}
	rec = call(t, srv, http.MethodGet, "/api/v1/data-health", token, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &fresh)
	imported := fresh["import"].(map[string]any)
	if imported["source"] != "local-seed" || imported["origin"] != "contract-fixtures" || imported["age_seconds"] != float64(3600) {
		t.Fatalf("import %#v", imported)
	}

	token = tokenAt(t, srv, time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC))
	rec = call(t, srv, http.MethodGet, "/api/v1/data-health", token, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &fresh)
	if fresh["freshness"] != "stale" {
		t.Fatalf("want stale, got %#v", fresh["freshness"])
	}
	warnings := fresh["warnings"].([]any)
	joined := fmtWarnings(warnings)
	if !strings.Contains(joined, "stale") {
		t.Fatalf("warnings %#v", warnings)
	}

	empty := store.NewMemory()
	srv.Store = empty
	rec = call(t, srv, http.MethodGet, "/api/v1/data-health", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing data status %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &fresh)
	if fresh["freshness"] != "missing" {
		t.Fatalf("want missing, got %#v body %s", fresh["freshness"], rec.Body.String())
	}
	if !strings.Contains(fmtWarnings(fresh["warnings"].([]any)), "serving/dashboard.json is missing") {
		t.Fatalf("warnings %#v", fresh["warnings"])
	}
}

func fmtWarnings(items []any) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.(string))
	}
	return strings.Join(parts, "\n")
}

func TestAlertsWarnForStaleDataAndNotForFresh(t *testing.T) {
	srv, _ := newServer(t)
	token := tokenAt(t, srv, time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC))
	rec := call(t, srv, http.MethodGet, "/api/v1/alerts", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var fresh map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh["firing"] != float64(0) || len(fresh["alerts"].([]any)) != 0 {
		t.Fatalf("fresh alerts %#v", fresh)
	}
	token = tokenAt(t, srv, time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC))
	rec = call(t, srv, http.MethodGet, "/api/v1/alerts", token, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &fresh)
	if fresh["firing"] == float64(0) {
		t.Fatal("stale data produced no alert")
	}
	raw, _ := json.Marshal(fresh["alerts"])
	if !strings.Contains(string(raw), "warning") || !strings.Contains(string(raw), "stale") {
		t.Fatalf("%s", raw)
	}
	srv.Store = store.NewMemory()
	rec = call(t, srv, http.MethodGet, "/api/v1/alerts", token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "missing") {
		t.Fatalf("missing %d %s", rec.Code, rec.Body.String())
	}
}

func TestDashboardMatchesFixture(t *testing.T) {
	srv, token := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-facade-key", "local-facade-key")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec.Header().Get("x-backend-lang") != "go" || rec.Header().Get("x-app-color") != "blue" {
		t.Fatalf("headers %#v", rec.Header())
	}
	want, _ := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "fixtures", "serving", "dashboard.json"))
	if !jsonEqual(rec.Body.Bytes(), want) {
		t.Fatalf("got %s", rec.Body.String())
	}
}

func TestStockAndFundMatchChart(t *testing.T) {
	srv, token := newServer(t)
	chart := call(t, srv, http.MethodGet, "/api/v1/chart/TSLA", token, nil)
	stock := call(t, srv, http.MethodGet, "/api/v1/stock/TSLA", token, nil)
	fund := call(t, srv, http.MethodGet, "/api/v1/fund/ARKK", token, nil)
	if chart.Code != http.StatusOK || stock.Code != http.StatusOK || fund.Code != http.StatusOK {
		t.Fatalf("chart %d stock %d fund %d", chart.Code, stock.Code, fund.Code)
	}
	for _, header := range []string{"content-type", "x-backend-env", "x-backend-lang", "x-backend-runtime", "x-app-color", "x-app-version", "x-data-version", "access-control-allow-origin", "access-control-allow-headers", "access-control-allow-methods"} {
		if stock.Header().Get(header) != chart.Header().Get(header) || fund.Header().Get(header) != chart.Header().Get(header) {
			t.Fatalf("%s chart %q stock %q fund %q", header, chart.Header().Get(header), stock.Header().Get(header), fund.Header().Get(header))
		}
	}
	if chart.Header().Get("content-type") != "application/json" {
		t.Fatalf("content-type %s", chart.Header().Get("content-type"))
	}
	root := filepath.Join("..", "..", "..", "..", "contract", "fixtures", "serving")
	wantStock, err := os.ReadFile(filepath.Join(root, "stock", "TSLA.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFund, err := os.ReadFile(filepath.Join(root, "funds", "ARKK.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(stock.Body.Bytes(), wantStock) || !jsonEqual(fund.Body.Bytes(), wantFund) {
		t.Fatalf("stock %s fund %s", stock.Body.String(), fund.Body.String())
	}
	missing := call(t, srv, http.MethodGet, "/api/v1/stock/NOPE", token, nil)
	missingFund := call(t, srv, http.MethodGet, "/api/v1/fund/NOPE", token, nil)
	for _, rec := range []*httptest.ResponseRecorder{missing, missingFund} {
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("content-type") != "application/problem+json" {
			t.Fatalf("content-type %s", rec.Header().Get("content-type"))
		}
		var problem map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if problem["type"] != "about:blank" || problem["status"] != float64(http.StatusNotFound) || problem["title"] == "" || problem["detail"] == "" {
			t.Fatalf("problem %#v", problem)
		}
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	srv, token := newServer(t)
	rec := call(t, srv, http.MethodGet, "/api/v1/prefs", token, nil)
	var prefs map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &prefs); err != nil {
		t.Fatal(err)
	}
	display := prefs["display"].(map[string]any)
	display["theme"] = "midnight-slate"
	prefs["display"] = display
	body, _ := json.Marshal(prefs)
	rec = call(t, srv, http.MethodPut, "/api/v1/prefs", token, body)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var next map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &next)
	if next["display"].(map[string]any)["theme"] != "midnight-slate" {
		t.Fatalf("%s", rec.Body.String())
	}
	if next["display"].(map[string]any)["time_zone"] != "America/New_York" {
		t.Fatal("unrelated field was dropped")
	}
}

func TestRejectsMissingFacadeKey(t *testing.T) {
	srv, token := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func call(t *testing.T, srv *Server, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-facade-key", "local-facade-key")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestGCSObjectUsesRequestContext(t *testing.T) {
	var hits int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Context() == nil {
			t.Error("request context is nil")
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.RawPath+r.URL.Path, "serving") {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer fake.Close()
	srv, token := newServer(t)
	srv.Store = store.GCS{Host: fake.URL, Bucket: "hcd-serving"}
	rec := call(t, srv, http.MethodGet, "/api/v1/status", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if hits == 0 {
		t.Fatal("fake-gcs received no request; a nil context never leaves the client")
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestPrefsReadErrorDoesNotOverwrite(t *testing.T) {
	srv, token := newServer(t)
	broken := &failPrefs{Store: srv.Store}
	srv.Store = broken
	rec := call(t, srv, http.MethodGet, "/api/v1/prefs", token, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if broken.puts != 0 {
		t.Fatalf("PutPrefs called %d times; a read error must not seed defaults", broken.puts)
	}
}

type failPrefs struct {
	store.Store
	puts int
}

func (f *failPrefs) GetPrefs(context.Context, string) ([]byte, error) {
	return nil, errors.New("object store unavailable")
}

func (f *failPrefs) PutPrefs(context.Context, string, []byte) error {
	f.puts++
	return nil
}

func jsonEqual(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	ab, _ := json.Marshal(left)
	bb, _ := json.Marshal(right)
	return string(ab) == string(bb)
}
