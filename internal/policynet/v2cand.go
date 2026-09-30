// The v2 candidate table (P§3.5): the shared encoding of one scored
// alternative. The intermediate V2SemCand carries the semantic FIELDS the
// spellbench v2 protocol poses (the "kind" plus its typed companions); the
// V front end decodes them from a wire Candidate's fields, the G side
// mirrors the per-option semantic construction of the gorge adapter
// (v2engine, the D§6 mapping run in reverse), and the encoder to the fixed
// row shape is single-sourced here.
//
// A candidate's pointer references are encoded twice: as dense scalar slots
// (row / 64, clamped, 0 = none) and as the exact V2Cand.Refs list the
// network gathers row embeddings from. A card row is positive (1 + row
// index), a player row negative (-(1 + player index)).

package policynet

import (
	"encoding/json"
	"strconv"
	"strings"
)

// V2CandKinds is the candidate-kind vocabulary, in one-hot order. "other"
// takes any kind outside the table.
var V2CandKinds = []string{"pass", "play_land", "cast_spell", "activate_mana_ability",
	"activate_ability", "special_action", "declare_attack", "declare_block",
	"finish_target_selection", "finish_selection", "choose_spell_mode", "choose_option",
	"choose_target", "choose_cost_target", "select_object", "choose_number",
	"choose_color", "choose_boolean", "choose_name", "optional_cost", "order_pick",
	"arrange_card", "other"}

// V2CandKindIndex returns a kind's one-hot offset.
func V2CandKindIndex(k string) int {
	for i, s := range V2CandKinds {
		if s == k {
			return i
		}
	}
	return len(V2CandKinds) - 1
}

// V2CandWidth is the dense scalar width of one candidate row.
const V2CandWidth = 47

// Named offsets into the candidate row.
const (
	v2kKindBase = 0 // 23 kind one-hots
	// 23..33 scalar slots
	v2kValue     = 23 // value / 8, clamped [-3, 3]
	v2kMin       = 24
	v2kMax       = 25
	v2kSelCount  = 26
	v2kModeIdx   = 27
	v2kModeCount = 28
	v2kSlot      = 29
	v2kOptIdx    = 30
	v2kOptCount  = 31
	v2kPosition  = 32
	v2kCount     = 33
	// 34..39 colour one-hot (W U B R G) + absent
	v2kColorBase = 34
	v2kPay       = 40
	v2kKeep      = 41
	v2kCastIt    = 42
	// 43..46 four pointer slots
	v2kRefBase = 43
)

// V2SemRef is one semantic pointer reference: a player seat ("p0") or an
// object id ("card-..."), never both.
type V2SemRef struct {
	Player string
	Object string
}

// V2SemCand is the neutral semantic of one candidate.
type V2SemCand struct {
	Kind string
	Refs []V2SemRef

	Method, Purpose, Action, Cost, CostKind, Destination, Event, Color string

	Value    int64
	ValueS   string
	ValueStr bool // the value is the string ValueS (a name, a label)

	Min, Max, SelCount, ModeIdx, ModeCount, Slot, OptIdx, OptCount, Position, Count int64

	Pay, Keep, CastIt *bool

	Name string // option_label / source_name
}

// V2RefResolver resolves a semantic reference to a table row index.
type V2RefResolver interface {
	// V2RefObject resolves an object id to a CARD row (return 1 + index, or
	// 0 when the object has no row).
	V2RefObject(objectID string) int32
	// V2RefPlayer resolves a seat word ("p0", "p1") to a PLAYER row
	// (return -(1 + index), or 0 when unknown).
	V2RefPlayer(seat string) int32
}

// V2Cand is one encoded candidate row.
type V2Cand struct {
	Raw  []float32
	Refs []int32
	Rows []Feature
}

// V2CandsEncode encodes semantics to rows. THE single home of the candidate
// encoding; both front ends call this.
func V2CandsEncode(sem []V2SemCand, res V2RefResolver) []V2Cand {
	out := make([]V2Cand, 0, len(sem))
	for i := range sem {
		out = append(out, v2CandEncode(&sem[i], res))
	}
	return out
}

