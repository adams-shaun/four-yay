package rules

import (
	"reflect"
	"strings"
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

	checked, fromSlot, targeted, defined := 0, 0, 0, 0
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
			if (f.ChangeZoneAll != nil) != (sa.CompiledAPI() == cards.APIChangeZoneAll || sa.API == "ChangeZoneAll") {
				t.Errorf("%s: %q (API %s): ChangeZoneAll half present=%v", c.Path, sa.Line, sa.API, f.ChangeZoneAll != nil)
			}
			if (f.Attach != nil) != (sa.CompiledAPI() == cards.APIAttach || sa.API == "Attach") {
				t.Errorf("%s: %q (API %s): Attach half present=%v", c.Path, sa.Line, sa.API, f.Attach != nil)
			}
			if (f.DealDamage != nil) != (sa.CompiledAPI() == cards.APIDealDamage || sa.API == "DealDamage") {
				t.Errorf("%s: %q (API %s): DealDamage half present=%v", c.Path, sa.Line, sa.API, f.DealDamage != nil)
			}
			if (f.PutCounter != nil) != (sa.CompiledAPI() == cards.APIPutCounter || sa.API == "PutCounter") {
				t.Errorf("%s: %q (API %s): PutCounter half present=%v", c.Path, sa.Line, sa.API, f.PutCounter != nil)
			}
			if (f.Effect != nil) != (sa.CompiledAPI() == cards.APIEffect || sa.API == "Effect") {
				t.Errorf("%s: %q (API %s): Effect half present=%v", c.Path, sa.Line, sa.API, f.Effect != nil)
			}
			// The targeting tier is compiled for EVERY ability, whatever its
			// API, and TargetsOf serves the configured record.
			if f.Targets == nil {
				t.Errorf("%s: %q (API %s): no Targets half", c.Path, sa.Line, sa.API)
			} else {
				if want := strings.TrimSpace(sa.Params["ValidTgts"]) != ""; f.Targets.Targeted() != want {
					t.Errorf("%s: %q (API %s): Targets.Targeted()=%v, want %v", c.Path, sa.Line, sa.API, f.Targets.Targeted(), want)
				}
				if got := effects.TargetsOf(sa); len(sa.Params) > 0 && got != f.Targets && !reflect.DeepEqual(*got, *f.Targets) {
					t.Errorf("%s: %q (API %s): TargetsOf disagrees with the configured Targets half", c.Path, sa.Line, sa.API)
				}
				if f.Targets.Targeted() {
					targeted++
				}
			}
			if (f.DelayedTrigger != nil) != (sa.CompiledAPI() == cards.APIDelayedTrigger || sa.API == "DelayedTrigger") {
				t.Errorf("%s: %q (API %s): DelayedTrigger half present=%v", c.Path, sa.Line, sa.API, f.DelayedTrigger != nil)
			}
			if (f.CopyPermanent != nil) != (sa.API == "CopyPermanent") {
				t.Errorf("%s: %q (API %s): CopyPermanent half present=%v", c.Path, sa.Line, sa.API, f.CopyPermanent != nil)
			}
			// The Defined-reference tier is compiled for EVERY ability,
			// whatever its API, and DefinedOf serves the configured record.
			if f.Defined == nil {
				t.Errorf("%s: %q (API %s): no Defined half", c.Path, sa.Line, sa.API)
			} else {
				if got := f.Defined.Defined.Text; got != strings.TrimSpace(sa.Params["Defined"]) {
					t.Errorf("%s: %q (API %s): Defined.Text=%q", c.Path, sa.Line, sa.API, got)
				}
				if got := effects.DefinedOf(sa); len(sa.Params) > 0 && got != f.Defined && !reflect.DeepEqual(*got, *f.Defined) {
					t.Errorf("%s: %q (API %s): DefinedOf disagrees with the configured Defined half", c.Path, sa.Line, sa.API)
				}
				if f.Defined.Defined.Set() {
					defined++
				}
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
	if targeted == 0 {
		t.Fatal("no configured ability targets: the Targets half check is vacuous")
	}
	if defined == 0 {
		t.Fatal("no configured ability names a Defined$ selector: the Defined half check is vacuous")
	}
	t.Logf("%d configured abilities carry their facts record (%d served from their own slot, %d targeting, %d naming Defined$)", checked, fromSlot, targeted, defined)
}
