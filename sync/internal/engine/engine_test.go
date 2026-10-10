package engine

import (
	"testing"

	"github.com/ram290476/hybrid-cloud-dashboard/libs/merge"
)

func hlc(wall int64, origin string) merge.HLC {
	return merge.HLC{WallMs: wall, Origin: origin}
}

func TestFieldMergeAndEchoSuppression(t *testing.T) {
	gcp := merge.EmptyDoc("user-1")
	gcp.Fields["display.theme"] = merge.Scalar{Value: []byte(`"terminal-amber"`), HLC: hlc(200, "azure-dotnet")}
	gcp.Tickers.Adds["a1"] = merge.Add{Value: "TSLA", HLC: hlc(100, "gcp-go")}
	aws := merge.EmptyDoc("user-1")
	aws.Fields["display.time_zone"] = merge.Scalar{Value: []byte(`"America/Los_Angeles"`), HLC: hlc(180, "aws-python")}
	aws.Tickers.Adds["b1"] = merge.Add{Value: "SPCX", HLC: hlc(160, "aws-python")}
	azure := merge.EmptyDoc("user-1")
	replicas := []Replica{
		{Name: "gcp-go", Docs: map[string]merge.Doc{"user-1": gcp}},
		{Name: "aws-python", Docs: map[string]merge.Doc{"user-1": aws}},
		{Name: "azure-dotnet", Docs: map[string]merge.Doc{"user-1": azure}},
	}
	writes, err := Step(replicas)
	if err != nil {
		t.Fatal(err)
	}
	if writes != 3 {
		t.Fatalf("writes %d", writes)
	}
	for _, replica := range replicas {
		pub := merge.Project(replica.Docs["user-1"])
		if pub.Display["theme"] != "terminal-amber" || pub.Display["time_zone"] != "America/Los_Angeles" {
			t.Fatalf("%s display %#v", replica.Name, pub.Display)
		}
		if len(pub.Tickers) != 2 {
			t.Fatalf("%s tickers %#v", replica.Name, pub.Tickers)
		}
	}
	again, err := Step(replicas)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("echo should not write again, got %d", again)
	}
}

func TestObservedRemoveSurvives(t *testing.T) {
	kept := merge.EmptyDoc("user-1")
	kept.Tickers.Adds["a1"] = merge.Add{Value: "TSLA", HLC: hlc(100, "gcp-go")}
	removed := merge.EmptyDoc("user-1")
	removed.Tickers.Adds["a1"] = merge.Add{Value: "TSLA", HLC: hlc(100, "gcp-go")}
	removed.Tickers.Removes["a1"] = hlc(300, "aws-python")
	replicas := []Replica{
		{Name: "gcp-go", Docs: map[string]merge.Doc{"user-1": kept}},
		{Name: "aws-python", Docs: map[string]merge.Doc{"user-1": removed}},
	}
	if _, err := Step(replicas); err != nil {
		t.Fatal(err)
	}
	if tickers := merge.Project(replicas[0].Docs["user-1"]).Tickers; len(tickers) != 0 {
		t.Fatalf("remove should win, got %#v", tickers)
	}
}
