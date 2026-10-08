package cards

import (
	"bytes"
	"encoding/gob"
	"sort"
)

// Canonical, deterministic gob encoding for the IR cache and segment files.
//
// encoding/gob walks every map with reflect.MapRange(), which is unordered
// (encoding/gob does not sort map keys), so a Card tree carrying
// Params/SVars maps encoded to different bytes on every run: two
// CompileDir+Save runs over the same corpus produced different .gob.gz (and
// .seg) bytes and even different file sizes. The decoded registry was always
// equal, so this was a cache-hygiene/determinism defect only.
//
// The map-bearing types (SA, Trigger, Static, Repl, Face) and cacheFile
// therefore carry explicit GobEncode/GobDecode pairs that emit every map as a
// key-sorted []kv. The methods are on the pointer receiver, so gob uses them
// for both pointer fields (`Sub *SA`, `[]*SA`, `[]*Face`) and value elements
// (`[]Trigger`, `[]Static`, `[]Repl`) -- gob detects a pointer-only GobEncoder
// via indir == -1 and takes the address of the addressable value.
//
// A wire struct handed to gob must list EVERY exported field of its type:
// once GobEncode is defined, gob no longer reflects over the live struct, so
// a field omitted here is silently dropped from the cache.
//
// Adding a new exported, gob-encoded field to one of these types means adding
// it to the matching wire struct below as well.

// kv is one key/value entry of a canonicalised map, in sorted-key order.
type kv struct {
	K string
	V string
}

// sortedKV returns m's entries sorted by key. A nil map yields a nil slice,
// which decodes back to a nil map (gob encodes nil and empty slices
// identically, so the distinction is preserved by the mapFromKV nil check).
func sortedKV(m map[string]string) []kv {
	if m == nil {
		return nil
	}
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// mapFromKV rebuilds the map a sortedKV slice represents.
func mapFromKV(p []kv) map[string]string {
	if p == nil {
		return nil
	}
	m := make(map[string]string, len(p))
	for _, e := range p {
		m[e.K] = e.V
	}
	return m
}

// saWire canonically encodes an SA. Sub is an *SA, so its own GobEncode runs
// recursively.
type saWire struct {
	Kind   string
	API    string
	Params []kv
	Sub    *SA
	Line   string
}

// GobEncode encodes s with its Params map in sorted key order.
func (s *SA) GobEncode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(saWire{
		Kind: s.Kind, API: s.API, Params: sortedKV(s.Params), Sub: s.Sub, Line: s.Line,
	})
	return b.Bytes(), err
}

// GobDecode restores an SA encoded by GobEncode.
func (s *SA) GobDecode(data []byte) error {
	var w saWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return err
	}
	s.Kind, s.API, s.Sub, s.Line = w.Kind, w.API, w.Sub, w.Line
	s.Params = mapFromKV(w.Params)
	return nil
}

// triggerWire canonically encodes a Trigger. Effect is an *SA.
type triggerWire struct {
	Mode   string
	Params []kv
	Effect *SA
}

// GobEncode encodes t with its Params map in sorted key order.
func (t *Trigger) GobEncode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(triggerWire{Mode: t.Mode, Params: sortedKV(t.Params), Effect: t.Effect})
	return b.Bytes(), err
}

// GobDecode restores a Trigger encoded by GobEncode.
func (t *Trigger) GobDecode(data []byte) error {
	var w triggerWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return err
	}
	t.Mode, t.Effect = w.Mode, w.Effect
	t.Params = mapFromKV(w.Params)
	return nil
}

// staticWire canonically encodes a Static.
type staticWire struct {
	Mode   string
	Params []kv
}

// GobEncode encodes s with its Params map in sorted key order.
func (s *Static) GobEncode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(staticWire{Mode: s.Mode, Params: sortedKV(s.Params)})
	return b.Bytes(), err
}

// GobDecode restores a Static encoded by GobEncode.
func (s *Static) GobDecode(data []byte) error {
	var w staticWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return err
	}
	s.Mode = w.Mode
	s.Params = mapFromKV(w.Params)
	return nil
}

// replWire canonically encodes a Repl. With is an *SA.
type replWire struct {
	Event  string
	Params []kv
	With   *SA
}

