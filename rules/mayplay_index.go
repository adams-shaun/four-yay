// May-play grant candidate indexing: the effect arms of legal.go's
// mayPlayLandIds and mayPlaySpellIds walks. Each enumerates, per active
// MayPlay ContinuousEffect, the (zone, id) pairs the grant covers, and the
// historical shape of that enumeration was a nested scan -- for every grant,
// every card of every public zone slice, through effectGrantMatches ->
// matchesSpec. A board that accumulates grants (Rakdos, the Muscle's
// UntilYourNextEndStep STPlay grant, one per sacrifice) and accumulates
// exiled cards paid that scan twice per priority decision:
// O(active grants x exiled cards) matcher calls each time, the growth the
// 2026-09-28 loop-growth profile measured.
//
// The index removes the nesting for the dominant Affects shape. The bare
// `Card.IsRemembered` spec (227 corpus files for the printed route; the
// effect-delivered route's STPlay grants) can only ever match an id in ONE
// of the two bindings effects/filter.go's IsRemembered predicate unions --
// the grant's own Remembered list and its source object's event-backed
// remembered list -- so those ids are the complete candidate set. Each
// candidate is still run through the grant's own effectGrantMatches gate
// and the walk's card test, so the index is a superset filter, never a
// semantic replacement: the offered pairs and their order are exactly what
// the nested scan emitted. Any other Affects shape (or the index disabled)
// falls back to the scan, which also remains the reference arm the
// mayPlayCandIndexVerify mode checks the indexed arm against.
package rules

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// mayPlayCandIndexOff turns the candidate index off for the offer-surface
// equivalence test's index-off arm: every effect arm takes the nested
// zone-slice scan exactly as it did before the index existed. Default
// false -- production always indexes when the Affects shape admits it.
var mayPlayCandIndexOff bool

// mayPlayCandIndexVerify re-derives every indexed enumeration with the
// nested scan and panics on a difference. Test-only (see
// rules/mayplay_index_test.go); off in production, where the index is
// trusted and the scan would be exactly the cost the index removes.
var mayPlayCandIndexVerify bool

// mayPlayCand is one (zone, id) pair an Effect-delivered may-play grant
// covers, in the walk's canonical order.
type mayPlayCand struct {
	zone state.Zone
	id   state.ObjID
}

// mayPlayPos is where one card sits in the walk's public-zone candidate
// space: the owner seat whose slice it is in and its index in that slice.
// The two keys (zone rank, then seat and slice position) are what orders
// the walk's nested scan, so the indexed arm sorts its candidates by the
// same key and the order never drifts.
type mayPlayPos struct {
	seat state.PlayerID
	pos  int32
}

// mayPlayIndexWalk carries one legal-actions walk's candidate space: the
// graveyard/exile position map, built lazily (a board with no indexable
// grant never pays for it) and reused across every grant the walk
// enumerates. One walk owns one value; the two walks (land, spell) of a
// legal-actions pass each build their own.
type mayPlayIndexWalk struct {
	e   *Engine
	pos map[state.ObjID]mayPlayPos
}

// positions is the walk's (seat, slice position) map over every card in the
// public zones the effect arms walk. A card sits in exactly one zone of one
// seat, so the ids are unique keys. Seats iterate in AliveFrom order -- the
// same order the scan's seat loop uses -- so a card whose owner has left
// the game carries no entry and the index skips it, exactly as the scan,
// which only ever visits AliveFrom slices, would.
func (w *mayPlayIndexWalk) positions() map[state.ObjID]mayPlayPos {
	if w.pos == nil {
		pos := make(map[state.ObjID]mayPlayPos)
		for _, q := range w.e.G.AliveFrom(0) {
			for _, z := range []state.Zone{state.ZGraveyard, state.ZExile} {
				for i, id := range w.e.G.Zone(z, q) {
					pos[id] = mayPlayPos{seat: q, pos: int32(i)}
				}
			}
		}
		w.pos = pos
	}
	return w.pos
}

// mayPlayWalkZones is one grant's considered zone list in walk order: the
// parsed AffectedZone order restricted to the public zones the effect arms
// walk (graveyard, exile), or both of them when the grant names no zone at
// all. This is the same list the nested scan iterated (its skip arm
// filtered the non-public zones out of the loop without visiting them), so
// a candidate's index in it is the zone rank the order needs.
func mayPlayWalkZones(zones []state.Zone, all bool) []state.Zone {
	if all {
		return []state.Zone{state.ZGraveyard, state.ZExile}
	}
	out := make([]state.Zone, 0, len(zones))
	for _, z := range zones {
		if z == state.ZGraveyard || z == state.ZExile {
			out = append(out, z)
		}
	}
	return out
}

