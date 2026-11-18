package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/facade/policy"
	"github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt"
)

func testDoc(urls ...string) policy.Document {
	var doc policy.Document
	doc.Metadata.Version = "2026.10.10"
	doc.Spec.Evaluation = policy.Evaluation{MinProbeWeight: 0, HysteresisPct: 0}
	names := []string{"gcp-go", "aws-python", "azure-dotnet"}
	langs := []string{"go", "python", "dotnet"}
	runtimes := []string{"cloud-run", "lambda", "app-service"}
	for i, name := range names {
		if i >= len(urls) {
			break
		}
		doc.Spec.Backends = append(doc.Spec.Backends, policy.Backend{
			Name: name, Endpoint: urls[i],
			Labels:   map[string]string{"lang": langs[i], "runtime": runtimes[i]},
			Cost:     policy.Cost{PerMillionRequestsUSD: 0.40},
			FreeTier: policy.FreeTier{Requests: 100, ThresholdPct: 80},
		})
	}
	route := policy.Route{Policy: policy.FreeTierFirst, FallbackOrder: names}
	route.Match.PathPrefix = "/api"
	doc.Spec.Routes = []policy.Route{route}
	return doc
}

func backend(lang, color string, fail bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","color":"` + color + `","version":"abc","data_version":"fixture","lang":"` + lang + `","runtime":"kind"}`))
			return
		}
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("x-backend-lang", lang)
		w.Header().Set("x-app-color", color)
		w.Header().Set("x-backend-runtime", "kind")
		w.Header().Set("x-app-version", "abc")
		w.Header().Set("x-data-version", "fixture")
		_, _ = w.Write([]byte(`{"ok":true,"lang":"` + lang + `"}`))
	}))
}

func TestDataHealthQueriesTheNamedBackend(t *testing.T) {
	seen := map[string]int{}
	var mu sync.Mutex
	newBackend := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				w.Header().Set("content-type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok","lang":"go","color":"blue","version":"dev","data_version":"fixture","runtime":"kind"}`))
				return
			}
			if r.Header.Get("Authorization") == "" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			mu.Lock()
			seen[name]++
			mu.Unlock()
			if r.URL.Path != "/api/v1/data-health" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("content-type", "application/json")
			_, _ = fmt.Fprintf(w, `{"backend":%q,"freshness":"fresh","counts":{"chart":1,"stock":1,"funds":0}}`, name)
		}))
	}
	a := newBackend("gcp-go")
	b := newBackend("aws-python")
	defer a.Close()
	defer b.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer down.Close()
	// Close the listener so the dial fails instead of returning 500.
	down.Close()
	doc := testDoc(a.URL, b.URL, down.URL)
	srv := New(doc, "c0ffee")
	srv.DevInject = true
	srv.DevSecret = "local-dev-secret-change-me"
	now := time.Unix(1_700_000_000, 0)
	srv.Now = func() time.Time { return now }
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facade/data-health/aws-python", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"backend":"aws-python"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facade/data-health/gcp-go", nil))
	if !strings.Contains(rec.Body.String(), `"backend":"gcp-go"`) {
		t.Fatalf("body %s", rec.Body.String())
	}
	if seen["gcp-go"] != 1 || seen["aws-python"] != 1 {
		t.Fatalf("seen %#v", seen)
	}
	before := len(srv.State().Audit)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facade/data-health/azure-dotnet", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"freshness": "missing"`) || !strings.Contains(rec.Body.String(), "unreachable") {
		t.Fatalf("down status %d body %s", rec.Code, rec.Body.String())
	}
	if len(srv.State().Audit) != before {
		t.Fatal("data-health poll wrote an audit line")
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facade/data-health/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown %d", rec.Code)
	}
}

