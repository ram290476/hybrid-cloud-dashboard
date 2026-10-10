package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp/internal/store"
	"github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt"
)

func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	mem := store.NewMemory()
	root := filepath.Join("..", "..", "..", "..", "contract", "fixtures")
	for _, key := range []string{"serving/dashboard.json", "serving/status.json", "serving/chart/TSLA.json"} {
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

func jsonEqual(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	ab, _ := json.Marshal(left)
	bb, _ := json.Marshal(right)
	return string(ab) == string(bb)
}
