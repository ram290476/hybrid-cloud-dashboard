// Package proxy is the local facade data plane. It health-checks the three
// backends, applies the Section 13 policy, proxies one request, and writes one
// audit record. The k3s facade runs the same binary; Envoy remains the cloud
// data-plane option described in facade/envoy.
package proxy

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/facade/health"
	"github.com/ram290476/hybrid-cloud-dashboard/facade/policy"
	"github.com/ram290476/hybrid-cloud-dashboard/libs/devjwt"
)

// Audit is one Section 13.6 record.
type Audit struct {
	Timestamp          string         `json:"timestamp"`
	RequestID          string         `json:"request_id"`
	TraceID            string         `json:"trace_id"`
	Client             Client         `json:"client"`
	BackendEnvironment string         `json:"backend_environment"`
	BackendLang        string         `json:"backend_lang"`
	BackendRuntime     string         `json:"backend_runtime"`
	BackendVersion     string         `json:"backend_version"`
	DataVersion        string         `json:"data_version"`
	BackendEndpoint    string         `json:"backend_endpoint"`
	BackendColor       *string        `json:"backend_color"`
	RoutingPolicy      string         `json:"routing_policy"`
	Route              string         `json:"route"`
	PolicyVersion      string         `json:"policy_version"`
	ConfigHash         string         `json:"config_hash"`
	DecisionReason     string         `json:"decision_reason"`
	SkippedBackends    []policy.Skip  `json:"skipped_backends"`
	BackendSnapshot    map[string]any `json:"backend_snapshot"`
	EstCostUSD         float64        `json:"est_cost_usd"`
	UpstreamStatus     int            `json:"upstream_status"`
	Outcome            string         `json:"outcome"`
	FailureClass       *string        `json:"failure_class"`
	LatencyMS          map[string]int `json:"latency_ms"`
	Attempts           int            `json:"attempts"`
	PriorBackends      []Prior        `json:"prior_backends"`
}

type Client struct {
	IPHash string `json:"ip_hash"`
	Method string `json:"method"`
	Host   string `json:"host"`
	Path   string `json:"path"`
}

type Prior struct {
	Env          string `json:"env"`
	FailureClass string `json:"failure_class"`
}

// BackendView is the dashboard card for one environment.
type BackendView struct {
	Name         string  `json:"name"`
	Endpoint     string  `json:"endpoint"`
	Healthy      bool    `json:"healthy"`
	P95MS        float64 `json:"p95_ms"`
	ErrorRate    float64 `json:"error_rate"`
	Weight       int     `json:"weight"`
	Score        float64 `json:"score"`
	Color        string  `json:"color"`
	Version      string  `json:"version"`
	DataVersion  string  `json:"data_version"`
	Lang         string  `json:"lang"`
	Runtime      string  `json:"runtime"`
	CapacityTier string  `json:"capacity_tier"`
	FreeUsedPct  float64 `json:"free_tier_used_pct"`
	MonthCostUSD float64 `json:"month_to_date_cost_usd"`
	Rollout      bool    `json:"rollout_in_progress"`
}

// State is the payload the hybrid dashboard polls.
type State struct {
	PolicyVersion string           `json:"policy_version"`
	ConfigHash    string           `json:"config_hash"`
	Backends      []BackendView    `json:"backends"`
	Routes        []map[string]any `json:"routes"`
	Audit         []Audit          `json:"audit"`
	GeneratedAt   string           `json:"generated_at"`
}

type runtimeInfo struct {
	color, version, dataVersion, lang, runtime string
}

type backendRT struct {
	samples  []float64
	fails    int
	oks      int
	streak   int
	outlier  bool
	info     runtimeInfo
	requests float64
	egress   float64
}

// Server is safe for concurrent use.
type Server struct {
	Doc        policy.Document
	ConfigHash string
	Checker    *health.Checker
	Client     *http.Client
	Salt       string
	FacadeKey  string
	DevSecret  string
	Issuer     string
	Audience   string
	DevInject  bool
	Frontend   string
	Now        func() time.Time
	// Pick, when set, replaces the random weight draw. Tests use it.
	Pick *float64

	mu          sync.Mutex
	obs         map[string]policy.Observation
	weights     map[string]map[string]int
	reasons     map[string]string
	skipped     map[string][]policy.Skip
	scores      map[string]map[string]float64
	lastChange  map[string]time.Time
	prevHealthy map[string]bool
	rt          map[string]*backendRT
	audit       []Audit
	rollout     map[string]bool
}

