// Package policy turns the Section 13.3 routing document into per-environment weights.
// Weights are per environment, never per color. Blue/green stays inside each backend.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	LowestCost        = "lowest-cost"
	BestPerformance   = "best-performance"
	Blended           = "blended"
	FreeTierFirst     = "free-tier-first"
	ManualOverride    = "manual-override"
	ReasonCheapest    = "cheapest_healthy"
	ReasonBestScore   = "best_score"
	ReasonFree        = "free_tier_below_threshold"
	ReasonSpill       = "free_tier_threshold_spill"
	ReasonHardCap     = "hard_cap_reached"
	ReasonCapOverflow = "cap_overflow"
	ReasonSLO         = "slo_floor_skip"
	ReasonOutlier     = "outlier_ejected"
	ReasonOverride    = "override"
	ReasonUnhealthy   = "unhealthy"
	ReasonUnreachable = "unreachable"
)

// Document is the Git-reviewed RoutingPolicy.
type Document struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Metadata   struct {
		Name    string `yaml:"name" json:"name"`
		Version string `yaml:"version" json:"version"`
	} `yaml:"metadata" json:"metadata"`
	Spec Spec `yaml:"spec" json:"spec"`
}

type Spec struct {
	Evaluation Evaluation `yaml:"evaluation" json:"evaluation"`
	Budget     Budget     `yaml:"budget" json:"budget"`
	Backends   []Backend  `yaml:"backends" json:"backends"`
	Routes     []Route    `yaml:"routes" json:"routes"`
	Overrides  []Override `yaml:"overrides" json:"overrides"`
	OnAllCaps  string     `yaml:"on_all_caps" json:"on_all_caps"`
}

type Evaluation struct {
	IntervalS      int     `yaml:"interval_s" json:"interval_s"`
	EWMAAlpha      float64 `yaml:"ewma_alpha" json:"ewma_alpha"`
	MinDwellS      int     `yaml:"min_dwell_s" json:"min_dwell_s"`
	HysteresisPct  int     `yaml:"hysteresis_pct" json:"hysteresis_pct"`
	MinProbeWeight int     `yaml:"min_probe_weight" json:"min_probe_weight"`
}

type Budget struct {
	MonthlyUSD float64 `yaml:"monthly_usd" json:"monthly_usd"`
	AlertAtPct []int   `yaml:"alert_at_pct" json:"alert_at_pct"`
	OnExceed   string  `yaml:"on_exceed" json:"on_exceed"`
}

type Backend struct {
	Name       string            `yaml:"name" json:"name"`
	Endpoint   string            `yaml:"endpoint" json:"endpoint"`
	Labels     map[string]string `yaml:"labels" json:"labels"`
	Cost       Cost              `yaml:"cost" json:"cost"`
	FreeTier   FreeTier          `yaml:"free_tier" json:"free_tier"`
	HardCapUSD float64           `yaml:"hard_cap_monthly_usd" json:"hard_cap_monthly_usd"`
	MaxWeight  int               `yaml:"max_weight" json:"max_weight"`
}

type Cost struct {
	PerMillionRequestsUSD float64 `yaml:"per_million_requests_usd" json:"per_million_requests_usd"`
	PerGBEgressUSD        float64 `yaml:"per_gb_egress_usd" json:"per_gb_egress_usd"`
}

type FreeTier struct {
	Requests     float64 `yaml:"requests" json:"requests"`
	VCPUSeconds  float64 `yaml:"vcpu_seconds" json:"vcpu_seconds"`
	GBSeconds    float64 `yaml:"gb_seconds" json:"gb_seconds"`
	GiBSeconds   float64 `yaml:"gib_seconds" json:"gib_seconds"`
	EgressGB     float64 `yaml:"egress_gb" json:"egress_gb"`
	VMHours      float64 `yaml:"vm_hours" json:"vm_hours"`
	ThresholdPct float64 `yaml:"threshold_pct" json:"threshold_pct"`
}

type Weights struct {
	WCost float64 `yaml:"w_cost" json:"w_cost"`
	WPerf float64 `yaml:"w_perf" json:"w_perf"`
	WErr  float64 `yaml:"w_err" json:"w_err"`
}