func v2CandEncode(sc *V2SemCand, res V2RefResolver) V2Cand {
	raw := make([]float32, V2CandWidth)
	raw[v2kKindBase+V2CandKindIndex(sc.Kind)] = 1
	put := func(off int, v int64, div float32, sat bool) {
		f := float32(v) / div
		if sat {
			raw[off] = v2clamp(f, -3, 3)
		} else {
			raw[off] = v2clamp(f, 0, 3)
		}
	}
	put(v2kValue, sc.Value, 8, true)
	put(v2kMin, sc.Min, 8, false)
	put(v2kMax, sc.Max, 8, false)
	put(v2kSelCount, sc.SelCount, 4, false)
	put(v2kModeIdx, sc.ModeIdx, 8, false)
	put(v2kModeCount, sc.ModeCount, 8, false)
	put(v2kSlot, sc.Slot, 4, false)
	put(v2kOptIdx, sc.OptIdx, 8, false)
	put(v2kOptCount, sc.OptCount, 8, false)
	put(v2kPosition, sc.Position, 8, false)
	put(v2kCount, sc.Count, 8, false)
	if c := optionColorSym(sc.Color); c >= 0 {
		raw[v2kColorBase+c] = 1
	}
	if sc.Pay != nil && *sc.Pay {
		raw[v2kPay] = 1
	}
	if sc.Keep != nil && *sc.Keep {
		raw[v2kKeep] = 1
	}
	if sc.CastIt != nil && *sc.CastIt {
		raw[v2kCastIt] = 1
	}
	// Refs: the first four semantic refs fill the dense slots (source-like
	// first is the producers' contract) and all of them ride Refs.
	var refs []int32
	var rows []Feature
	for i, r := range sc.Refs {
		ref := int32(0)
		switch {
		case r.Object != "" && res != nil:
			ref = res.V2RefObject(r.Object)
		case r.Player != "" && res != nil:
			ref = res.V2RefPlayer(r.Player)
		}
		if ref != 0 {
			if i < 4 {
				raw[v2kRefBase+i] = v2clamp(float32(abs32(ref))/64, 0, 3)
			}
			refs = append(refs, ref)
		}
	}
	// Sparse string rows.
	push := func(k, v string) {
		if v != "" {
			rows = append(rows, Feature{Row: hashID("v2|" + k + "|" + v), Value: 1})
		}
	}
	push("method", sc.Method)
	push("purpose", sc.Purpose)
	push("action", sc.Action)
	push("cost", sc.Cost)
	push("cost_kind", sc.CostKind)
	push("destination", sc.Destination)
	push("event", sc.Event)
	if sc.ValueStr {
		push("value_s", sc.ValueS)
	}
	push("candname", sc.Name)
	sortFeatures(rows)
	return V2Cand{Raw: raw, Refs: refs, Rows: rows}
}

// abs32 is |x| without branches on the sign's semantics.
func abs32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}

// optionColorSym maps a colour word to its one-hot offset (W U B R G), -1
// when unknown.
func optionColorSym(c string) int {
	switch strings.ToUpper(strings.TrimSpace(c)) {
	case "W", "WHITE":
		return 0
	case "U", "BLUE":
		return 1
	case "B", "BLACK":
		return 2
	case "R", "RED":
		return 3
	case "G", "GREEN":
		return 4
	}
	return -1
}

// ---------------------------------------------------------------------------
// Wire decode: a v2agent.Candidate's semantic fields -> V2SemCand. The
// protocol's lenient reading applies (spec 4.2): unknown fields are
// ignored, mistyped values leave their slot unset.