// New builds a server. Call Evaluate once before serving traffic.
func New(doc policy.Document, hash string) *Server {
	s := &Server{
		Doc: doc, ConfigHash: hash, Checker: health.New(),
		Client: &http.Client{Timeout: 5 * time.Second},
		Salt:   "hcd-local-salt", Issuer: "http://localhost/dev-auth", Audience: "hcd-local",
		Now: time.Now,
		obs: map[string]policy.Observation{}, weights: map[string]map[string]int{},
		reasons: map[string]string{}, skipped: map[string][]policy.Skip{},
		scores: map[string]map[string]float64{}, lastChange: map[string]time.Time{},
		prevHealthy: map[string]bool{}, rt: map[string]*backendRT{}, rollout: map[string]bool{},
	}
	for _, backend := range doc.Spec.Backends {
		s.rt[backend.Name] = &backendRT{info: runtimeInfo{
			lang: backend.Labels["lang"], runtime: backend.Labels["runtime"], color: "blue",
		}}
		s.obs[backend.Name] = policy.Observation{Reachable: false, FreeUsedPct: -1}
	}
	return s
}

// Evaluate recomputes weights for every route from the current observations.
func (s *Server) Evaluate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evaluateLocked()
}

func (s *Server) evaluateLocked() {
	now := s.Now()
	for _, route := range s.Doc.Spec.Routes {
		prefix := route.Match.PathPrefix
		in := policy.Input{
			Now: now, Obs: s.snapshotObs(), Previous: s.weights[prefix],
			PreviousReason: s.reasons[prefix], PrevHealthy: s.prevHealthy,
			LastChange: s.lastChange[prefix],
		}
		result := policy.Compute(s.Doc, route, in)
		if result.Changed || s.weights[prefix] == nil {
			s.lastChange[prefix] = now
		}
		s.weights[prefix] = result.Weights
		if result.Reason != "" {
			s.reasons[prefix] = result.Reason
		}
		s.skipped[prefix] = result.Skipped
		s.scores[prefix] = result.Scores
	}
	next := map[string]bool{}
	for name, obs := range s.obs {
		next[name] = obs.Healthy && obs.Reachable && !obs.OutlierEjected
	}
	s.prevHealthy = next
}

func (s *Server) snapshotObs() map[string]policy.Observation {
	out := map[string]policy.Observation{}
	for name, obs := range s.obs {
		cp := obs
		if rt := s.rt[name]; rt != nil {
			cp.P95MS = p95(rt.samples)
			cp.ErrorRate = rate(rt.fails, rt.oks)
			cp.OutlierEjected = rt.outlier
			cp.Requests = rt.requests
			cp.EgressGB = rt.egress / (1024 * 1024 * 1024)
			cp.MonthCostUSD = s.costOf(name, rt)
			cp.RolloutInProgress = s.rollout[name]
		}
		out[name] = cp
	}
	return out
}

func (s *Server) costOf(name string, rt *backendRT) float64 {
	for _, backend := range s.Doc.Spec.Backends {
		if backend.Name != name {
			continue
		}
		return rt.requests*backend.Cost.PerMillionRequestsUSD/1_000_000 + (rt.egress/(1024*1024*1024))*backend.Cost.PerGBEgressUSD
	}
	return 0
}

// ProbeOnce calls /healthz on every backend and updates the health machine.
func (s *Server) ProbeOnce() {
	for _, backend := range s.Doc.Spec.Backends {
		ok, info := s.probe(backend)
		s.mu.Lock()
		up, _ := s.Checker.Observe(backend.Name, ok)
		obs := s.obs[backend.Name]
		obs.Healthy = up
		obs.Reachable = ok || up
		if !ok && !up {
			obs.Reachable = false
		}
		if ok {
			obs.Reachable = true
		}
		s.obs[backend.Name] = obs
		if ok {
			rt := s.rt[backend.Name]
			if info.color != "" {
				rt.info.color = info.color
			}
			if info.version != "" {
				rt.info.version = info.version
			}
			if info.dataVersion != "" {
				rt.info.dataVersion = info.dataVersion
			}
			if info.lang != "" {
				rt.info.lang = info.lang
			}
			if info.runtime != "" {
				rt.info.runtime = info.runtime
			}
		}
		s.mu.Unlock()
	}
	s.Evaluate()
}