type SLOFloor struct {
	P95MS     float64 `yaml:"p95_ms" json:"p95_ms"`
	ErrorRate float64 `yaml:"error_rate" json:"error_rate"`
}

type Route struct {
	Match struct {
		PathPrefix string `yaml:"path_prefix" json:"path_prefix"`
	} `yaml:"match" json:"match"`
	Policy        string   `yaml:"policy" json:"policy"`
	Weights       Weights  `yaml:"weights" json:"weights"`
	SLOFloor      SLOFloor `yaml:"slo_floor" json:"slo_floor"`
	FallbackOrder []string `yaml:"fallback_order" json:"fallback_order"`
	Affinity      struct {
		HashOn string `yaml:"hash_on" json:"hash_on"`
	} `yaml:"affinity" json:"affinity"`
}

type Override struct {
	Route   string         `yaml:"route" json:"route"`
	Weights map[string]int `yaml:"weights" json:"weights"`
	Expires string         `yaml:"expires" json:"expires"`
}

// Observation is the smoothed view the controller has of one environment.
type Observation struct {
	P95MS             float64
	ErrorRate         float64
	Healthy           bool
	Reachable         bool
	OutlierEjected    bool
	RolloutInProgress bool
	Requests          float64
	EgressGB          float64
	VCPUSeconds       float64
	GBSeconds         float64
	VMHours           float64
	MonthCostUSD      float64
	// FreeUsedPct, when non-negative, overrides the computed allowance percentage.
	FreeUsedPct float64
}

// Input is one evaluation of one route.
type Input struct {
	Now            time.Time
	Obs            map[string]Observation
	Previous       map[string]int
	PreviousReason string
	// PrevHealthy is the last published health bit per environment.
	// A flip bypasses dwell and rollout freeze. Nil means "no flip claimed".
	PrevHealthy   map[string]bool
	LastChange    time.Time
	MonthSpendUSD float64
}

// Skip records a backend the decision did not use for primary traffic.
type Skip struct {
	Env    string `json:"env"`
	Reason string `json:"reason"`
}

// Result is the weight vector for one route.
type Result struct {
	Weights      map[string]int
	Reason       string
	Skipped      []Skip
	Scores       map[string]float64
	CapacityTier map[string]string
	FreeUsedPct  map[string]float64
	Changed      bool
	Frozen       bool
}

// LoadFile reads a RoutingPolicy YAML and stamps config_hash.
func LoadFile(path string) (Document, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Document{}, "", err
	}
	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Document{}, "", err
	}
	sum := sha256.Sum256(raw)
	return doc, hex.EncodeToString(sum[:8]), nil
}

// MatchRoute returns the longest matching prefix. The last resort is the first route.
func MatchRoute(doc Document, path string) Route {
	best := doc.Spec.Routes[0]
	bestLen := -1
	for _, route := range doc.Spec.Routes {
		prefix := route.Match.PathPrefix
		if prefix == "" {
			prefix = "/"
		}
		if len(prefix) > bestLen && hasPrefix(path, prefix) {
			best = route
			bestLen = len(prefix)
		}
	}
	return best
}

func hasPrefix(path, prefix string) bool {
	if prefix == "/" {
		return true
	}
	if len(path) < len(prefix) {
		return false
	}
	if path[:len(prefix)] != prefix {
		return false
	}
	return len(path) == len(prefix) || path[len(prefix)] == '/' || prefix[len(prefix)-1] == '/'
}

// EWMA smooths one sample. A zero previous value is treated as "no history yet".
func EWMA(previous, sample, alpha float64) float64 {
	if previous == 0 {
		return sample
	}
	if alpha <= 0 {
		return previous
	}
	if alpha > 1 {
		alpha = 1
	}
	return alpha*sample + (1-alpha)*previous
}

type row struct {
	backend Backend
	obs     Observation
	tier    string
	freePct float64
	cost    float64
	score   float64
}

