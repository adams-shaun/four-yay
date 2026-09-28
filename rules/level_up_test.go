package rules

// The Level up keyword (CR 702.87). K:Level up:<cost> used to have no
// expansion in cards/keywords.go, so the level-gated S:Mode$ Continuous
// bands had no LEVEL counters to read and effects.Supported() lacked
// kw:Level up. The case now synthesizes the ordinary sorcery-speed
// PutCounter activation; these tests pin it on the real Coralhelm Commander
// script (read as text from the gitignored corpus, never committed) through
// the ordinary activated-ability path.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestLevelUpKeywordIsSupported: the keyword must be registered as an
// implemented non-API primitive, or every Level up carrier stays
// "unplayable" in make report / the acceptance ratchet regardless of the
// expansion working.
func TestLevelUpKeywordIsSupported(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Level up"] {
		t.Fatal("effects.Supported() lacks kw:Level up")
	}
}

// levelUpOptionActivate drives one sorcery-speed Level up activation on the
// permanent: it funds the cost, finds the "ability" option whose Ability
// index is idx, submits it and drains the stack.
func levelUpOptionActivate(t *testing.T, e *Engine, id state.ObjID, mana string, idx int) {
	t.Helper()
	addMana(t, e, 0, mana)
	opt := abilityOption(t, e, id, idx)
	if opt.Label == "" {
		t.Fatalf("Level up ability %d has an empty label", idx)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
}

// coralhelmMerfolkSrc is a bare Merfolk fixture (never a committed .txt) the
// LEVEL 4+ lord band's "Other Merfolk creatures you control get +1/+1"
// applies to.
const coralhelmMerfolkSrc = "Name:Test Merfolk\nManaCost:1\nTypes:Creature Merfolk\nPT:1/1\nOracle:x\n"

// TestCoralhelmCommanderLevelsIntoThe23Band pins the real corpus card end to
// end: the 2/2 baseline, the 3/3 flying band at two LEVEL counters, and the
// 4/4 flying band at four, including the LEVEL 4+ lord half (a second Merfolk
// picks up +1/+1) that reaches other creatures through the band static's
// AddStaticAbility$ SBoost. The band statics are the existing IsPresent$
// counters_GE/LE_LEVEL-gated SetPower/SetToughness/AddKeyword machinery --
// this test is what proves the synthesized ability feeds it.
func TestCoralhelmCommanderLevelsIntoThe23Band(t *testing.T) {
	t.Parallel()
	src := corpusCardText(t, "c/coralhelm_commander.txt")
	e, cfg, _ := newFixtureDeck(t, 907, src, coralhelmMerfolkSrc)
	id := putCreature(t, e, 0, src)
	merfolk := putCreature(t, e, 0, coralhelmMerfolkSrc)

	if got := e.Power(id); got != 2 {
		t.Fatalf("baseline power = %d, want 2", got)
	}
	if e.HasKeyword(id, "Flying") {
		t.Fatal("baseline Coralhelm Commander must not fly")
	}
	if got := e.Power(merfolk); got != 1 {
		t.Fatalf("baseline lord has no business at power %d, want 1", got)
	}

	// Two activations -> two LEVEL counters -> LEVEL 2-3 band.
	levelUpOptionActivate(t, e, id, "C", 0)
	if got := e.G.Obj(id).Counter("LEVEL"); got != 1 {
		t.Fatalf("LEVEL counters after one activation = %d, want 1", got)
	}
	levelUpOptionActivate(t, e, id, "C", 0)
	if got := e.G.Obj(id).Counter("LEVEL"); got != 2 {
		t.Fatalf("LEVEL counters after two activations = %d, want 2", got)
	}
	if p, tho := e.Power(id), e.Toughness(id); p != 3 || tho != 3 {
		t.Fatalf("LEVEL 2-3 body = %d/%d, want 3/3", p, tho)
	}
	if !e.HasKeyword(id, "Flying") {
		t.Fatal("LEVEL 2-3 Coralhelm Commander must have flying")
	}
	if got := e.Power(merfolk); got != 1 {
		t.Fatalf("LEVEL 2-3 is not a lord band: other Merfolk power = %d, want 1", got)
	}

	// Two more -> four LEVEL counters -> LEVEL 4+ band.
	levelUpOptionActivate(t, e, id, "C", 0)
	levelUpOptionActivate(t, e, id, "C", 0)
	if got := e.G.Obj(id).Counter("LEVEL"); got != 4 {
		t.Fatalf("LEVEL counters after four activations = %d, want 4", got)
	}
	if p, tho := e.Power(id), e.Toughness(id); p != 4 || tho != 4 {
		t.Fatalf("LEVEL 4+ body = %d/%d, want 4/4", p, tho)
	}
	if !e.HasKeyword(id, "Flying") {
		t.Fatal("LEVEL 4+ Coralhelm Commander must have flying")
	}
	if p, tho := e.Power(merfolk), e.Toughness(merfolk); p != 2 || tho != 2 {
		t.Fatalf("LEVEL 4+ lord band: other Merfolk = %d/%d, want 2/2", p, tho)
	}
	replayCheck(t, e, cfg)
}
