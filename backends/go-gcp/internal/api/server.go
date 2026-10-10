package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp/internal/store"
	"github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt"
	"github.com/ram290476/hybrid-cloud-dashboard/libs/merge"
)

// Server is the Go backend. The same routes exist in the Python and .NET services.
type Server struct {
	Store       store.Store
	Backend     string
	Lang        string
	Runtime     string
	Color       string
	Version     string
	DataVersion string
	AuthMode    string
	DevSecret   string
	Issuer      string
	Audience    string
	FacadeKey   string
	Now         func() time.Time
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := s.now()
	s.headers(w)
	switch {
	case r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, s.identity("ok"))
	case r.URL.Path == "/readyz":
		if err := s.Store.Ready(r.Context()); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	case r.URL.Path == "/version":
		writeJSON(w, http.StatusOK, s.identity("ok"))
	case strings.HasPrefix(r.URL.Path, "/api/"):
		s.api(w, r, start)
	default:
		writeProblem(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) api(w http.ResponseWriter, r *http.Request, start time.Time) {
	if s.FacadeKey != "" && r.Header.Get("x-facade-key") != s.FacadeKey {
		writeProblem(w, http.StatusForbidden, "missing facade key")
		return
	}
	sub, err := s.subject(r)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, err.Error())
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/status":
		s.object(w, "serving/status.json")
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/dashboard":
		s.object(w, "serving/dashboard.json")
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/chart/"):
		ticker := strings.TrimPrefix(r.URL.Path, "/api/v1/chart/")
		s.object(w, "serving/chart/"+ticker+".json")
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/prefs":
		s.getPrefs(w, sub)
	case r.Method == http.MethodPut && r.URL.Path == "/api/v1/prefs":
		s.putPrefs(w, r, sub)
	default:
		writeProblem(w, http.StatusNotFound, "not found")
	}
	s.log(r, start)
}

func (s *Server) object(w http.ResponseWriter, key string) {
	body, err := s.Store.GetObject(nil, key)
	if err != nil {
		writeProblem(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) getPrefs(w http.ResponseWriter, sub string) {
	doc, err := s.load(sub)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, merge.Project(doc))
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request, sub string) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var incoming merge.Public
	if err := json.Unmarshal(raw, &incoming); err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, err := s.load(sub)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, err.Error())
		return
	}
	stamp := merge.HLC{WallMs: s.now().UnixMilli(), Origin: s.Backend}
	next, err := merge.ApplyPublic(doc, incoming, stamp)
	if err != nil {
		writeProblem(w, http.StatusConflict, err.Error())
		return
	}
	encoded, _ := json.Marshal(next)
	if err := s.Store.PutPrefs(r.Context(), sub, encoded); err != nil {
		writeProblem(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, merge.Project(next))
}

func (s *Server) load(sub string) (merge.Doc, error) {
	raw, err := s.Store.GetPrefs(nil, sub)
	if err != nil {
		doc := merge.EmptyDoc(sub)
		doc.Fields["display.theme"] = mustScalar("industrial-dark", 1, "seed")
		doc.Fields["display.time_zone"] = mustScalar("America/New_York", 1, "seed")
		doc.Fields["display.updown_palette"] = mustScalar("green-red", 1, "seed")
		doc.Fields["display.chart_period"] = mustScalar("1M", 1, "seed")
		doc.Tickers.Adds["seed-tsla"] = merge.Add{Value: "TSLA", HLC: merge.HLC{WallMs: 1, Origin: "seed"}}
		doc.Tickers.Adds["seed-spcx"] = merge.Add{Value: "SPCX", HLC: merge.HLC{WallMs: 2, Origin: "seed"}}
		doc.Pinned.Adds["seed-tsla"] = merge.Add{Value: "TSLA", HLC: merge.HLC{WallMs: 1, Origin: "seed"}}
		doc.Pinned.Adds["seed-spcx"] = merge.Add{Value: "SPCX", HLC: merge.HLC{WallMs: 2, Origin: "seed"}}
		encoded, _ := json.Marshal(doc)
		_ = s.Store.PutPrefs(nil, sub, encoded)
		return doc, nil
	}
	var doc merge.Doc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return merge.Doc{}, err
	}
	return doc, nil
}

func mustScalar(v any, wall int64, origin string) merge.Scalar {
	raw, _ := json.Marshal(v)
	return merge.Scalar{Value: raw, HLC: merge.HLC{WallMs: wall, Origin: origin}}
}

func (s *Server) subject(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", errors.New("missing bearer token")
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if s.AuthMode == "dev" || s.AuthMode == "" {
		claims, err := devjwt.Parse(s.DevSecret, token, s.Issuer, s.Audience, s.now())
		if err != nil {
			return "", err
		}
		return claims.Sub, nil
	}
	return "", errors.New("cognito validation needs COGNITO_ISSUER; local mode is AUTH_MODE=dev")
}

func (s *Server) headers(w http.ResponseWriter) {
	w.Header().Set("x-backend-env", s.Backend)
	w.Header().Set("x-backend-lang", s.Lang)
	w.Header().Set("x-backend-runtime", s.Runtime)
	w.Header().Set("x-app-color", s.Color)
	w.Header().Set("x-app-version", s.Version)
	w.Header().Set("x-data-version", s.DataVersion)
	w.Header().Set("access-control-allow-origin", "*")
	w.Header().Set("access-control-allow-headers", "authorization,content-type,x-facade-key,x-request-id,traceparent")
	w.Header().Set("access-control-allow-methods", "GET,PUT,OPTIONS")
}

func (s *Server) identity(status string) map[string]string {
	return map[string]string{
		"status": status, "backend": s.Backend, "lang": s.Lang, "runtime": s.Runtime,
		"color": s.Color, "version": s.Version, "data_version": s.DataVersion,
	}
}

func (s *Server) log(r *http.Request, start time.Time) {
	line, _ := json.Marshal(map[string]any{
		"ts": s.now().UTC().Format(time.RFC3339Nano), "level": "info", "msg": "request",
		"route": r.URL.Path, "method": r.Method, "backend": s.Backend, "color": s.Color,
		"duration_ms": s.now().Sub(start).Milliseconds(), "request_id": r.Header.Get("x-request-id"),
	})
	_, _ = os.Stdout.Write(append(line, '\n'))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("content-type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail,
	})
}
