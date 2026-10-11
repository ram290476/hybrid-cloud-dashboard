package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	runtimes := []string{"cloud-run", "ecs-fargate", "app-service"}
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
