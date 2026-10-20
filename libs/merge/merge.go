// Package merge is the field-level preference merge shared by the backends and hcd-sync.
//
// Scalar fields (theme, time zone, chart period, per-ticker chart settings) use a
// hybrid logical clock: the higher stamp wins, so a theme change in one cloud and a
// ticker change in another both survive. Watchlist tickers and pins are an OR-set:
// adds carry a unique tag, removes name the tags they observed. After merging, the
// 25-ticker and 6-pin limits drop the oldest adds so every cloud trims the same way.
package merge

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const (
	MaxTickers = 25
	MaxPinned  = 6
	futureSkew = 5 * time.Minute
)

// HLC is a hybrid logical clock stamp. Comparison is wall_ms, then counter, then origin.
type HLC struct {
	WallMs  int64  `json:"wall_ms"`
	Counter int64  `json:"counter"`
	Origin  string `json:"origin"`
}

// Cmp returns -1, 0, or 1.
func (h HLC) Cmp(o HLC) int {
	if h.WallMs != o.WallMs {
		if h.WallMs < o.WallMs {
			return -1
		}
		return 1
	}
	if h.Counter != o.Counter {
		if h.Counter < o.Counter {
			return -1
		}
		return 1
	}
	if h.Origin != o.Origin {
		if h.Origin < o.Origin {
			return -1
		}
		return 1
	}
	return 0
}

// Scalar is a last-writer-wins field. Tombstone drops the field when its stamp wins.
type Scalar struct {
	Value     json.RawMessage `json:"value"`
	HLC       HLC             `json:"hlc"`
	Tombstone bool            `json:"tombstone,omitempty"`
}

// Add is one OR-set addition.
type Add struct {
	Value string `json:"value"`
	HLC   HLC    `json:"hlc"`
}

// ORSet is an observed-remove set. A tag is present when it has an add and no remove.
type ORSet struct {
	Adds    map[string]Add `json:"adds"`
	Removes map[string]HLC `json:"removes"`
}

// Doc is the stored preference document, keyed by Cognito sub.
type Doc struct {
	UserSub string            `json:"user_sub"`
	Fields  map[string]Scalar `json:"fields"`
	Tickers ORSet             `json:"tickers"`
	Pinned  ORSet             `json:"pinned"`
	Version int64             `json:"version"`
}

// Public is the API shape the investor-dashboard frontend already sends.
type Public struct {
	Tickers       []string                  `json:"tickers"`
	Pinned        []string                  `json:"pinned"`
	Display       map[string]any            `json:"display"`
	ChartSettings map[string]map[string]any `json:"chart_settings"`
	Version       int64                     `json:"version"`
	UpdatedAt     string                    `json:"updated_at,omitempty"`
}

// EmptyDoc returns a document with empty maps so JSON stays stable.
func EmptyDoc(sub string) Doc {
	return Doc{
		UserSub: sub,
		Fields:  map[string]Scalar{},
		Tickers: ORSet{Adds: map[string]Add{}, Removes: map[string]HLC{}},
		Pinned:  ORSet{Adds: map[string]Add{}, Removes: map[string]HLC{}},
	}
}

func (d *Doc) normalize() {
	if d.Fields == nil {
		d.Fields = map[string]Scalar{}
	}
	if d.Tickers.Adds == nil {
		d.Tickers.Adds = map[string]Add{}
	}
	if d.Tickers.Removes == nil {
		d.Tickers.Removes = map[string]HLC{}
	}
	if d.Pinned.Adds == nil {
		d.Pinned.Adds = map[string]Add{}
	}
	if d.Pinned.Removes == nil {
		d.Pinned.Removes = map[string]HLC{}
	}
}

