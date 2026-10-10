package policy

import (
	"strings"
	"testing"
	"time"
)

func doc3() Document {
	var doc Document
	doc.Metadata.Version = "2026.10.10"
	doc.Spec.Evaluation = Evaluation{IntervalS: 15, EWMAAlpha: 0.3, MinDwellS: 120, HysteresisPct: 10, MinProbeWeight: 2}
	doc.Spec.Budget = Budget{MonthlyUSD: 50, OnExceed: LowestCost}
	doc.Spec.OnAllCaps = "gcp-go"
	doc.Spec.Backends = []Backend{
		{Name: "gcp-go", Cost: Cost{PerMillionRequestsUSD: 0.40}, FreeTier: FreeTier{Requests: 2_000_000, ThresholdPct: 80}, HardCapUSD: 5, Labels: map[string]string{"lang": "go"}},
		{Name: "azure-dotnet", Cost: Cost{PerMillionRequestsUSD: 0.40}, FreeTier: FreeTier{Requests: 2_000_000, ThresholdPct: 80}, HardCapUSD: 5, Labels: map[string]string{"lang": "dotnet"}},
		{Name: "aws-python", Cost: Cost{PerMillionRequestsUSD: 0.60}, FreeTier: FreeTier{Requests: 1_000_000, ThresholdPct: 80}, HardCapUSD: 5, Labels: map[string]string{"lang": "python"}},
	}
	doc.Spec.Routes = []Route{{
		Policy:        FreeTierFirst,
		Weights:       Weights{WCost: 0.5, WPerf: 0.3, WErr: 0.2},
		SLOFloor:      SLOFloor{P95MS: 400, ErrorRate: 0.01},
		FallbackOrder: []string{"gcp-go", "azure-dotnet", "aws-python"},
	}}
	doc.Spec.Routes[0].Match.PathPrefix = "/api"
	return doc
}

func healthy(p95, errRate, freePct, cost float64) Observation {
	return Observation{P95MS: p95, ErrorRate: errRate, Healthy: true, Reachable: true, FreeUsedPct: freePct, MonthCostUSD: cost}
}

func TestWeightsArePerEnvironmentNeverPerColor(t *testing.T) {
	doc := doc3()
	in := Input{Now: time.Unix(1_700_000_000, 0), Obs: map[string]Observation{
		"gcp-go":       healthy(100, 0, 10, 0),
		"azure-dotnet": healthy(120, 0, 10, 0),
		"aws-python":   healthy(140, 0, 10, 0),
	}}
	got := Compute(doc, doc.Spec.Routes[0], in)
	if sum(got.Weights) != 100 {
		t.Fatalf("sum %d %#v", sum(got.Weights), got.Weights)
	}
	for name := range got.Weights {
		if name == "blue" || name == "green" || strings.Contains(name, "color") {
			t.Fatalf("weight key %s is a color, not an environment", name)
		}
	}
	if got.Reason != ReasonFree {
		t.Fatalf("reason %s", got.Reason)
	}
	if got.Weights["gcp-go"] <= got.Weights["aws-python"] {
		t.Fatalf("free-tier-first should prefer gcp-go, got %#v", got.Weights)
	}
}

func TestFreeTierSpill(t *testing.T) {
	doc := doc3()
	in := Input{Obs: map[string]Observation{
		"gcp-go":       healthy(100, 0, 90, 0),
		"azure-dotnet": healthy(110, 0, 10, 0),
		"aws-python":   healthy(110, 0, 10, 0),
	}}
	got := Compute(doc, doc.Spec.Routes[0], in)
	if got.Reason != ReasonSpill {
		t.Fatalf("reason %s weights %#v", got.Reason, got.Weights)
	}
	if got.Weights["azure-dotnet"] <= got.Weights["gcp-go"] {
		t.Fatalf("expected spill to azure, got %#v", got.Weights)
	}
}