// Compute assigns integer weights that sum to 100.
func Compute(doc Document, route Route, in Input) Result {
	if in.Obs == nil {
		in.Obs = map[string]Observation{}
	}
	probe := doc.Spec.Evaluation.MinProbeWeight
	if probe < 0 {
		probe = 0
	}
	rows := make([]row, 0, len(doc.Spec.Backends))
	tiers := map[string]string{}
	freePct := map[string]float64{}
	for _, backend := range doc.Spec.Backends {
		obs := in.Obs[backend.Name]
		if _, ok := in.Obs[backend.Name]; !ok {
			obs.Reachable = false
		}
		pct := freePercent(backend, obs)
		tier := "free"
		threshold := backend.FreeTier.ThresholdPct
		if threshold <= 0 {
			threshold = 80
		}
		if pct >= threshold {
			tier = "paid"
		}
		tiers[backend.Name] = tier
		freePct[backend.Name] = pct
		rows = append(rows, row{
			backend: backend,
			obs:     obs,
			tier:    tier,
			freePct: pct,
			cost:    costPerRequest(backend),
		})
	}

	eff := route.Policy
	if doc.Spec.Budget.MonthlyUSD > 0 && in.MonthSpendUSD >= doc.Spec.Budget.MonthlyUSD && doc.Spec.Budget.OnExceed != "" {
		eff = doc.Spec.Budget.OnExceed
	}
	var override *Override
	for i := range doc.Spec.Overrides {
		ov := &doc.Spec.Overrides[i]
		if ov.Route != route.Match.PathPrefix {
			continue
		}
		if ov.Expires != "" {
			exp, err := time.Parse(time.RFC3339, ov.Expires)
			if err == nil && !in.Now.Before(exp) {
				continue
			}
		}
		override = ov
		eff = ManualOverride
	}

	healthChanged := healthChanged(doc, in)
	if shouldFreeze(doc, rows, eff, healthChanged, in) {
		return Result{
			Weights:      cloneWeights(in.Previous),
			Reason:       in.PreviousReason,
			CapacityTier: tiers,
			FreeUsedPct:  freePct,
			Frozen:       true,
		}
	}

	weights := map[string]int{}
	var skipped []Skip
	var eligible []row
	for _, item := range rows {
		name := item.backend.Name
		if !item.obs.Reachable {
			skipped = append(skipped, Skip{Env: name, Reason: ReasonUnreachable})
			continue
		}
		// Every reachable backend keeps a probe share so failover and metrics stay warm.
		weights[name] = probe
		switch {
		case !item.obs.Healthy:
			skipped = append(skipped, Skip{Env: name, Reason: ReasonUnhealthy})
		case item.obs.OutlierEjected:
			skipped = append(skipped, Skip{Env: name, Reason: ReasonOutlier})
		case item.backend.HardCapUSD > 0 && item.obs.MonthCostUSD >= item.backend.HardCapUSD:
			skipped = append(skipped, Skip{Env: name, Reason: ReasonHardCap})
		case breachesSLO(route, item.obs):
			skipped = append(skipped, Skip{Env: name, Reason: ReasonSLO})
		default:
			eligible = append(eligible, item)
		}
	}
	used := sum(weights)
	if used > 100 {
		scaleInto(weights, 100)
		used = sum(weights)
	}
	remaining := 100 - used
	scores := map[string]float64{}
	reason := ReasonBestScore
	switch {
	case len(eligible) == 0:
		reason = ReasonHardCap
		target := doc.Spec.OnAllCaps
		if target != "" && remaining > 0 {
			if _, probed := weights[target]; probed || containsBackend(doc, target) {
				weights[target] += remaining
				remaining = 0
			}
		}
	case eff == ManualOverride && override != nil:
		reason = ReasonOverride
		give(weights, scaleMap(override.Weights, remaining), remaining)
	case eff == LowestCost:
		reason = ReasonCheapest
		give(weights, winnerTakeAll(sortByCost(eligible), remaining), remaining)
	case eff == FreeTierFirst:
		free := filterTier(eligible, "free")
		if len(free) > 0 {
			reason = ReasonFree
			ordered := orderBy(free, route.FallbackOrder)
			if spilled(route.FallbackOrder, rows, ordered[0].backend.Name) {
				reason = ReasonSpill
			}
			give(weights, winnerTakeAll(ordered, remaining), remaining)
		} else {
			reason = ReasonSpill
			scored := scoreRows(eligible, route.Weights)
			for _, item := range scored {
				scores[item.backend.Name] = item.score
			}
			give(weights, byInverseScore(scored, remaining), remaining)
		}
	default:
		w := route.Weights
		if eff == BestPerformance {
			w = Weights{WCost: 0, WPerf: 0.7, WErr: 0.3}
		}
		scored := scoreRows(eligible, w)
		for _, item := range scored {
			scores[item.backend.Name] = item.score
		}
		reason = ReasonBestScore
		give(weights, byInverseScore(scored, remaining), remaining)
	}
	weights = applyMax(weights, rows, remainingPool(weights))
	weights = fixSum(weights, 100)

	if !healthChanged && len(in.Previous) > 0 && eff != ManualOverride && withinHysteresis(weights, in.Previous, doc.Spec.Evaluation.HysteresisPct) {
		return Result{
			Weights:      cloneWeights(in.Previous),
			Reason:       in.PreviousReason,
			Skipped:      skipped,
			Scores:       scores,
			CapacityTier: tiers,
			FreeUsedPct:  freePct,
			Frozen:       true,
		}
	}
	changed := !sameWeights(weights, in.Previous)
	return Result{
		Weights:      weights,
		Reason:       reason,
		Skipped:      skipped,
		Scores:       scores,
		CapacityTier: tiers,
		FreeUsedPct:  freePct,
		Changed:      changed,
	}
}

