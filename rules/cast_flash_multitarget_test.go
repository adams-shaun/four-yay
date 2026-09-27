package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The multi-target member of the target-conditional CastWithFlash family:
// the announcement pool must stay FULL when the grant covers only part of it
// (the permission is existential over the announced set, so a legal
// completion may mix covered and uncovered targets), and the cast must
// complete without a CR 601.2e reversal.

// multiTargetFlashSrc is a hand-built sorcery with Flash Photography's
// self-carried permission shape (`ValidSA$ Spell.IsTargeting Valid
// Permanent.YouCtrl`) and Incriminate's mandatory two-target declaration
// (`TargetMin$ 2 | TargetMax$ 2`).
const multiTargetFlashSrc = "Name:Tandem Verdict\nManaCost:2 U\nTypes:Sorcery\n" +
	"S:Mode$ CastWithFlash | ValidCard$ Card.Self | ValidSA$ Spell.IsTargeting Valid Permanent.YouCtrl | EffectZone$ All | Caster$ You | Description$ You may cast this spell as though it had flash if it targets a permanent you control.\n" +
	"A:SP$ Pump | ValidTgts$ Creature | TgtPrompt$ Choose two target creatures | TargetMin$ 2 | TargetMax$ 2 | TargetUnique$ True | NumAtt$ +1 | NumDef$ +1 | SpellDescription$ Two target creatures each get +1/+1.\n" +
	"Oracle:You may cast this spell as though it had flash if it targets a permanent you control.\\nTwo target creatures each get +1/+1."

func multiTargetFlashEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, card(t, multiTargetFlashSrc))
	e.G.Active = 1
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MU] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("precondition: the spell is in %s, want hand", e.G.Obj(spell).Zone)
	}
	if e.G.Active != 1 {
		t.Fatalf("precondition: active seat = %d, want 1 (off-turn for seat 0)", e.G.Active)
	}
	if !e.hasTargetConditionalFlash(0, spell) {
		t.Fatal("precondition: the spell's flash grant is not target-conditional; the test would not pin the multi-target filter")
	}
	return e, spell
}

// TestFlashMultitargetMixedCompletionIsOfferedAndCasts pins the r2 MAJOR:
// with a mandatory two-target ask and exactly ONE qualifying own permanent,
// the per-candidate filter left a one-candidate pool that could not satisfy
// Min 2, so the ask aborted a cast the offer census had admitted. The full
// pool keeps the mixed completion (own creature + opponent creature) legal.
func TestFlashMultitargetMixedCompletionIsOfferedAndCasts(t *testing.T) {
	e, spell := multiTargetFlashEngine(t)
	myBear := battlePerm(t, e, 0, "Name:My Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirBear := battlePerm(t, e, 1, "Name:Their Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if a, b := e.G.Obj(myBear), e.G.Obj(theirBear); a == nil || b == nil || a.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield {
		t.Fatal("precondition: both creatures must be on the battlefield")
	}
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the cast was withheld although a mixed completion (own + opponent creature) is legal")
	}
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("the ask must offer the mixed completion, pending = %+v (note 'cast aborted: no legal target' = abort)", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("precondition: target ask bounds = %d..%d, want 2..2", d.Min, d.Max)
	}
	if targetOptionIndex(d, myBear) < 0 || targetOptionIndex(d, theirBear) < 0 {
		t.Fatalf("precondition: both creatures must be offered: %+v", d.Options)
	}
	// Selecting the mixed completion must complete the cast, not reverse it.
	submitChoices(t, e, targetOptionIndex(d, myBear), targetOptionIndex(d, theirBear))
	if hasNote(e, "cast aborted") {
		t.Fatal("a mixed covered/uncovered completion was reversed; the permission judges the whole announced set")
	}
	if e.G.Obj(spell).Zone == state.ZHand {
		t.Fatalf("the mixed completion left the spell in %s, want off-hand (stack)", e.G.Obj(spell).Zone)
	}
}

// TestFlashMultitargetQualifyingPairAnnouncesBoth pins the second half of the
// finding: with TWO qualifying own permanents AND an uncovered opponent
// permanent on the board, the opponent creature must stay on the menu (a
// pair including it and one own permanent is a legal announcement), instead
// of being hidden by the per-candidate filter.
func TestFlashMultitargetQualifyingPairAnnouncesBoth(t *testing.T) {
	e, spell := multiTargetFlashEngine(t)
	bearOne := battlePerm(t, e, 0, "Name:My Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bearTwo := battlePerm(t, e, 0, "Name:My Other Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirBear := battlePerm(t, e, 1, "Name:Their Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	for _, id := range []state.ObjID{bearOne, bearTwo, theirBear} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: creature missing: %+v", o)
		}
	}
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the cast was withheld although qualifying targets exist")
	}
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the two-target ask", d)
	}
	if targetOptionIndex(d, theirBear) < 0 {
		t.Fatalf("the uncovered opponent creature was hidden although pairings with an own creature are legal: %+v", d.Options)
	}
	// And a pairing of qualifying + uncovered completes.
	submitChoices(t, e, targetOptionIndex(d, bearOne), targetOptionIndex(d, theirBear))
	if hasNote(e, "cast aborted") {
		t.Fatal("the qualifying-pair-with-uncovered completion was reversed")
	}
	if e.G.Obj(spell).Zone == state.ZHand {
		t.Fatalf("the cast left the spell in %s, want off-hand", e.G.Obj(spell).Zone)
	}
}

// TestFlashMultitargetNoQualifyingTargetWithheld keeps the old contract
// alive on the multi-target shape: with only opponent creatures the cast can
// NEVER satisfy the grant, so it must not be offered (and not reach the ask).
func TestFlashMultitargetNoQualifyingTargetWithheld(t *testing.T) {
	e, spell := multiTargetFlashEngine(t)
	battlePerm(t, e, 1, "Name:Their Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(spell).Face().IsInstant() {
		t.Fatal("precondition: the spell would not need flash if it were an instant")
	}
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the multi-target flash cast was offered although no qualifying target exists")
	}
}
