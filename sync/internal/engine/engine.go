// Package engine merges preference documents across the three clouds.
// A replica whose document already matches the merge is left alone, which
// suppresses the echo of a write this process just pushed.
package engine

import (
	"sort"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/libs/merge"
)

// Replica is one cloud's copy of the preference documents, keyed by Cognito sub.
type Replica struct {
	Name string
	Docs map[string]merge.Doc
}

// Step folds every sub and writes the result back only where it differs.
// The returned count is the number of replica writes. A second call with the
// same inputs returns 0.
func Step(replicas []Replica) (int, error) {
	names := map[string]struct{}{}
	for _, replica := range replicas {
		for sub := range replica.Docs {
			names[sub] = struct{}{}
		}
	}
	subs := make([]string, 0, len(names))
	for sub := range names {
		subs = append(subs, sub)
	}
	sort.Strings(subs)
	writes := 0
	for _, sub := range subs {
		var docs []merge.Doc
		for _, replica := range replicas {
			if doc, ok := replica.Docs[sub]; ok {
				docs = append(docs, doc)
			}
		}
		merged, err := merge.MergeDocs(time.Time{}, docs...)
		if err != nil {
			return writes, err
		}
		merged.UserSub = sub
		canon := merge.Canonical(merged)
		for i := range replicas {
			local, ok := replicas[i].Docs[sub]
			if ok && merge.Canonical(local) == canon {
				continue
			}
			replicas[i].Docs[sub] = merged
			writes++
		}
	}
	return writes, nil
}