// MergeDocs folds every replica into one document. now == 0 disables the future-stamp check.
func MergeDocs(now time.Time, docs ...Doc) (Doc, error) {
	out := EmptyDoc("")
	for _, doc := range docs {
		doc.normalize()
		if err := rejectFuture(now, doc); err != nil {
			return Doc{}, err
		}
		if doc.UserSub != "" {
			out.UserSub = doc.UserSub
		}
		if doc.Version > out.Version {
			out.Version = doc.Version
		}
		for key, field := range doc.Fields {
			cur, ok := out.Fields[key]
			if !ok || field.HLC.Cmp(cur.HLC) > 0 {
				out.Fields[key] = field
			}
		}
		mergeSet(&out.Tickers, doc.Tickers)
		mergeSet(&out.Pinned, doc.Pinned)
	}
	return out, nil
}

func rejectFuture(now time.Time, doc Doc) error {
	if now.IsZero() {
		return nil
	}
	limit := now.Add(futureSkew).UnixMilli()
	check := func(h HLC) error {
		if h.WallMs > limit {
			return fmt.Errorf("hlc from %s is more than %s ahead of now", h.Origin, futureSkew)
		}
		return nil
	}
	for _, f := range doc.Fields {
		if err := check(f.HLC); err != nil {
			return err
		}
	}
	for _, a := range doc.Tickers.Adds {
		if err := check(a.HLC); err != nil {
			return err
		}
	}
	for _, h := range doc.Tickers.Removes {
		if err := check(h); err != nil {
			return err
		}
	}
	for _, a := range doc.Pinned.Adds {
		if err := check(a.HLC); err != nil {
			return err
		}
	}
	for _, h := range doc.Pinned.Removes {
		if err := check(h); err != nil {
			return err
		}
	}
	return nil
}

func mergeSet(dst *ORSet, src ORSet) {
	for tag, add := range src.Adds {
		if _, ok := dst.Adds[tag]; !ok {
			dst.Adds[tag] = add
		}
	}
	for tag, hlc := range src.Removes {
		cur, ok := dst.Removes[tag]
		if !ok || hlc.Cmp(cur) > 0 {
			dst.Removes[tag] = hlc
		}
	}
}

type visible struct {
	tag   string
	value string
	hlc   HLC
}

func present(set ORSet) []visible {
	var items []visible
	for tag, add := range set.Adds {
		if _, removed := set.Removes[tag]; removed {
			continue
		}
		items = append(items, visible{tag: tag, value: add.Value, hlc: add.HLC})
	}
	sort.Slice(items, func(i, j int) bool {
		c := items[i].hlc.Cmp(items[j].hlc)
		if c != 0 {
			return c < 0
		}
		return items[i].tag < items[j].tag
	})
	// One visible row per value. A newer surviving tag wins; order follows the winner's stamp.
	best := map[string]visible{}
	for _, item := range items {
		cur, ok := best[item.value]
		if !ok || item.hlc.Cmp(cur.hlc) > 0 {
			best[item.value] = item
		}
	}
	out := make([]visible, 0, len(best))
	for _, item := range best {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		c := out[i].hlc.Cmp(out[j].hlc)
		if c != 0 {
			return c < 0
		}
		return out[i].tag < out[j].tag
	})
	return out
}

func trimOldest(items []visible, max int) []visible {
	if max >= 0 && len(items) > max {
		items = items[len(items)-max:]
	}
	return items
}