func shouldFreeze(doc Document, rows []row, eff string, healthChanged bool, in Input) bool {
	if healthChanged || len(in.Previous) == 0 || eff == ManualOverride {
		return false
	}
	for _, item := range rows {
		if item.obs.RolloutInProgress {
			return true
		}
	}
	if !DwellExceeded(in.LastChange, in.Now, doc.Spec.Evaluation.MinDwellS) {
		return true
	}
	return false
}

// DwellExceeded reports whether a non-health weight change is allowed.
func DwellExceeded(last time.Time, now time.Time, minDwellS int) bool {
	if last.IsZero() || minDwellS <= 0 {
		return true
	}
	return !now.Before(last.Add(time.Duration(minDwellS) * time.Second))
}

func healthChanged(doc Document, in Input) bool {
	if len(in.Previous) == 0 {
		return true
	}
	if in.PrevHealthy == nil {
		return false
	}
	for _, backend := range doc.Spec.Backends {
		obs, ok := in.Obs[backend.Name]
		if !ok {
			continue
		}
		nowUp := obs.Healthy && obs.Reachable && !obs.OutlierEjected
		if in.PrevHealthy[backend.Name] != nowUp {
			return true
		}
	}
	return false
}

func breachesSLO(route Route, obs Observation) bool {
	if route.SLOFloor.P95MS > 0 && obs.P95MS > route.SLOFloor.P95MS {
		return true
	}
	if route.SLOFloor.ErrorRate > 0 && obs.ErrorRate > route.SLOFloor.ErrorRate {
		return true
	}
	return false
}

func freePercent(backend Backend, obs Observation) float64 {
	if obs.FreeUsedPct >= 0 && (obs.Requests == 0 && obs.EgressGB == 0 && obs.VCPUSeconds == 0 && obs.GBSeconds == 0 && obs.VMHours == 0) {
		return obs.FreeUsedPct
	}
	ft := backend.FreeTier
	var max float64
	consider := func(limit, used float64) {
		if limit <= 0 {
			return
		}
		pct := used / limit * 100
		if pct > max {
			max = pct
		}
	}
	consider(ft.Requests, obs.Requests)
	consider(ft.VCPUSeconds, obs.VCPUSeconds)
	gb := ft.GBSeconds
	if ft.GiBSeconds > 0 {
		gb = ft.GiBSeconds
	}
	consider(gb, obs.GBSeconds)
	consider(ft.EgressGB, obs.EgressGB)
	consider(ft.VMHours, obs.VMHours)
	if max == 0 && obs.FreeUsedPct > 0 {
		return obs.FreeUsedPct
	}
	return max
}

func costPerRequest(backend Backend) float64 {
	return backend.Cost.PerMillionRequestsUSD / 1_000_000
}

