package merge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stamp(wall int64, origin string) HLC {
	return HLC{WallMs: wall, Counter: 0, Origin: origin}
}

func scalar(v any, wall int64, origin string) Scalar {
	raw, _ := json.Marshal(v)
	return Scalar{Value: raw, HLC: stamp(wall, origin)}
}

func TestFieldMergeKeepsBothClouds(t *testing.T) {
	a := EmptyDoc("user-1")
	a.Fields["display.theme"] = scalar("terminal-amber", 200, "azure-dotnet")
	a.Fields["display.time_zone"] = scalar("America/Los_Angeles", 100, "gcp-go")
	a.Tickers.Adds["a1"] = Add{Value: "TSLA", HLC: stamp(100, "gcp-go")}

	b := EmptyDoc("user-1")
	b.Fields["display.theme"] = scalar("industrial-dark", 150, "gcp-go")
	b.Fields["display.chart_period"] = scalar("1Y", 180, "aws-python")
	b.Tickers.Adds["b1"] = Add{Value: "SPCX", HLC: stamp(160, "aws-python")}

	got, err := MergeDocs(time.Time{}, a, b)
	if err != nil {
		t.Fatal(err)
	}
	pub := Project(got)
	if pub.Display["theme"] != "terminal-amber" {
		t.Fatalf("theme: %#v", pub.Display["theme"])
	}
	if pub.Display["time_zone"] != "America/Los_Angeles" {
		t.Fatalf("zone: %#v", pub.Display["time_zone"])
	}
	if pub.Display["chart_period"] != "1Y" {
		t.Fatalf("period: %#v", pub.Display["chart_period"])
	}
	if len(pub.Tickers) != 2 || pub.Tickers[0] != "TSLA" || pub.Tickers[1] != "SPCX" {
		t.Fatalf("tickers: %#v", pub.Tickers)
	}
}

func TestRemoveDropsObservedTag(t *testing.T) {
	doc := EmptyDoc("user-1")
	doc.Tickers.Adds["a1"] = Add{Value: "TSLA", HLC: stamp(100, "gcp-go")}
	doc.Tickers.Removes["a1"] = stamp(300, "aws-python")
	pub := Project(doc)
	if len(pub.Tickers) != 0 {
		t.Fatalf("tickers: %#v", pub.Tickers)
	}
}

func TestReaddUsesNewTag(t *testing.T) {
	doc := EmptyDoc("user-1")
	doc.Tickers.Adds["a1"] = Add{Value: "TSLA", HLC: stamp(100, "gcp-go")}
	doc.Tickers.Removes["a1"] = stamp(200, "aws-python")
	doc.Tickers.Adds["a2"] = Add{Value: "TSLA", HLC: stamp(300, "azure-dotnet")}
	pub := Project(doc)
	if len(pub.Tickers) != 1 || pub.Tickers[0] != "TSLA" {
		t.Fatalf("tickers: %#v", pub.Tickers)
	}
}

func TestTrimOldestTickers(t *testing.T) {
	doc := EmptyDoc("user-1")
	for i := 0; i < 26; i++ {
		value := string(rune('A'+(i%26))) + string(rune('A'+((i/26)%26)))
		// Distinct symbols T00..T25
		value = tickerName(i)
		tag := "t" + tickerName(i)
		doc.Tickers.Adds[tag] = Add{Value: value, HLC: stamp(int64(i+1), "gcp-go")}
	}
	pub := Project(doc)
	if len(pub.Tickers) != MaxTickers {
		t.Fatalf("len %d", len(pub.Tickers))
	}
	if pub.Tickers[0] == tickerName(0) {
		t.Fatalf("oldest ticker should have been trimmed, got %s", pub.Tickers[0])
	}
	if pub.Tickers[0] != tickerName(1) || pub.Tickers[len(pub.Tickers)-1] != tickerName(25) {
		t.Fatalf("tickers: %#v", pub.Tickers)
	}
}

func tickerName(i int) string {
	return "T" + string(rune('A'+i/10)) + string(rune('0'+i%10))
}

func TestApplyVersionConflictAndBothFields(t *testing.T) {
	doc := EmptyDoc("user-1")
	doc.Fields["display.theme"] = scalar("industrial-dark", 100, "gcp-go")
	doc.Tickers.Adds["a1"] = Add{Value: "TSLA", HLC: stamp(100, "gcp-go")}
	pub := Project(doc)
	pub.Display["theme"] = "midnight-slate"
	pub.Tickers = []string{"TSLA", "SPCX"}
	next, err := ApplyPublic(doc, pub, stamp(400, "azure-dotnet"))
	if err != nil {
		t.Fatal(err)
	}
	out := Project(next)
	if out.Display["theme"] != "midnight-slate" {
		t.Fatalf("theme %#v", out.Display["theme"])
	}
	if len(out.Tickers) != 2 || out.Tickers[1] != "SPCX" {
		t.Fatalf("tickers %#v", out.Tickers)
	}
	if next.Version != 1 {
		t.Fatalf("version %d", next.Version)
	}
	pub.Version = 0
	if _, err := ApplyPublic(next, pub, stamp(500, "gcp-go")); err == nil {
		t.Fatal("expected version conflict")
	}
}

func TestFutureStampRejected(t *testing.T) {
	doc := EmptyDoc("user-1")
	doc.Fields["display.theme"] = scalar("industrial-dark", time.Now().Add(time.Hour).UnixMilli(), "gcp-go")
	if _, err := MergeDocs(time.Now(), doc); err == nil {
		t.Fatal("expected future stamp error")
	}
}

func TestWriteVectors(t *testing.T) {
	a := EmptyDoc("user-1")
	a.Version = 2
	a.Fields["display.theme"] = scalar("terminal-amber", 200, "azure-dotnet")
	a.Fields["display.time_zone"] = scalar("America/Los_Angeles", 100, "gcp-go")
	a.Tickers.Adds["a1"] = Add{Value: "TSLA", HLC: stamp(100, "gcp-go")}
	a.Pinned.Adds["p1"] = Add{Value: "TSLA", HLC: stamp(110, "gcp-go")}

	b := EmptyDoc("user-1")
	b.Version = 4
	b.Fields["display.theme"] = scalar("industrial-dark", 150, "gcp-go")
	b.Fields["display.chart_period"] = scalar("1Y", 180, "aws-python")
	b.Tickers.Adds["b1"] = Add{Value: "SPCX", HLC: stamp(160, "aws-python")}

	merged, err := MergeDocs(time.Time{}, a, b)
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		Name string `json:"name"`
		Docs []Doc  `json:"docs"`
		Want Doc    `json:"want"`
	}
	vectors := []vector{{
		Name: "theme-and-ticker-from-different-clouds",
		Docs: []Doc{a, b},
		Want: merged,
	}}
	removed := EmptyDoc("user-1")
	removed.Tickers.Adds["a1"] = Add{Value: "TSLA", HLC: stamp(100, "gcp-go")}
	removed.Tickers.Removes["a1"] = stamp(300, "aws-python")
	wantRemoved := removed
	vectors = append(vectors, vector{Name: "remove-observed-tag", Docs: []Doc{removed}, Want: wantRemoved})

	raw, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "contract", "fixtures", "merge", "vectors.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := []vector{}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	got, err := MergeDocs(time.Time{}, loaded[0].Docs...)
	if err != nil {
		t.Fatal(err)
	}
	if Canonical(got) != Canonical(loaded[0].Want) {
		t.Fatalf("round trip mismatch\n%s\n%s", Canonical(got), Canonical(loaded[0].Want))
	}
}