// V2SemCandFromFields decodes one wire candidate.
func V2SemCandFromFields(fields map[string]json.RawMessage) (V2SemCand, error) {
	var sc V2SemCand
	var err error
	get := func(k string) (json.RawMessage, bool) {
		r, ok := fields[k]
		return r, ok && len(r) > 0 && string(r) != "null"
	}
	if r, ok := get("kind"); ok {
		if err = json.Unmarshal(r, &sc.Kind); err != nil {
			return sc, err
		}
	}
	if r, ok := get("source"); ok {
		if ref, e := v2SemRefFromJSON(r); e != nil {
			return sc, e
		} else if ref != nil {
			sc.Refs = append(sc.Refs, *ref)
		}
	}
	// The pointer-bearing companions, in wire order.
	for _, k := range []string{"target", "candidate", "choice", "card", "attacker", "defender", "item"} {
		r, ok := get(k)
		if !ok {
			continue
		}
		if k == "item" {
			// order_pick/arrange items nest the ref one level deeper.
			var m map[string]json.RawMessage
			if err = json.Unmarshal(r, &m); err != nil {
				return sc, err
			}
			for _, sub := range []string{"trigger", "object", "card"} {
				if sr, ok := m[sub]; ok {
					if ref, e := v2SemRefFromJSON(sr); e != nil {
						return sc, e
					} else if ref != nil {
						sc.Refs = append(sc.Refs, *ref)
					}
					break
				}
			}
			continue
		}
		if ref, e := v2SemRefFromJSON(r); e != nil {
			return sc, e
		} else if ref != nil {
			sc.Refs = append(sc.Refs, *ref)
		}
	}
	strs := map[string]*string{
		"method": &sc.Method, "purpose": &sc.Purpose, "action": &sc.Action,
		"cost": &sc.Cost, "cost_kind": &sc.CostKind, "destination": &sc.Destination,
		"event": &sc.Event, "color": &sc.Color, "option_label": &sc.Name,
		"source_name": &sc.Name,
	}
	for k, dst := range strs {
		if r, ok := get(k); ok {
			if err = json.Unmarshal(r, dst); err != nil {
				return sc, err
			}
		}
	}
	if r, ok := get("value"); ok {
		var num int64
		if json.Unmarshal(r, &num) == nil {
			sc.Value = num
		} else if json.Unmarshal(r, &sc.ValueS) == nil {
			sc.ValueStr = true
		}
	}
	for k, dst := range map[string]*int64{
		"minimum": &sc.Min, "maximum": &sc.Max, "selected_count": &sc.SelCount,
		"mode_index": &sc.ModeIdx, "mode_count": &sc.ModeCount, "slot": &sc.Slot,
		"option_index": &sc.OptIdx, "option_count": &sc.OptCount,
		"position": &sc.Position, "count": &sc.Count,
	} {
		if r, ok := get(k); ok {
			if v, e := v2JSONInt(r); e == nil {
				*dst = v
			}
		}
	}
	for k, dst := range map[string]**bool{
		"pay": &sc.Pay, "keep": &sc.Keep, "cast_it": &sc.CastIt,
	} {
		if r, ok := get(k); ok {
			var b bool
			if json.Unmarshal(r, &b) == nil {
				*dst = &b
			}
		}
	}
	return sc, nil
}

// v2SemRefFromJSON decodes {"player": "p1"} or {"object": {"object_id":..}}.
func v2SemRefFromJSON(r json.RawMessage) (*V2SemRef, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(r, &m); err != nil {
		return nil, err
	}
	if p, ok := m["player"]; ok {
		var seat string
		if err := json.Unmarshal(p, &seat); err != nil {
			return nil, err
		}
		return &V2SemRef{Player: seat}, nil
	}
	if o, ok := m["object"]; ok {
		var om map[string]json.RawMessage
		if err := json.Unmarshal(o, &om); err != nil {
			return nil, err
		}
		if id, ok := om["object_id"]; ok {
			var idStr string
			if err := json.Unmarshal(id, &idStr); err != nil {
				return nil, err
			}
			return &V2SemRef{Object: idStr}, nil
		}
	}
	return nil, nil
}

// v2JSONInt decodes a JSON number (or numeric string) as int64.
func v2JSONInt(r json.RawMessage) (int64, error) {
	var n int64
	if err := json.Unmarshal(r, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	}
	return 0, json.Unmarshal(r, &n)
}