// GobEncode encodes r with its Params map in sorted key order.
func (r *Repl) GobEncode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(replWire{Event: r.Event, Params: sortedKV(r.Params), With: r.With})
	return b.Bytes(), err
}

// GobDecode restores a Repl encoded by GobEncode.
func (r *Repl) GobDecode(data []byte) error {
	var w replWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return err
	}
	r.Event, r.With = w.Event, w.With
	r.Params = mapFromKV(w.Params)
	return nil
}

// faceWire canonically encodes a Face. Every exported Face field is listed
// here; the unexported derived fields are rebuilt by derive/link after decode,
// exactly as they are on the existing gob path.
type faceWire struct {
	SpecializeColor string
	CopyFaceFrom    string
	Name            string
	ManaCost        string
	Types           []string
	PT              string
	Loyalty         string
	Defense         string
	Colors          string
	Oracle          string
	Keywords        []string
	Aliases         []string
	Abilities       []*SA
	Triggers        []Trigger
	Statics         []Static
	Repls           []Repl
	SVars           []kv
}

// GobEncode encodes f with its SVars map in sorted key order.
func (f *Face) GobEncode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(faceWire{
		SpecializeColor: f.SpecializeColor,
		CopyFaceFrom:    f.CopyFaceFrom,
		Name:            f.Name,
		ManaCost:        f.ManaCost,
		Types:           f.Types,
		PT:              f.PT,
		Loyalty:         f.Loyalty,
		Defense:         f.Defense,
		Colors:          f.Colors,
		Oracle:          f.Oracle,
		Keywords:        f.Keywords,
		Aliases:         f.Aliases,
		Abilities:       f.Abilities,
		Triggers:        f.Triggers,
		Statics:         f.Statics,
		Repls:           f.Repls,
		SVars:           sortedKV(f.SVars),
	})
	return b.Bytes(), err
}

// GobDecode restores a Face encoded by GobEncode.
func (f *Face) GobDecode(data []byte) error {
	var w faceWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return err
	}
	f.SpecializeColor = w.SpecializeColor
	f.CopyFaceFrom = w.CopyFaceFrom
	f.Name = w.Name
	f.ManaCost = w.ManaCost
	f.Types = w.Types
	f.PT = w.PT
	f.Loyalty = w.Loyalty
	f.Defense = w.Defense
	f.Colors = w.Colors
	f.Oracle = w.Oracle
	f.Keywords = w.Keywords
	f.Aliases = w.Aliases
	f.Abilities = w.Abilities
	f.Triggers = w.Triggers
	f.Statics = w.Statics
	f.Repls = w.Repls
	f.SVars = mapFromKV(w.SVars)
	return nil
}

// tokenEntry is one corpus token in the canonical cacheFile encoding: the
// keyword under which Registry.Tokens holds the card, paired with the card.
type tokenEntry struct {
	Key  string
	Card *Card
}

// cacheFileWire canonically encodes the on-disk cacheFile. Tokens becomes a
// key-sorted []tokenEntry (the same ordering writeSegments already uses for
// segTokens.Keys), so the whole cache is byte-identical across runs.
type cacheFileWire struct {
	Version int
	Cards   []*Card
	Tokens  []tokenEntry
}

// GobEncode encodes cf with its Tokens map in sorted key order.
func (cf *cacheFile) GobEncode() ([]byte, error) {
	w := cacheFileWire{Version: cf.Version, Cards: cf.Cards}
	if cf.Tokens != nil {
		w.Tokens = make([]tokenEntry, 0, len(cf.Tokens))
		for k, c := range cf.Tokens {
			w.Tokens = append(w.Tokens, tokenEntry{Key: k, Card: c})
		}
		sort.Slice(w.Tokens, func(i, j int) bool { return w.Tokens[i].Key < w.Tokens[j].Key })
	}
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(w)
	return b.Bytes(), err
}

// GobDecode restores a cacheFile encoded by GobEncode.
func (cf *cacheFile) GobDecode(data []byte) error {
	var w cacheFileWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&w); err != nil {
		return err
	}
	cf.Version = w.Version
	cf.Cards = w.Cards
	if w.Tokens == nil {
		cf.Tokens = nil
		return nil
	}
	cf.Tokens = make(map[string]*Card, len(w.Tokens))
	for _, e := range w.Tokens {
		cf.Tokens[e.Key] = e.Card
	}
	return nil
}