func scoreRows(eligible []row, w Weights) []row {
	if w.WCost == 0 && w.WPerf == 0 && w.WErr == 0 {
		w = Weights{WCost: 0.34, WPerf: 0.33, WErr: 0.33}
	}
	maxLat, maxCost := 0.0, 0.0
	for _, item := range eligible {
		if item.obs.P95MS > maxLat {
			maxLat = item.obs.P95MS
		}
		if item.cost > maxCost {
			maxCost = item.cost
		}
	}
	out := append([]row(nil), eligible...)
	for i := range out {
		normLat, normCost := 0.0, 0.0
		if maxLat > 0 {
			normLat = out[i].obs.P95MS / maxLat
		}
		if maxCost > 0 {
			normCost = out[i].cost / maxCost
		}
		score := w.WCost*normCost + w.WPerf*normLat + w.WErr*out[i].obs.ErrorRate
		if score <= 0 {
			score = 1e-9
		}
		out[i].score = score
	}
	return out
}

func byInverseScore(rows []row, remaining int) map[string]int {
	weights := map[string]int{}
	if remaining <= 0 || len(rows) == 0 {
		return weights
	}
	var sumInv float64
	inv := map[string]float64{}
	for _, item := range rows {
		value := 1 / item.score
		inv[item.backend.Name] = value
		sumInv += value
	}
	for name, value := range inv {
		weights[name] = int(math.Round(value / sumInv * float64(remaining)))
	}
	return fixSum(weights, remaining)
}

func sortByCost(eligible []row) []row {
	out := append([]row(nil), eligible...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].cost != out[j].cost {
			return out[i].cost < out[j].cost
		}
		return out[i].backend.Name < out[j].backend.Name
	})
	return out
}

// spilled reports whether an earlier fallback backend is over its free-tier threshold.
func spilled(fallback []string, rows []row, winner string) bool {
	tier := map[string]string{}
	for _, item := range rows {
		tier[item.backend.Name] = item.tier
	}
	for _, name := range fallback {
		if name == winner {
			return false
		}
		if tier[name] == "paid" {
			return true
		}
	}
	return false
}

func filterTier(eligible []row, tier string) []row {
	var out []row
	for _, item := range eligible {
		if item.tier == tier {
			out = append(out, item)
		}
	}
	return out
}

func orderBy(eligible []row, fallback []string) []row {
	rank := map[string]int{}
	for i, name := range fallback {
		rank[name] = i
	}
	out := append([]row(nil), eligible...)
	sort.Slice(out, func(i, j int) bool {
		ri, okI := rank[out[i].backend.Name]
		rj, okJ := rank[out[j].backend.Name]
		if !okI {
			ri = len(fallback) + 1
		}
		if !okJ {
			rj = len(fallback) + 1
		}
		if ri != rj {
			return ri < rj
		}
		return out[i].backend.Name < out[j].backend.Name
	})
	return out
}

func winnerTakeAll(ordered []row, remaining int) map[string]int {
	weights := map[string]int{}
	if len(ordered) == 0 || remaining <= 0 {
		return weights
	}
	left := remaining
	for _, item := range ordered {
		if left <= 0 {
			break
		}
		give := left
		max := item.backend.MaxWeight
		if max > 0 && give > max {
			give = max
		}
		weights[item.backend.Name] = give
		left -= give
		if item.backend.MaxWeight <= 0 {
			break
		}
		if give == left+give && item.backend.MaxWeight <= 0 {
			break
		}
	}
	if left > 0 {
		// Caps blocked a full assignment. Spill to the last uncapped, else the first.
		for i := len(ordered) - 1; i >= 0; i-- {
			if ordered[i].backend.MaxWeight <= 0 {
				weights[ordered[i].backend.Name] += left
				left = 0
				break
			}
		}
	}
	if left > 0 && len(ordered) > 0 {
		weights[ordered[0].backend.Name] += left
	}
	return weights
}

func give(dst, src map[string]int, remaining int) {
	for name, weight := range src {
		dst[name] += weight
	}
	_ = remaining
}

func applyMax(weights map[string]int, rows []row, _ int) map[string]int {
	maxOf := map[string]int{}
	for _, item := range rows {
		if item.backend.MaxWeight > 0 {
			maxOf[item.backend.Name] = item.backend.MaxWeight
		}
	}
	for name, max := range maxOf {
		if weights[name] > max {
			extra := weights[name] - max
			weights[name] = max
			// Give extra to the highest other weight without a tighter cap.
			best := ""
			bestW := -1
			for other, weight := range weights {
				if other == name {
					continue
				}
				if cap, ok := maxOf[other]; ok && weight >= cap {
					continue
				}
				if weight > bestW {
					best = other
					bestW = weight
				}
			}
			if best != "" {
				weights[best] += extra
			}
		}
	}
	return weights
}