func TestLowestCostSkipsSLOBreach(t *testing.T) {
	doc := doc3()
	route := doc.Spec.Routes[0]
	route.Policy = LowestCost
	in := Input{Obs: map[string]Observation{
		"gcp-go":       healthy(900, 0, 0, 0),
		"azure-dotnet": healthy(100, 0, 0, 0),
		"aws-python":   healthy(120, 0, 0, 0),
	}}
	got := Compute(doc, route, in)
	if got.Reason != ReasonCheapest {
		t.Fatalf("reason %s", got.Reason)
	}
	if got.Weights["azure-dotnet"] <= got.Weights["gcp-go"] {
		t.Fatalf("cheap but slow gcp should lose, %#v", got.Weights)
	}
	found := false
	for _, skip := range got.Skipped {
		if skip.Env == "gcp-go" && skip.Reason == ReasonSLO {
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped %#v", got.Skipped)
	}
	if got.Weights["gcp-go"] != doc.Spec.Evaluation.MinProbeWeight {
		t.Fatalf("slo skip should keep probe weight, got %d", got.Weights["gcp-go"])
	}
}

func TestHardCapProbeOnly(t *testing.T) {
	doc := doc3()
	route := doc.Spec.Routes[0]
	route.Policy = LowestCost
	in := Input{Obs: map[string]Observation{
		"gcp-go":       healthy(100, 0, 0, 9),
		"azure-dotnet": healthy(100, 0, 0, 0),
		"aws-python":   healthy(100, 0, 0, 0),
	}}
	got := Compute(doc, route, in)
	if got.Weights["gcp-go"] != 2 {
		t.Fatalf("capped backend weight %#v", got.Weights)
	}
	found := false
	for _, skip := range got.Skipped {
		if skip.Env == "gcp-go" && skip.Reason == ReasonHardCap {
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped %#v", got.Skipped)
	}
}

func TestUnhealthyGetsProbeAndBypassesDwell(t *testing.T) {
	doc := doc3()
	route := doc.Spec.Routes[0]
	route.Policy = LowestCost
	prev := map[string]int{"gcp-go": 96, "azure-dotnet": 2, "aws-python": 2}
	now := time.Unix(1_700_000_000, 0)
	in := Input{
		Now: now, LastChange: now, Previous: prev, PreviousReason: ReasonCheapest,
		PrevHealthy: map[string]bool{"gcp-go": true, "azure-dotnet": true, "aws-python": true},
		Obs: map[string]Observation{
			"gcp-go":       {P95MS: 100, Healthy: false, Reachable: true},
			"azure-dotnet": healthy(100, 0, 0, 0),
			"aws-python":   healthy(100, 0, 0, 0),
		},
	}
	got := Compute(doc, route, in)
	if got.Frozen {
		t.Fatal("health removal must bypass dwell")
	}
	if got.Weights["gcp-go"] != 2 {
		t.Fatalf("unhealthy weight %#v", got.Weights)
	}
	if got.Weights["azure-dotnet"] <= 2 {
		t.Fatalf("traffic should move, %#v", got.Weights)
	}
}

func TestHysteresisAndDwell(t *testing.T) {
	doc := doc3()
	route := doc.Spec.Routes[0]
	route.Policy = Blended
	route.Weights = Weights{WCost: 0, WPerf: 1, WErr: 0}
	prev := map[string]int{"gcp-go": 40, "azure-dotnet": 30, "aws-python": 30}
	now := time.Unix(1_700_000_000, 0)
	obs := map[string]Observation{
		"gcp-go":       healthy(100, 0, 0, 0),
		"azure-dotnet": healthy(110, 0, 0, 0),
		"aws-python":   healthy(120, 0, 0, 0),
	}
	held := Compute(doc, route, Input{Now: now, LastChange: now, Previous: prev, PreviousReason: ReasonBestScore, Obs: obs})
	if !held.Frozen {
		t.Fatal("expected dwell to hold weights")
	}
	if held.Weights["gcp-go"] != 40 {
		t.Fatalf("dwell changed weights %#v", held.Weights)
	}
	later := now.Add(3 * time.Minute)
	// Nearly equal latency so the new vector stays inside 10 points of 40/30/30.
	close := map[string]Observation{
		"gcp-go":       healthy(100, 0, 0, 0),
		"azure-dotnet": healthy(100, 0, 0, 0),
		"aws-python":   healthy(105, 0, 0, 0),
	}
	hyst := Compute(doc, route, Input{Now: later, LastChange: now, Previous: prev, PreviousReason: ReasonBestScore, Obs: close})
	if !hyst.Frozen {
		t.Fatalf("expected hysteresis to hold, got %#v", hyst.Weights)
	}
}

func TestRolloutFreezesCostShift(t *testing.T) {
	doc := doc3()
	route := doc.Spec.Routes[0]
	route.Policy = LowestCost
	prev := map[string]int{"gcp-go": 96, "azure-dotnet": 2, "aws-python": 2}
	now := time.Unix(1_700_000_000, 0).Add(time.Hour)
	obs := map[string]Observation{
		"gcp-go":       healthy(100, 0, 0, 0),
		"azure-dotnet": healthy(50, 0, 0, 0),
		"aws-python":   healthy(50, 0, 0, 0),
	}
	obs["gcp-go"] = Observation{P95MS: 100, Healthy: true, Reachable: true, RolloutInProgress: true, FreeUsedPct: -1}
	got := Compute(doc, route, Input{Now: now, LastChange: now.Add(-time.Hour), Previous: prev, PreviousReason: ReasonCheapest, Obs: obs})
	if !got.Frozen || got.Weights["gcp-go"] != 96 {
		t.Fatalf("rollout should freeze cost shift, frozen=%v weights=%#v", got.Frozen, got.Weights)
	}
}

func TestBestPerformancePrefersLowLatency(t *testing.T) {
	doc := doc3()
	doc.Spec.Evaluation.MinProbeWeight = 0
	doc.Spec.Evaluation.HysteresisPct = 0
	doc.Spec.Evaluation.MinDwellS = 0
	route := doc.Spec.Routes[0]
	route.Policy = BestPerformance
	got := Compute(doc, route, Input{Obs: map[string]Observation{
		"gcp-go":       healthy(300, 0, 0, 0),
		"azure-dotnet": healthy(100, 0, 0, 0),
		"aws-python":   healthy(200, 0, 0, 0),
	}})
	if got.Weights["azure-dotnet"] <= got.Weights["gcp-go"] {
		t.Fatalf("best performance weights %#v", got.Weights)
	}
	if sum(got.Weights) != 100 {
		t.Fatalf("sum %d", sum(got.Weights))
	}
}

func TestStickyChoiceIsStable(t *testing.T) {
	weights := map[string]int{"gcp-go": 50, "azure-dotnet": 30, "aws-python": 20}
	first := ChooseSticky(weights, "user-sub-1")
	for i := 0; i < 5; i++ {
		if got := ChooseSticky(weights, "user-sub-1"); got != first {
			t.Fatalf("sticky changed %s -> %s", first, got)
		}
	}
	if first == "" {
		t.Fatal("empty choice")
	}
}

func TestManualOverride(t *testing.T) {
	doc := doc3()
	doc.Spec.Overrides = []Override{{
		Route:   "/api",
		Weights: map[string]int{"aws-python": 100},
		Expires: time.Unix(1_800_000_000, 0).UTC().Format(time.RFC3339),
	}}
	got := Compute(doc, doc.Spec.Routes[0], Input{
		Now: time.Unix(1_700_000_000, 0),
		Obs: map[string]Observation{
			"gcp-go": healthy(10, 0, 0, 0), "azure-dotnet": healthy(10, 0, 0, 0), "aws-python": healthy(10, 0, 0, 0),
		},
	})
	if got.Reason != ReasonOverride || got.Weights["aws-python"] < 90 {
		t.Fatalf("override %#v reason %s", got.Weights, got.Reason)
	}
}

func TestEWMA(t *testing.T) {
	if got := EWMA(0, 100, 0.3); got != 100 {
		t.Fatalf("bootstrap %v", got)
	}
	got := EWMA(100, 200, 0.3)
	if got < 129 || got > 131 {
		t.Fatalf("ewma %v", got)
	}
}

func TestMatchLongestPrefix(t *testing.T) {
	doc := doc3()
	prefs := doc.Spec.Routes[0]
	prefs.Match.PathPrefix = "/api/v1/prefs"
	prefs.Policy = Blended
	root := doc.Spec.Routes[0]
	root.Match.PathPrefix = "/"
	root.Policy = LowestCost
	doc.Spec.Routes = []Route{root, doc.Spec.Routes[0], prefs}
	if MatchRoute(doc, "/api/v1/prefs").Policy != Blended {
		t.Fatal("prefs route")
	}
	if MatchRoute(doc, "/api/v1/status").Match.PathPrefix != "/api" {
		t.Fatal("api route")
	}
	if MatchRoute(doc, "/healthz").Policy != LowestCost {
		t.Fatal("root route")
	}
}