func TestAlertsHealthFailsAndClears(t *testing.T) {
	up := true
	newBackend := func(name, generated string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "application/json")
			switch r.URL.Path {
			case "/healthz":
				if !up && name == "gcp-go" {
					http.Error(w, "down", http.StatusInternalServerError)
					return
				}
				_, _ = w.Write([]byte(`{"status":"ok","lang":"go","color":"blue","version":"dev","data_version":"fixture","runtime":"kind"}`))
			case "/api/v1/alerts":
				_, _ = w.Write([]byte(`{"alerts":[],"firing":0}`))
			case "/api/v1/data-health":
				fmt.Fprintf(w, `{"backend":%q,"counts":{"chart":1,"stock":1,"funds":0},"files":{"dashboard":{"generated_at":%q},"status":{"generated_at":%q}}}`, name, generated, generated)
			default:
				http.NotFound(w, r)
			}
		}))
	}
	a := newBackend("gcp-go", "2026-10-09T00:00:00Z")
	b := newBackend("aws-python", "2026-10-10T00:00:00Z")
	defer a.Close()
	defer b.Close()
	srv := New(testDoc(a.URL, b.URL), "c0ffee")
	now := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	srv.Now = func() time.Time { return now }
	srv.DevInject = true
	srv.DevSecret = "local-dev-secret-change-me"
	up = false
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/alerts?backend=gcp-go&from=15m", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"severity": "critical"`) || !strings.Contains(rec.Body.String(), "health check is failing") {
		t.Fatalf("down %d %s", rec.Code, rec.Body.String())
	}
	up = true
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/alerts?q=health&from=24h", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state": "resolved"`) {
		t.Fatalf("recovered %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/alerts?q=generated_at&from=24h", nil))
	if !strings.Contains(rec.Body.String(), "disagree on dashboard generated_at") {
		t.Fatalf("disagree %s", rec.Body.String())
	}
}

func TestOpsLogsFiltersTheRecordedRequest(t *testing.T) {
	a := backend("go", "blue", false)
	defer a.Close()
	doc := testDoc(a.URL)
	srv := New(doc, "c0ffee")
	now := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	srv.Now = func() time.Time { return now }
	srv.ProbeOnce()
	srv.ProbeOnce()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stock/TSLA", nil))
	if rec.Code != 200 {
		t.Fatalf("proxy %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ops/logs?backend=go-gcp&q=TSLA&from=15m", nil))
	if rec.Code != 200 {
		t.Fatalf("logs %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/api/v1/stock/TSLA") || !strings.Contains(rec.Body.String(), `"backend": "gcp-go"`) {
		t.Fatalf("body %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ops/logs?backend=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad backend %d", rec.Code)
	}
}

func TestProxyRoutesAndAudits(t *testing.T) {
	a := backend("go", "blue", false)
	b := backend("python", "green", false)
	c := backend("dotnet", "blue", false)
	defer a.Close()
	defer b.Close()
	defer c.Close()
	doc := testDoc(a.URL, b.URL, c.URL)
	srv := New(doc, "c0ffee")
	srv.ProbeOnce()
	srv.ProbeOnce()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if lang := rec.Header().Get("x-backend-lang"); lang != "go" {
		t.Fatalf("free-tier-first should prefer gcp-go, lang %q body %s", lang, rec.Body.String())
	}
	if rec.Header().Get("x-route-policy") != policy.FreeTierFirst {
		t.Fatalf("policy header %s", rec.Header().Get("x-route-policy"))
	}
	state := srv.State()
	if len(state.Audit) != 1 {
		t.Fatalf("audit %d", len(state.Audit))
	}
	record := state.Audit[0]
	if record.RoutingPolicy != policy.FreeTierFirst || record.Outcome != "success" || record.Attempts < 1 {
		t.Fatalf("record %#v", record)
	}
	if record.Client.Path != "/api/v1/status" || strings.Contains(record.Client.IPHash, ".") {
		t.Fatalf("client %#v", record.Client)
	}
	raw, _ := json.Marshal(record)
	for _, field := range []string{"request_id", "backend_environment", "decision_reason", "config_hash", "latency_ms"} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("missing %s in %s", field, raw)
		}
	}
}

func TestRetryIdempotentGetNotPost(t *testing.T) {
	bad := backend("go", "blue", true)
	good := backend("python", "green", false)
	defer bad.Close()
	defer good.Close()
	doc := testDoc(bad.URL, good.URL)
	doc.Spec.Evaluation.MinProbeWeight = 2
	srv := New(doc, "abc")
	pick := 0.9 // lands on gcp-go, which is failing; the retry uses aws-python's probe share
	srv.Pick = &pick
	srv.ProbeOnce()
	srv.ProbeOnce()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if rec.Code != 200 {
		t.Fatalf("GET should retry, got %d %s", rec.Code, rec.Body.String())
	}
	if srv.State().Audit[0].Attempts < 2 {
		t.Fatalf("attempts %#v", srv.State().Audit[0])
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/prefs", strings.NewReader("{}")))
	if rec.Code == 200 {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("POST must not retry onto a healthy backend, body %s", body)
	}
}

func TestStickyAcceptsJwtSubHyphen(t *testing.T) {
	a := backend("go", "blue", false)
	b := backend("python", "green", false)
	defer a.Close()
	defer b.Close()
	doc := testDoc(a.URL, b.URL)
	doc.Spec.Routes[0].Match.PathPrefix = "/api/v1/prefs"
	doc.Spec.Routes[0].Affinity.HashOn = "jwt-sub"
	srv := New(doc, "c0ffee")
	secret := "local-dev-secret-change-me"
	srv.DevSecret = secret
	weights := map[string]int{"gcp-go": 50, "aws-python": 50}
	srv.mu.Lock()
	srv.weights["/api/v1/prefs"] = weights
	srv.mu.Unlock()
	now := time.Unix(1_700_000_000, 0)
	srv.Now = func() time.Time { return now }
	token, err := devjwt.Mint(secret, devjwt.Claims{
		Sub: "user-1", Iss: srv.Issuer, Aud: srv.Audience, Iat: now.Unix(), Exp: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/prefs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	want := policy.ChooseSticky(weights, "user-1")
	got := rec.Header().Get("x-backend-lang")
	expect := map[string]string{"gcp-go": "go", "aws-python": "python"}[want]
	if got != expect {
		t.Fatalf("jwt-sub sticky chose lang %q, want %s (%s)", got, expect, want)
	}
}