// Project turns the stored document into the public prefs object.
func Project(doc Doc) Public {
	doc.normalize()
	tickers := trimOldest(present(doc.Tickers), MaxTickers)
	allowed := map[string]bool{}
	names := make([]string, 0, len(tickers))
	for _, item := range tickers {
		allowed[item.value] = true
		names = append(names, item.value)
	}
	var pins []visible
	for _, item := range present(doc.Pinned) {
		if allowed[item.value] {
			pins = append(pins, item)
		}
	}
	pins = trimOldest(pins, MaxPinned)
	pinNames := make([]string, 0, len(pins))
	for _, item := range pins {
		pinNames = append(pinNames, item.value)
	}
	display := map[string]any{}
	charts := map[string]map[string]any{}
	var updated int64
	for key, field := range doc.Fields {
		if field.Tombstone {
			continue
		}
		if field.HLC.WallMs > updated {
			updated = field.HLC.WallMs
		}
		var value any
		if len(field.Value) > 0 {
			_ = json.Unmarshal(field.Value, &value)
		}
		switch {
		case len(key) > 8 && key[:8] == "display.":
			display[key[8:]] = value
		case len(key) > 6 && key[:6] == "chart.":
			ticker := key[6:]
			if m, ok := value.(map[string]any); ok {
				charts[ticker] = m
			}
		}
	}
	if display["theme"] == nil {
		display["theme"] = "industrial-dark"
	}
	if display["updown_palette"] == nil {
		display["updown_palette"] = "green-red"
	}
	if display["chart_period"] == nil {
		display["chart_period"] = "1M"
	}
	if display["time_zone"] == nil {
		display["time_zone"] = "America/New_York"
	}
	pub := Public{
		Tickers:       names,
		Pinned:        pinNames,
		Display:       display,
		ChartSettings: charts,
		Version:       doc.Version,
	}
	if updated > 0 {
		pub.UpdatedAt = time.UnixMilli(updated).UTC().Format(time.RFC3339)
	}
	if pub.Tickers == nil {
		pub.Tickers = []string{}
	}
	if pub.Pinned == nil {
		pub.Pinned = []string{}
	}
	return pub
}

// ApplyPublic merges a user write into the stored document.
// The caller's version must match. Changed fields receive stamp.
func ApplyPublic(doc Doc, incoming Public, stamp HLC) (Doc, error) {
	doc.normalize()
	if incoming.Version != doc.Version {
		return Doc{}, fmt.Errorf("version conflict: have %d sent %d", doc.Version, incoming.Version)
	}
	current := Project(doc)
	doc.Version++
	if incoming.Display != nil {
		for _, key := range []string{"theme", "time_zone", "updown_palette", "chart_period", "panels", "panel_mode"} {
			next, ok := incoming.Display[key]
			if !ok {
				continue
			}
			prev := current.Display[key]
			if !jsonEqual(prev, next) {
				raw, err := json.Marshal(next)
				if err != nil {
					return Doc{}, err
				}
				doc.Fields["display."+key] = Scalar{Value: raw, HLC: stamp}
			}
		}
	}
	if incoming.ChartSettings != nil {
		seen := map[string]bool{}
		for ticker, settings := range incoming.ChartSettings {
			seen[ticker] = true
			prev := current.ChartSettings[ticker]
			if !jsonEqual(prev, settings) {
				raw, _ := json.Marshal(settings)
				doc.Fields["chart."+ticker] = Scalar{Value: raw, HLC: stamp}
			}
		}
		for ticker := range current.ChartSettings {
			if !seen[ticker] {
				doc.Fields["chart."+ticker] = Scalar{Tombstone: true, HLC: stamp}
			}
		}
	}
	if err := applySet(&doc.Tickers, current.Tickers, incoming.Tickers, stamp); err != nil {
		return Doc{}, err
	}
	if err := applySet(&doc.Pinned, current.Pinned, incoming.Pinned, stamp); err != nil {
		return Doc{}, err
	}
	return doc, nil
}

func applySet(set *ORSet, before, after []string, stamp HLC) error {
	prev := map[string]bool{}
	for _, v := range before {
		prev[v] = true
	}
	next := map[string]bool{}
	for _, v := range after {
		next[v] = true
	}
	for value := range prev {
		if next[value] {
			continue
		}
		for tag, add := range set.Adds {
			if add.Value == value {
				if _, removed := set.Removes[tag]; !removed {
					set.Removes[tag] = stamp
				}
			}
		}
	}
	for value := range next {
		if prev[value] {
			continue
		}
		tag := newTag(stamp, value)
		set.Adds[tag] = Add{Value: value, HLC: stamp}
	}
	return nil
}

func newTag(stamp HLC, value string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%s", stamp.Origin, stamp.WallMs, stamp.Counter, value)))
	return hex.EncodeToString(sum[:8])
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// Canonical JSON for equality checks and echo suppression.
func Canonical(doc Doc) string {
	doc.normalize()
	raw, _ := json.Marshal(doc)
	return string(raw)
}
