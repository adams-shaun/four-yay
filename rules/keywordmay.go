package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// mayHaveDerivedKeyword is an exact precheck for "does id's DERIVED keyword
// list carry an entry whose KeywordHead is head (case-insensitively)?".
//
// derivedCompute seeds the list with the printed face's keywords (or the
// face-down basis's none), IntrinsicKeywords, the marker-counter keywords and
// three status keywords (a cloaked face-down permanent's Ward:2, a suspected
// creature's Menace, a granted Suspend), and its layer-6 walk only ever
// removes entries from that list or appends an active effect's AddKeywords.
// So the derived list is a SUBSET of the union of those seeds and every
// active effect's AddKeywords, and when no element of the union has the head
// the answer is false without the full layer walk. A true answer is only
// "maybe": the caller runs the ordinary Derived read.
//
// The printed face is read even while face down (a superset of the basis's
// empty list), and any status flag short-circuits to "maybe" rather than
// matching its keyword, so the precheck never needs to mirror derivedCompute's
// conditions. The active-effect half reads activeKWHeads, the head set
// active() rebuilds with its list; a re-entrant call (inside an active()
// build) scans the list it was handed instead. Verify mode
// (derivedMemoVerify) recomputes every negative answer through Derived.
func (e *Engine) mayHaveDerivedKeyword(id state.ObjID, head string) bool {
	return e.mayHaveDerivedKeywordH(id, kwHeadOf(head))
}

// mayHaveDerivedKeywordH is mayHaveDerivedKeyword for a precompiled head.
func (e *Engine) mayHaveDerivedKeywordH(id state.ObjID, head kwHead) bool {
	return e.mayHaveDerivedKeywordAnyH(id, head, kwHead{})
}

// stackKeywordPossible is mayHaveDerivedKeyword for the cast-keyword reads
// that derive a proposed spell under the stack-zone override
// (derivedWith(id, ZStack): hasCastConvoke, hasCastImprovise,
// hasCastConspire, casualtySpec, blitzCosts, offspringRawParam). The override
// changes only which zone an AffectedZone$ grant is judged against; the
// keyword list it derives is still the same printed/intrinsic/counter/status
// seeds plus the AddKeywords of the same active() list, so the precheck's
// subset argument holds for it unchanged and a false answer means no entry of
// that derivation has the head. The offer walk asks these for every hand
// card, so the negative answer skips a whole layer walk per card. Verify
// mode (derivedMemoVerify) runs the override derivation and panics on a
// contradicted negative.
func (e *Engine) stackKeywordPossibleH(id state.ObjID, h kwHead) bool {
	head := h.s
	if e.mayHaveDerivedKeywordH(id, h) {
		return true
	}
	if derivedMemoVerify {
		for _, k := range e.derivedWith(id, state.ZStack).Keywords {
			if strings.EqualFold(cards.KeywordHead(k), head) {
				panic(fmt.Sprintf("rules: stack keyword precheck ruled out %q on obj %d but the stack derivation carries %q", head, id, k))
			}
		}
	}
	return false
}

// headIs reports whether k's KeywordHead equals a or (when non-empty) b,
// case-insensitively.
func headIs(k, a, b string) bool {
	h := cards.KeywordHead(k)
	return strings.EqualFold(h, a) || (b != "" && strings.EqualFold(h, b))
}

// mayHaveDerivedKeywordAny is mayHaveDerivedKeyword for either of two heads
// in one pass (b empty: head a alone).
func (e *Engine) mayHaveDerivedKeywordAny(id state.ObjID, a, b string) bool {
	hb := kwHead{}
	if b != "" {
		hb = kwHeadOf(b)
	}
	return e.mayHaveDerivedKeywordAnyH(id, kwHeadOf(a), hb)
}

// mayHaveDerivedKeywordAnyH is mayHaveDerivedKeywordAny over precompiled
// heads (b.s empty: head a alone).
func (e *Engine) mayHaveDerivedKeywordAnyH(id state.ObjID, ha, hb kwHead) bool {
	a, b := ha.s, hb.s
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	f := o.Face()
	if f == nil {
		return false // Derived is the zero value: no keywords
	}
	if o.Cloaked || o.Suspected || o.SuspendGranted {
		return true
	}
	// The printed keyword lines through the face's interned head bitset
	// (cards.Face.KeywordLinesHaveHead: headIs's EqualFold answer per head).
	if f.KeywordLinesHaveHead(a, ha.id) || (b != "" && f.KeywordLinesHaveHead(b, hb.id)) {
		return true
	}
	for _, k := range o.IntrinsicKeywords {
		if headIs(k, a, b) {
			return true
		}
	}
	for _, c := range o.Counters {
		if kwName, ok := cards.CounterKeyword(c.Kind); ok && c.N > 0 && headIs(kwName, a, b) {
			return true
		}
	}
	outer := e.activeDepth == 0
	active := e.active()
	if outer {
		for _, h := range e.activeKWHeads {
			if strings.EqualFold(h, a) || (b != "" && strings.EqualFold(h, b)) {
				return true
			}
		}
		return false
	}
	for i := range active {
		for _, k := range active[i].AddKeywords {
			if headIs(k, a, b) {
				return true
			}
		}
	}
	return false
}

// appendKWHeads appends the distinct KeywordHead of every AddKeywords entry
// in list to dst.
func appendKWHeads(dst []string, list []ContinuousEffect) []string {
	for i := range list {
		for _, k := range list[i].AddKeywords {
			h := cards.KeywordHead(k)
			dup := false
			for _, have := range dst {
				if have == h {
					dup = true
					break
				}
			}
			if !dup {
				dst = append(dst, h)
			}
		}
	}
	return dst
}

// verifyKeywordPrecheck panics when a negative mayHaveDerivedKeyword answer
// is contradicted by the full Derived read.
func (e *Engine) verifyKeywordPrecheck(id state.ObjID, head string) {
	for _, k := range e.Derived(id).Keywords {
		if strings.EqualFold(cards.KeywordHead(k), head) {
			panic(fmt.Sprintf("rules: keyword precheck ruled out %q on obj %d but Derived carries %q", head, id, k))
		}
	}
}