func (s *Server) probe(backend policy.Backend) (bool, runtimeInfo) {
	req, err := http.NewRequest(http.MethodGet, join(backend.Endpoint, "/healthz"), nil)
	if err != nil {
		return false, runtimeInfo{}
	}
	if s.FacadeKey != "" {
		req.Header.Set("x-facade-key", s.FacadeKey)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return false, runtimeInfo{}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return false, runtimeInfo{}
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	return true, runtimeInfo{
		color: str(payload["color"]), version: str(payload["version"]),
		dataVersion: str(payload["data_version"]), lang: str(payload["lang"]), runtime: str(payload["runtime"]),
	}
}

// SetRollout pauses cost-driven shifts for one environment (Section 13.5).
func (s *Server) SetRollout(name string, active bool) {
	s.mu.Lock()
	s.rollout[name] = active
	s.mu.Unlock()
	s.Evaluate()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "role": "facade"})
	case r.URL.Path == "/readyz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	case r.URL.Path == "/version":
		writeJSON(w, http.StatusOK, map[string]string{"version": "dev", "policy": s.Doc.Metadata.Version})
	case r.URL.Path == "/api/v1/facade/state":
		writeJSON(w, http.StatusOK, s.State())
	case r.URL.Path == "/api/v1/facade/rollout" && r.Method == http.MethodPost:
		s.handleRollout(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		s.proxy(w, r)
	default:
		s.static(w, r)
	}
}