// mayPlayRememberedCandidates returns the candidate ids one Effect-delivered
// may-play grant's bare `Card.IsRemembered` Affects can possibly cover: the
// grant's own Remembered ids unioned with its source object's event-backed
// remembered object ids -- the two bindings the IsRemembered predicate
// (effects/filter.go) unions, both failing closed to no-match when absent.
// nil means the Affects spec is any other shape (compound, qualified, a
// different predicate) or the index is off: the caller keeps the nested
// zone-slice scan. The result is a SUPERSET, never a subset: the predicate
// cannot match an id outside both lists, and every candidate is still run
// through the grant's own effectGrantMatches gate and the walk's card test,
// so an id the spec, the zone set or the card test refuses is refused
// exactly as the scan would refuse it.
func mayPlayRememberedCandidates(g *state.Game, ce *state.ContinuousEffect) []state.ObjID {
	if mayPlayCandIndexOff || strings.TrimSpace(ce.Affects) != "Card.IsRemembered" {
		return nil
	}
	out := make([]state.ObjID, 0, len(ce.Remembered))
	out = append(out, ce.Remembered...)
	if src := g.Obj(ce.Source); src != nil {
		for _, t := range src.Remembered {
			if !t.IsPlayer {
				out = append(out, t.Obj)
			}
		}
	}
	return out
}

// pairs enumerates the (zone, id) pairs one Effect-delivered may-play grant
// covers among the walk's public-zone slices, keeping the walk's own card
// test keep, in the walk's canonical order -- zones in zl order, then seats
// in AliveFrom order, then each zone slice's order; seatFirst swaps to the
// spell walk's seat-major order. This is the effect arm's enumeration half
// ONLY: the caller keeps its own (zone, id[, key]) dedupe and offer append,
// exactly as before the index existed, so two grants covering the same card
// and duplicate candidate ids deduplicate exactly as the scan's did.
func (w *mayPlayIndexWalk) pairs(ce *state.ContinuousEffect, zl []state.Zone, seatFirst bool, keep func(*state.Object) bool) []mayPlayCand {
	cands := mayPlayRememberedCandidates(w.e.G, ce)
	if cands == nil || len(zl) == 0 {
		return w.scan(ce, zl, seatFirst, keep)
	}
	pos := w.positions()
	type keyed struct {
		c   mayPlayCand
		key int64
	}
	var hits []keyed
	for _, id := range cands {
		o := w.e.G.Obj(id)
		if o == nil || !keep(o) {
			continue
		}
		zr := -1
		for i, z := range zl {
			if z == o.Zone {
				zr = i
				break
			}
		}
		if zr < 0 {
			continue
		}
		pp, ok := pos[id]
		if !ok {
			continue
		}
		if !w.e.effectGrantMatches(ce, id) {
			continue
		}
		hits = append(hits, keyed{mayPlayCand{o.Zone, id}, mayPlayPairKey(zr, pp, seatFirst)})
	}
	slices.SortFunc(hits, func(a, b keyed) int { return cmp.Compare(a.key, b.key) })
	out := make([]mayPlayCand, len(hits))
	for i := range hits {
		out[i] = hits[i].c
	}
	if mayPlayCandIndexVerify {
		want := w.scan(ce, zl, seatFirst, keep)
		if !slices.Equal(out, want) {
			panic(fmt.Sprintf("rules: may-play candidate index diverged for grant of obj %d: indexed %v, scanned %v", ce.Source, out, want))
		}
	}
	return out
}

// mayPlayPairKey packs one pair's walk order into a comparable key. Slice
// positions stay below 2^32 (a zone slice of half a billion cards is not a
// reachable state), seat and zone ranks below 2^8, so the packed key
// preserves the rank order exactly.
func mayPlayPairKey(zr int, pp mayPlayPos, seatFirst bool) int64 {
	seat, pos := int64(pp.seat), int64(pp.pos)
	if seatFirst {
		return seat<<40 | int64(zr)<<32 | pos
	}
	return int64(zr)<<40 | seat<<32 | pos
}

// scan is the effect arm's original nested zone-slice walk, unchanged in
// shape: every card of every considered zone slice, in the walk's canonical
// order, through the grant's own effectGrantMatches gate and the walk's
// card test. It serves the Affects shapes the candidate index cannot
// pre-filter, and it is the reference arm mayPlayCandIndexVerify checks the
// indexed arm against.
func (w *mayPlayIndexWalk) scan(ce *state.ContinuousEffect, zl []state.Zone, seatFirst bool, keep func(*state.Object) bool) []mayPlayCand {
	var out []mayPlayCand
	emit := func(z state.Zone, id state.ObjID) {
		o := w.e.G.Obj(id)
		if o == nil || !keep(o) || !w.e.effectGrantMatches(ce, id) {
			return
		}
		out = append(out, mayPlayCand{z, id})
	}
	if seatFirst {
		for _, q := range w.e.G.AliveFrom(0) {
			for _, z := range zl {
				for _, id := range w.e.G.Zone(z, q) {
					emit(z, id)
				}
			}
		}
	} else {
		for _, z := range zl {
			for _, q := range w.e.G.AliveFrom(0) {
				for _, id := range w.e.G.Zone(z, q) {
					emit(z, id)
				}
			}
		}
	}
	return out
}
