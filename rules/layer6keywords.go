package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// EffectiveKeywords publishes the layer-derived keyword table to the effects
// tier (effects' keywordTableHost): every battlefield object whose current
// keyword list (Derived, CR 613.1f -- an AddKeyword$ grant, a keyword lost
// with its abilities) differs from what its printed face and keyword
// counters give, with that current list. A resolving effect's own filter
// walk (DamageAll's ValidCards$, DestroyAll, Count$Valid, ...) builds its
// SpecContext without rules' per-candidate ExtraKeywords bind, so before
// this table a with<Keyword>/without<Keyword> predicate there read the
// printed face: Seismic Rupture's "each creature without flying" damaged a
// Grizzly Bears that Ajani's -3 had given flying. Entries are copies (the
// Derived keyword stream is a scratch buffer), objects in battlefield seat
// order; nil on the common board where nothing differs, and nil while a
// layer walk is in progress (the printed read stands, as before).
func (e *Engine) EffectiveKeywords() []effects.ObjectKeywords {
	if e.activeDepth != 0 || e.derivedDepth != 0 {
		return nil
	}
	var out []effects.ObjectKeywords
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			derived := e.Derived(id).Keywords
			if keywordsMatchPrinted(o, derived) {
				continue
			}
			out = append(out, effects.ObjectKeywords{ID: id, Keywords: append([]string{}, derived...)})
		}
	}
	return out
}

// keywordsMatchPrinted reports whether derived names exactly the keyword
// heads the printed read (effects.PrintedHasKeyword) answers for o: every
// derived head is printed, and every printed face keyword survives.
func keywordsMatchPrinted(o *state.Object, derived []string) bool {
	for _, k := range derived {
		if !effects.PrintedHasKeyword(o, cards.KeywordHead(k)) {
			return false
		}
	}
	for _, k := range o.Face().Keywords {
		head := cards.KeywordHead(k)
		found := false
		for _, d := range derived {
			if strings.EqualFold(cards.KeywordHead(d), head) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
