package policy

import "testing"

func TestCheckedInPoliciesLoad(t *testing.T) {
	for _, path := range []string{"routing-policy.yaml", "routing-policy.local.yaml", "routing-policy.kind.yaml"} {
		doc, hash, err := LoadFile(path)
		if err != nil {
			t.Fatal(path, err)
		}
		if hash == "" || len(doc.Spec.Backends) != 3 || len(doc.Spec.Routes) != 2 {
			t.Fatalf("%s backends %d routes %d", path, len(doc.Spec.Backends), len(doc.Spec.Routes))
		}
		for _, backend := range doc.Spec.Backends {
			if backend.Endpoint == "" {
				t.Fatalf("%s missing endpoint", backend.Name)
			}
		}
	}
}