func remainingPool(weights map[string]int) int { return sum(weights) }

func scaleMap(in map[string]int, total int) map[string]int {
	out := map[string]int{}
	if total <= 0 {
		return out
	}
	base := sum(in)
	if base <= 0 {
		return out
	}
	for name, weight := range in {
		out[name] = int(math.Round(float64(weight) / float64(base) * float64(total)))
	}
	return fixSum(out, total)
}

func scaleInto(weights map[string]int, total int) {
	base := sum(weights)
	if base <= 0 {
		return
	}
	for name, weight := range weights {
		weights[name] = int(math.Round(float64(weight) / float64(base) * float64(total)))
	}
	fixed := fixSum(weights, total)
	for name := range weights {
		delete(weights, name)
	}
	for name, weight := range fixed {
		weights[name] = weight
	}
}

func fixSum(weights map[string]int, total int) map[string]int {
	if weights == nil {
		weights = map[string]int{}
	}
	for name, weight := range weights {
		if weight < 0 {
			weights[name] = 0
		}
	}
	delta := total - sum(weights)
	if delta == 0 {
		return weights
	}
	names := make([]string, 0, len(weights))
	for name := range weights {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if weights[names[i]] != weights[names[j]] {
			return weights[names[i]] > weights[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) == 0 {
		return weights
	}
	weights[names[0]] += delta
	if weights[names[0]] < 0 {
		weights[names[0]] = 0
	}
	return weights
}

func sum(weights map[string]int) int {
	total := 0
	for _, weight := range weights {
		total += weight
	}
	return total
}

func withinHysteresis(next, prev map[string]int, pct int) bool {
	if pct <= 0 {
		return false
	}
	seen := map[string]bool{}
	for name := range next {
		seen[name] = true
	}
	for name := range prev {
		seen[name] = true
	}
	for name := range seen {
		diff := next[name] - prev[name]
		if diff < 0 {
			diff = -diff
		}
		if diff > pct {
			return false
		}
	}
	return true
}

func sameWeights(a, b map[string]int) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(b) == 0 {
		return false
	}
	seen := map[string]bool{}
	for name := range a {
		seen[name] = true
	}
	for name := range b {
		seen[name] = true
	}
	for name := range seen {
		if a[name] != b[name] {
			return false
		}
	}
	return true
}

func cloneWeights(in map[string]int) map[string]int {
	out := map[string]int{}
	for name, weight := range in {
		out[name] = weight
	}
	return out
}

func containsBackend(doc Document, name string) bool {
	for _, backend := range doc.Spec.Backends {
		if backend.Name == name {
			return true
		}
	}
	return false
}

// Choose picks a backend from a weight vector. unit is in [0, 1).
func Choose(weights map[string]int, unit float64) string {
	if unit < 0 {
		unit = 0
	}
	if unit >= 1 {
		unit = 0.999999
	}
	names := make([]string, 0, len(weights))
	total := 0
	for name, weight := range weights {
		if weight <= 0 {
			continue
		}
		names = append(names, name)
		total += weight
	}
	if total == 0 || len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	target := int(unit * float64(total))
	if target >= total {
		target = total - 1
	}
	acc := 0
	for _, name := range names {
		acc += weights[name]
		if target < acc {
			return name
		}
	}
	return names[len(names)-1]
}

// ChooseSticky keeps one user on one environment. key is the JWT sub.
func ChooseSticky(weights map[string]int, key string) string {
	sum := sha256.Sum256([]byte(key))
	var n uint64
	for i := 0; i < 8; i++ {
		n = n<<8 | uint64(sum[i])
	}
	unit := float64(n%100000) / 100000
	return Choose(weights, unit)
}

// HashConfig is the short config hash stored in audit records.
func HashConfig(weights map[string]int, version string) string {
	names := make([]string, 0, len(weights))
	for name := range weights {
		names = append(names, name)
	}
	sort.Strings(names)
	payload := version
	for _, name := range names {
		payload += "|" + name + ":" + itoa(weights[name])
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:4])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
