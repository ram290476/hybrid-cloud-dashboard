package health

import "testing"

func TestUnhealthyUntilTwoSuccesses(t *testing.T) {
	c := New()
	up, changed := c.Observe("gcp-go", true)
	if up || changed {
		t.Fatalf("first success should stay down, up=%v changed=%v", up, changed)
	}
	up, changed = c.Observe("gcp-go", true)
	if !up || !changed {
		t.Fatalf("second success should mark healthy, up=%v changed=%v", up, changed)
	}
}

func TestThreeFailuresMarkUnhealthy(t *testing.T) {
	c := New()
	c.Observe("aws-python", true)
	c.Observe("aws-python", true)
	up, _ := c.Observe("aws-python", false)
	if !up {
		t.Fatal("one failure keeps the backend healthy")
	}
	up, _ = c.Observe("aws-python", false)
	if !up {
		t.Fatal("two failures keep the backend healthy")
	}
	up, changed := c.Observe("aws-python", false)
	if up || !changed {
		t.Fatalf("third failure should eject, up=%v changed=%v", up, changed)
	}
}

func TestSuccessResetsFailureStreak(t *testing.T) {
	c := New()
	c.Observe("azure-dotnet", true)
	c.Observe("azure-dotnet", true)
	c.Observe("azure-dotnet", false)
	c.Observe("azure-dotnet", false)
	up, _ := c.Observe("azure-dotnet", true)
	if !up {
		t.Fatal("a success should reset the failure streak")
	}
	if c.States["azure-dotnet"].Failures != 0 {
		t.Fatal("failures not reset")
	}
}
