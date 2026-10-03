package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestEveryConfiguredAbilityHasItsFactsRecord holds W4 step 1's binding
// total over every repo deck card and every token script: each ability
// reachable from a configured face (its Abilities and their SubAbility$
// chains, trigger Execute$ bodies, replacement bodies) has its compiled
// ParamSet bound, an ExtSlot, and its own saFacts record, with the mana half
// present exactly for an AB$ ability. A reader of a typed
// per-SA fact can therefore rely on the record for configured text and
// keep its map fallback only for runtime-built abilities.
func TestEveryConfiguredAbilityHasItsFactsRecord(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var all []*cards.Card
	for _, name := range testutil.RepoDeckNames() {
		all = append(all, testutil.RepoDeck(t, reg, name)...)
	}
	ct := buildCompiledText(Config{Decks: [][]*cards.Card{all}, Tokens: reg.Tokens})

	checked, fromSlot := 0, 0
	seen := map[*cards.SA]bool{}
	var check func(c *cards.Card, sa *cards.SA)
	check = func(c *cards.Card, sa *cards.SA) {
		for ; sa != nil && !seen[sa]; sa = sa.Sub {
			seen[sa] = true
			checked++
			// MayHaveAnyParam answers false for an empty mask only through a
			// bound ParamSet (an unbound node answers conservatively true).
			if sa.Params != nil && sa.MayHaveAnyParam(cards.ParamMask{}) {
				t.Errorf("%s: %q: ParamSet not bound", c.Path, sa.Line)
			}
			if sa.ExtSlot() == nil {
				t.Errorf("%s: %q: no ExtSlot", c.Path, sa.Line)
				continue
			}
			// The record is served from the slot, or from the table when a
			// by-value copy of the ability (which shares its slot) published
			// first -- the corpus registry is one shared instance per process,
			// so another test's engine may have done so.
			f := ct.factsOf(sa)
			if f == nil || f.SA != sa {
				t.Errorf("%s: %q: no own facts record", c.Path, sa.Line)
				continue
			}
			if p := effects.LoadSAFacts(sa); p != nil && p.SA == sa {
				fromSlot++
			}
			if (manaHalf(f) != nil) != (sa.Kind == "AB") {
				t.Errorf("%s: %q (Kind %s): mana half present=%v", c.Path, sa.Line, sa.Kind, manaHalf(f) != nil)
			}
			if (f.ChangeZone != nil) != (sa.CompiledAPI() == cards.APIChangeZone || sa.API == "ChangeZone") {
				t.Errorf("%s: %q (API %s): ChangeZone half present=%v", c.Path, sa.Line, sa.API, f.ChangeZone != nil)
			}
		}
	}
	visit := func(c *cards.Card) {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, sa := range f.Abilities {
				check(c, sa)
			}
			for i := range f.Triggers {
				check(c, f.Triggers[i].Effect)
			}
			for i := range f.Repls {
				check(c, f.Repls[i].With)
			}
		}
	}
	for _, c := range all {
		visit(c)
	}
	for _, c := range reg.Tokens {
		visit(c)
	}
	if checked < 1000 {
		t.Fatalf("checked only %d abilities: the walk is vacuous", checked)
	}
	t.Logf("%d configured abilities carry their facts record (%d served from their own slot)", checked, fromSlot)
}
