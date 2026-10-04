package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// mayHaveDerivedKeywordH is mayHaveDerivedKeyword for a precompiled head.
func (e *Engine) mayHaveDerivedKeywordH(id state.ObjID, head kwHead) bool {
	return e.mayHaveDerivedKeywordAnyH(id, head)
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
	head := h.S
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

// mayHaveDerivedKeywordAnyH is mayHaveDerivedKeywordAny over precompiled
// heads; a head whose s is empty is skipped. It takes any number of heads so
// one precheck can cover a whole set (the four granted-expanded-keyword heads
// Cycling/TypeCycling/Saddle/Crew) in a single pass over the derived seeds.
func (e *Engine) mayHaveDerivedKeywordAnyH(id state.ObjID, heads ...kwHead) bool {
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
	// (cards.Face.KeywordLinesHaveHead: the EqualFold answer per head).
	for _, h := range heads {
		if h.S != "" && f.KeywordLinesHaveHead(h.S, h.ID) {
			return true
		}
	}
	matchHead := func(k string) bool {
		head := cards.KeywordHead(k)
		for _, h := range heads {
			if h.S != "" && strings.EqualFold(head, h.S) {
				return true
			}
		}
		return false
	}
	for _, k := range o.IntrinsicKeywords {
		if matchHead(k) {
			return true
		}
	}
	for _, c := range o.Counters {
		if kwName, ok := cards.CounterKeyword(c.Kind); ok && c.N > 0 && matchHead(kwName) {
			return true
		}
	}
	outer := e.activeDepth == 0
	active := e.active()
	if outer {
		// Interned heads answer by bit: two ASCII heads are EqualFold
		// exactly when they intern to one ordinal. A head that did not
		// intern (non-ASCII, or the vocabulary full) takes the fold scan.
		if e.activeKWHeadSetOK {
			for _, hd := range heads {
				if hd.S == "" {
					continue
				}
				if hd.ID != 0 {
					if e.activeKWHeadSet.Has(hd.ID) {
						return true
					}
					continue
				}
				for _, h := range e.activeKWHeads {
					if strings.EqualFold(h, hd.S) {
						return true
					}
				}
			}
			return false
		}
		for _, h := range e.activeKWHeads {
			for _, hd := range heads {
				if hd.S != "" && strings.EqualFold(h, hd.S) {
					return true
				}
			}
		}
		return false
	}
	for i := range active {
		for _, k := range active[i].AddKeywords {
			if matchHead(k) {
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

// kwHeadSetOf interns heads into a keyword-head bitset; ok is false when
// some head has no ordinal (non-ASCII, or the vocabulary full).
func kwHeadSetOf(heads []string) (set cards.KeywordHeadSet, ok bool) {
	for _, h := range heads {
		id := cards.KeywordHeadIDOf(h)
		if id == 0 {
			return cards.KeywordHeadSet{}, false
		}
		set[id>>6] |= 1 << (id & 63)
	}
	return set, true
}