func (s *Server) handleRollout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Backend string `json:"backend"`
		Active  bool   `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.SetRollout(body.Backend, body.Active)
	writeJSON(w, http.StatusOK, map[string]any{"backend": body.Backend, "active": body.Active})
}

func (s *Server) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]BackendView, 0, len(s.Doc.Spec.Backends))
	// Show weights for the /api route, which is the default API policy.
	apiWeights := s.weights["/api"]
	if apiWeights == nil {
		apiWeights = s.weights["/"]
	}
	apiScores := s.scores["/api"]
	for _, backend := range s.Doc.Spec.Backends {
		obs := s.obs[backend.Name]
		rt := s.rt[backend.Name]
		tier := "free"
		var pct float64
		if api := findRoute(s.Doc, "/api"); true {
			_ = api
		}
		// Capacity tier from the last free-tier computation stored on obs via requests.
		views = append(views, BackendView{
			Name: backend.Name, Endpoint: backend.Endpoint, Healthy: obs.Healthy && obs.Reachable,
			P95MS: p95(rt.samples), ErrorRate: rate(rt.fails, rt.oks), Weight: apiWeights[backend.Name],
			Score: apiScores[backend.Name], Color: rt.info.color, Version: rt.info.version,
			DataVersion: rt.info.dataVersion, Lang: rt.info.lang, Runtime: rt.info.runtime,
			CapacityTier: tier, FreeUsedPct: pct, MonthCostUSD: s.costOf(backend.Name, rt),
			Rollout: s.rollout[backend.Name],
		})
	}
	// Fill tier from a fresh compute so the UI matches the audit snapshot.
	if route, ok := routeByPrefix(s.Doc, "/api"); ok {
		result := policy.Compute(s.Doc, route, policy.Input{Now: s.Now(), Obs: s.snapshotObs()})
		for i := range views {
			views[i].CapacityTier = result.CapacityTier[views[i].Name]
			views[i].FreeUsedPct = result.FreeUsedPct[views[i].Name]
			if views[i].Weight == 0 {
				views[i].Weight = result.Weights[views[i].Name]
			}
		}
	}
	routes := make([]map[string]any, 0, len(s.Doc.Spec.Routes))
	for _, route := range s.Doc.Spec.Routes {
		routes = append(routes, map[string]any{
			"path": route.Match.PathPrefix, "policy": route.Policy,
			"weights": s.weights[route.Match.PathPrefix], "reason": s.reasons[route.Match.PathPrefix],
		})
	}
	audit := append([]Audit(nil), s.audit...)
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	return State{
		PolicyVersion: s.Doc.Metadata.Version, ConfigHash: s.ConfigHash,
		Backends: views, Routes: routes, Audit: audit, GeneratedAt: s.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	start := s.Now()
	requestID := r.Header.Get("x-request-id")
	if requestID == "" {
		requestID = newID()
	}
	traceID := traceOf(r.Header.Get("traceparent"))
	if traceID == "" {
		traceID = strings.ReplaceAll(newID(), "-", "")
	}
	route := policy.MatchRoute(s.Doc, r.URL.Path)
	s.mu.Lock()
	weights := clone(s.weights[route.Match.PathPrefix])
	reason := s.reasons[route.Match.PathPrefix]
	skipped := append([]policy.Skip(nil), s.skipped[route.Match.PathPrefix]...)
	scores := s.scores[route.Match.PathPrefix]
	s.mu.Unlock()
	if len(weights) == 0 {
		s.Evaluate()
		s.mu.Lock()
		weights = clone(s.weights[route.Match.PathPrefix])
		reason = s.reasons[route.Match.PathPrefix]
		s.mu.Unlock()
	}
	sub := s.subject(r)
	var chosen string
	if stickyHash(route.Affinity.HashOn) && sub != "" {
		chosen = policy.ChooseSticky(weights, sub)
	} else {
		u := unit()
		if s.Pick != nil {
			u = *s.Pick
		}
		chosen = policy.Choose(weights, u)
	}
	body, _ := io.ReadAll(r.Body)
	idempotent := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Header.Get("idempotency-key") != ""
	attempts := 0
	var priors []Prior
	var lastStatus int
	var lastErr string
	var served string
	var respHeader http.Header
	var respBody []byte
	tried := map[string]bool{}
	for attempts < 2 {
		if chosen == "" {
			lastErr = "connection_error"
			break
		}
		tried[chosen] = true
		attempts++
		status, hdr, resp, ferr := s.forward(r, chosen, body, requestID, route, reason)
		if ferr != "" && idempotent && attempts == 1 {
			priors = append(priors, Prior{Env: chosen, FailureClass: ferr})
			lastErr = ferr
			chosen = s.alternate(weights, tried)
			continue
		}
		if status >= 500 && idempotent && attempts == 1 {
			priors = append(priors, Prior{Env: chosen, FailureClass: "upstream_5xx"})
			lastErr = "upstream_5xx"
			lastStatus = status
			chosen = s.alternate(weights, tried)
			continue
		}
		lastStatus = status
		if ferr != "" {
			lastErr = ferr
		} else if status >= 500 {
			lastErr = "upstream_5xx"
		} else {
			lastErr = ""
		}
		served = chosen
		respHeader = hdr
		respBody = resp
		break
	}
	if served == "" {
		served = chosen
	}
	s.record(served, lastStatus, lastErr, len(respBody), start)
	record := s.auditRecord(r, requestID, traceID, route, reason, served, weights, scores, skipped, priors, attempts, lastStatus, lastErr, respHeader, start)
	s.pushAudit(record)
	if lastStatus == 0 {
		writeProblem(w, http.StatusBadGateway, "no healthy backend")
		return
	}
	for key, values := range respHeader {
		if strings.EqualFold(key, "content-length") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("x-request-id", requestID)
	w.Header().Set("x-route-policy", route.Policy)
	w.Header().Set("x-route-reason", reason)
	w.Header().Set("x-route-config", s.ConfigHash)
	w.WriteHeader(lastStatus)
	_, _ = w.Write(respBody)
}

func (s *Server) forward(r *http.Request, name string, body []byte, requestID string, route policy.Route, reason string) (int, http.Header, []byte, string) {
	backend := findBackend(s.Doc, name)
	if backend.Name == "" {
		return 0, nil, nil, "connection_error"
	}
	target := join(backend.Endpoint, r.URL.Path)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequest(r.Method, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, "connection_error"
	}
	req.Header = r.Header.Clone()
	req.Header.Set("x-request-id", requestID)
	req.Header.Set("x-route-policy", route.Policy)
	req.Header.Set("x-route-reason", reason)
	req.Header.Set("x-route-config", s.ConfigHash)
	if s.FacadeKey != "" {
		req.Header.Set("x-facade-key", s.FacadeKey)
	}
	if s.DevInject && req.Header.Get("Authorization") == "" && s.DevSecret != "" {
		token, err := devjwt.Mint(s.DevSecret, devjwt.Claims{
			Sub: "local-user", Iss: s.Issuer, Aud: s.Audience, Iat: s.Now().Unix(), Exp: s.Now().Add(time.Hour).Unix(),
		})
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			return 0, nil, nil, "timeout"
		}
		return 0, nil, nil, "connection_error"
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, resp.Header, respBody, ""
}

func (s *Server) record(name string, status int, ferr string, nbytes int, start time.Time) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.rt[name]
	if rt == nil {
		return
	}
	elapsed := s.Now().Sub(start).Seconds() * 1000
	rt.samples = append(rt.samples, elapsed)
	if len(rt.samples) > 50 {
		rt.samples = rt.samples[len(rt.samples)-50:]
	}
	rt.requests++
	rt.egress += float64(nbytes)
	failed := ferr != "" || status >= 500 || status == 0
	if failed {
		rt.fails++
		rt.streak++
		if rt.streak >= 3 {
			rt.outlier = true
			if obs, ok := s.obs[name]; ok {
				obs.OutlierEjected = true
				s.obs[name] = obs
			}
		}
	} else {
		rt.oks++
		rt.streak = 0
		rt.outlier = false
	}
}

func (s *Server) auditRecord(r *http.Request, requestID, traceID string, route policy.Route, reason, served string, weights map[string]int, scores map[string]float64, skipped []policy.Skip, priors []Prior, attempts, status int, ferr string, hdr http.Header, start time.Time) Audit {
	if hdr == nil {
		hdr = http.Header{}
	}
	s.mu.Lock()
	rt := s.rt[served]
	var info runtimeInfo
	var p95ms, errRate, cost float64
	if rt != nil {
		info = rt.info
		p95ms = p95(rt.samples)
		errRate = rate(rt.fails, rt.oks)
		cost = s.costOf(served, rt)
	}
	obs := s.obs[served]
	s.mu.Unlock()
	backend := findBackend(s.Doc, served)
	var color *string
	if info.color != "" {
		c := info.color
		if hdr.Get("x-app-color") != "" {
			c = hdr.Get("x-app-color")
		}
		color = &c
	}
	lang := info.lang
	if hdr.Get("x-backend-lang") != "" {
		lang = hdr.Get("x-backend-lang")
	}
	runtime := info.runtime
	if hdr.Get("x-backend-runtime") != "" {
		runtime = hdr.Get("x-backend-runtime")
	}
	version := info.version
	if hdr.Get("x-app-version") != "" {
		version = hdr.Get("x-app-version")
	}
	dataVersion := info.dataVersion
	if hdr.Get("x-data-version") != "" {
		dataVersion = hdr.Get("x-data-version")
	}
	outcome := "success"
	var failure *string
	if ferr != "" || status >= 500 || status == 0 {
		outcome = "failure"
		class := ferr
		if class == "" {
			class = "upstream_5xx"
		}
		failure = &class
	}
	tier := "free"
	var pct float64
	if result := policy.Compute(s.Doc, route, policy.Input{Now: s.Now(), Obs: map[string]policy.Observation{served: obs}}); result.CapacityTier != nil {
		tier = result.CapacityTier[served]
		pct = result.FreeUsedPct[served]
	}
	total := int(s.Now().Sub(start) / time.Millisecond)
	if total < 0 {
		total = 0
	}
	if priors == nil {
		priors = []Prior{}
	}
	if skipped == nil {
		skipped = []policy.Skip{}
	}
	return Audit{
		Timestamp: s.Now().UTC().Format(time.RFC3339Nano), RequestID: requestID, TraceID: traceID,
		Client:             Client{IPHash: hashIP(s.Salt, clientIP(r)), Method: r.Method, Host: r.Host, Path: r.URL.Path},
		BackendEnvironment: served, BackendLang: lang, BackendRuntime: runtime,
		BackendVersion: version, DataVersion: dataVersion, BackendEndpoint: backend.Endpoint,
		BackendColor: color, RoutingPolicy: route.Policy, Route: route.Match.PathPrefix,
		PolicyVersion: s.Doc.Metadata.Version, ConfigHash: s.ConfigHash, DecisionReason: reason,
		SkippedBackends: skipped,
		BackendSnapshot: map[string]any{
			"p95_ms": p95ms, "error_rate": errRate, "weight": weights[served], "score": scores[served],
			"capacity_tier": tier, "free_tier_used_pct": pct, "month_to_date_cost_usd": cost,
		},
		EstCostUSD: backend.Cost.PerMillionRequestsUSD / 1_000_000, UpstreamStatus: status, Outcome: outcome, FailureClass: failure,
		LatencyMS: map[string]int{"total": total, "upstream": total}, Attempts: attempts, PriorBackends: priors,
	}
}

func (s *Server) pushAudit(record Audit) {
	s.mu.Lock()
	s.audit = append(s.audit, record)
	if len(s.audit) > 200 {
		s.audit = s.audit[len(s.audit)-200:]
	}
	s.mu.Unlock()
	line, _ := json.Marshal(record)
	// Stdout is the shipping path. Fluent Bit or the collector tails it in the cluster.
	println(string(line))
}

func stickyHash(hashOn string) bool {
	return hashOn == "jwt_sub" || hashOn == "jwt-sub"
}

func (s *Server) subject(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || s.DevSecret == "" {
		return ""
	}
	claims, err := devjwt.Parse(s.DevSecret, strings.TrimPrefix(header, "Bearer "), s.Issuer, s.Audience, s.Now())
	if err != nil {
		return ""
	}
	return claims.Sub
}

func (s *Server) alternate(weights map[string]int, tried map[string]bool) string {
	best := ""
	bestW := 0
	for name, weight := range weights {
		if tried[name] || weight <= 0 {
			continue
		}
		if weight > bestW || (weight == bestW && (best == "" || name < best)) {
			best = name
			bestW = weight
		}
	}
	return best
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if s.Frontend == "" {
		http.NotFound(w, r)
		return
	}
	http.FileServer(http.Dir(s.Frontend)).ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("content-type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail})
}

func join(endpoint, path string) string {
	base := endpoint
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + path
	}
	u.Path = path
	u.RawQuery = ""
	return u.String()
}

func findBackend(doc policy.Document, name string) policy.Backend {
	for _, backend := range doc.Spec.Backends {
		if backend.Name == name {
			return backend
		}
	}
	return policy.Backend{}
}

func findRoute(doc policy.Document, prefix string) policy.Route {
	route, _ := routeByPrefix(doc, prefix)
	return route
}

func routeByPrefix(doc policy.Document, prefix string) (policy.Route, bool) {
	for _, route := range doc.Spec.Routes {
		if route.Match.PathPrefix == prefix {
			return route, true
		}
	}
	return policy.Route{}, false
}

func clone(in map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func p95(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	cp := append([]float64(nil), samples...)
	sort.Float64s(cp)
	idx := int(float64(len(cp)-1) * 0.95)
	return cp[idx]
}

func rate(fails, oks int) float64 {
	total := fails + oks
	if total == 0 {
		return 0
	}
	return float64(fails) / float64(total)
}

func hashIP(salt, ip string) string {
	sum := sha256.Sum256([]byte(salt + "|" + ip))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("x-forwarded-for"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

func newID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:4]) + "-" + hex.EncodeToString(buf[4:6]) + "-" + hex.EncodeToString(buf[6:8]) + "-" + hex.EncodeToString(buf[8:10]) + "-" + hex.EncodeToString(buf[10:])
}

func traceOf(header string) string {
	parts := strings.Split(header, "-")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func unit() float64 {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	var n uint64
	for _, b := range buf {
		n = n<<8 | uint64(b)
	}
	return float64(n%100000) / 100000
}
