package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The graveyard section's candidate filter.
//
// graveyardCastsWalk runs eight loops over the seat's graveyard, one per
// route, and every loop's first gate is a keyword read: the PRINTED
// Harmonize and Warp lines (harmonizeCost, keywordAltCost: Face.KeywordParam),
// the DERIVED Flashback, Escape, Retrace, Jump-start and Mayhem heads
// (hasKeywordH / mayHaveDerivedKeywordH, whose exact negative precheck is
// mayHaveDerivedKeywordAnyH), and the aftermath alternate face
// (aftermathAlternateFace). Measured on the random SpellBench leg the section
// was 12% of the walk, almost all of it those gates re-run per loop over
// graveyards in which no card has any of the routes.
//
// graveyardCandidates evaluates the union of those first gates ONCE per card:
// a card survives when any route's first gate could pass, and only survivors
// reach the loops. The union is exact on the negative side -- each gate is a
// read of the same seeds this checks (the face's printed keyword heads, the
// object's IntrinsicKeywords, its keyword counters, its three status flags,
// and the active effects' AddKeywords heads), so a card ruled out here fails
// every loop's first gate and the loops would have skipped it. A board whose
// active effects grant any derived graveyard head is not filtered at all, nor
// is a walk inside an active() build (where mayHaveDerivedKeywordAnyH reads
// the handed list rather than activeKWHeads). walkSkipVerify (the rules test
// binary) runs the loops over the whole graveyard too and panics unless the
// two option lists are identical.

var (
	kwhHarmonize = newKWHead("Harmonize")
	kwhWarp      = newKWHead("Warp")
)

// graveyardPrintedHeads are the heads whose printed keyword line opens a
// graveyard route; graveyardDerivedHeads are those read through the derived
// list (a subset: Harmonize and Warp are printed-only reads).
var (
	graveyardPrintedHeads = [...]kwHead{kwhHarmonize, kwhWarp, kwhFlashback, kwhEscape, kwhRetrace, kwhJumpStart, kwhMayhem}
	graveyardDerivedHeads = [...]kwHead{kwhFlashback, kwhEscape, kwhRetrace, kwhJumpStart, kwhMayhem}
)

// graveyardCandidates appends to buf the ids of zone that can pass some
// graveyard route's first gate, in zone order (filtered true), or reports
// filtered false when the board cannot be filtered (the whole zone is the
// candidate list).
func (w *legalWalk) graveyardCandidates(zone []state.ObjID, buf []state.ObjID) ([]state.ObjID, bool) {
	e := w.e
	if len(zone) == 0 || e.activeDepth != 0 {
		return buf, false
	}
	e.active()
	for _, h := range e.activeKWHeads {
		for _, hd := range graveyardDerivedHeads {
			if strings.EqualFold(h, hd.S) {
				return buf, false
			}
		}
	}
	for _, id := range zone {
		if graveyardCandidate(e.G.Obj(id)) {
			buf = append(buf, id)
		}
	}
	return buf, true
}

// graveyardCandidate is the per-card half of graveyardCandidates.
func graveyardCandidate(o *state.Object) bool {
	if o == nil {
		return false
	}
	if aftermathAlternateFace(o) != nil {
		return true
	}
	f := o.Face()
	if f == nil {
		return false
	}
	if o.Cloaked || o.Suspected || o.SuspendGranted {
		return true
	}
	if len(f.Keywords) != 0 {
		for _, h := range graveyardPrintedHeads {
			if f.KeywordLinesHaveHead(h.S, h.ID) {
				return true
			}
		}
	}
	for _, k := range o.IntrinsicKeywords {
		if graveyardDerivedHead(k) {
			return true
		}
	}
	for _, c := range o.Counters {
		if kwName, ok := cards.CounterKeyword(c.Kind); ok && c.N > 0 && graveyardDerivedHead(kwName) {
			return true
		}
	}
	return false
}

func graveyardDerivedHead(k string) bool {
	head := cards.KeywordHead(k)
	for _, h := range graveyardDerivedHeads {
		if strings.EqualFold(head, h.S) {
			return true
		}
	}
	return false
}

// verifyGraveyardCandidates runs the section over the whole graveyard and
// over the filtered list and panics unless both offer the same options.
func (w *legalWalk) verifyGraveyardCandidates(zone, grave []state.ObjID) {
	n := len(w.out)
	w.graveyardCastsOver(zone)
	full := slices.Clone(w.out[n:])
	w.outHW = max(w.outHW, len(w.out))
	w.out = w.out[:n]
	w.graveyardCastsOver(grave)
	if got := w.out[n:]; !(len(got) == 0 && len(full) == 0) && !optionsEqual(got, full) {
		panic(fmt.Sprintf("rules: graveyard candidate filter offered %+v, the whole graveyard %+v (kept %v of %v)", got, full, grave, zone))
	}
}
