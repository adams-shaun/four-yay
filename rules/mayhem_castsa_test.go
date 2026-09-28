package rules

// The Card.CastSa Spell.Mayhem provenance (review round 3 finding): the
// mayhem cast's pay-time CastInfo stamps state.FlagMayhem, and the
// ConditionPresent$ Card.CastSa/!CastSa Spell.Mayhem gates evaluate it —
// pinned end to end on the real corpus carrier, Sandman's Quicksand
// (`.cards/cardsfolder/s/sandmans_quicksand.txt`): a plain cast gives every
// creature -2/-2, a mayhem cast gives only creatures the caster's OPPONENTS
// control -2/-2.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const sandmansFixture = "Name:Test Sandman's Quicksand\nManaCost:1 B B\nTypes:Sorcery\nK:Mayhem:3 B\n" +
	"A:SP$ PumpAll | ValidCards$ Creature | NumAtt$ -2 | NumDef$ -2 | SubAbility$ DBPumpAll | " +
	"ConditionDefined$ Self | ConditionPresent$ Card.!CastSa Spell.Mayhem | ConditionCompare$ EQ1\n" +
	"SVar:DBPumpAll:DB$ PumpAll | ValidCards$ Creature.OppCtrl | NumAtt$ -2 | NumDef$ -2 | " +
	"ConditionDefined$ Self | ConditionPresent$ Card.CastSa Spell.Mayhem\nOracle:x\n"

// quicksandBoard builds a two-seat engine with one 3/3 creature under each
// player and the fixture sorcery in player 0's hand. The bears are the
// fixture: a vanilla 3/3 with no keywords, so the -2/-2 arithmetic is exact.
func quicksandBoard(t *testing.T, plainCast bool) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, card(t, sandmansFixture))
	mine := onBoard(t, e, 0, "Name:Test Bear Home\nManaCost:2 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	theirs := onBoard(t, e, 1, "Name:Test Bear Away\nManaCost:2 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	// Precondition: the gates below compare powers, so the board must start
	// symmetric and intact.
	if e.Power(mine) != 3 || e.Toughness(mine) != 3 || e.Power(theirs) != 3 || e.Toughness(theirs) != 3 {
		t.Fatalf("setup: bears are not 3/3 (mine %d/%d, theirs %d/%d)",
			e.Power(mine), e.Toughness(mine), e.Power(theirs), e.Toughness(theirs))
	}
	qs := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(qs).Face().Name != "Test Sandman's Quicksand" {
		t.Fatalf("setup: hand card = %q, want the fixture sorcery", e.G.Obj(qs).Face().Name)
	}
	if !plainCast {
		discardToGraveyard(t, e, qs, 0)
	}
	return e, qs, mine, theirs
}

// TestSandmansQuicksandPlainCastHitsAllCreatures pins the !CastSa arm: a
// plain hand cast leaves no FlagMayhem provenance, so the gate's EQ1 holds
// and ALL creatures get -2/-2 — the opponent's bear included, which is the
// assertion that distinguishes this arm from the mayhem arm.
func TestSandmansQuicksandPlainCastHitsAllCreatures(t *testing.T) {
	t.Parallel()
	e, qs, mine, theirs := quicksandBoard(t, true)
	addMana(t, e, 0, "1BB")

	var opts []int
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == qs {
			opts = append(opts, o.Index)
		}
	}
	if len(opts) != 1 {
		t.Fatalf("plain cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0])
	if e.G.Obj(qs).CastFlags != 0 {
		t.Fatalf("plain cast carries CastFlags %+v, want none", e.G.Obj(qs).CastFlags)
	}
	passUntilStackEmpty(t, e, 20)

	if got := e.Power(mine); got != 1 {
		t.Fatalf("own bear power = %d, want 1 (plain cast hits all creatures)", got)
	}
	if got := e.Power(theirs); got != 1 {
		t.Fatalf("opponent bear power = %d, want 1 (plain cast hits all creatures)", got)
	}
}

// TestSandmansQuicksandMayhemCastHitsOnlyOpponentsCreatures pins the CastSa
// arm: the mayhem cast's pay-time CastInfo stamps state.FlagMayhem on the
// resolving spell, so the main gate's !CastSa alternative is dropped and the
// sub's positive gate holds — the caster's own bear is untouched while the
// opponent's bear drops by 2. The previous build ran BOTH pumps (the
// unresolved gate fail-opened) and left the own bear at 1.
func TestSandmansQuicksandMayhemCastHitsOnlyOpponentsCreatures(t *testing.T) {
	t.Parallel()
	e, qs, mine, theirs := quicksandBoard(t, false)
	addMana(t, e, 0, "1BBB") // the {3}{B} mayhem cost, not the printed {1}{B}{B}

	opts := mayhemOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != qs {
		t.Fatalf("mayhem cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0].Index)
	// Precondition for the gate below: the cast must actually carry the
	// provenance bit while it resolves, or the condition read would be
	// vacuous either way.
	if e.G.Obj(qs).Zone != state.ZStack {
		t.Fatalf("mayhem spell zone = %v, want stack at the resolution point", e.G.Obj(qs).Zone)
	}
	if got := e.G.Obj(qs).CastFlags; got != state.FlagMayhem {
		t.Fatalf("mayhem cast CastFlags = %+v, want FlagMayhem", got)
	}
	passUntilStackEmpty(t, e, 20)

	if got := e.Power(mine); got != 3 {
		t.Fatalf("own bear power = %d, want 3 (mayhem pump spares the caster's creatures)", got)
	}
	if got := e.Power(theirs); got != 1 {
		t.Fatalf("opponent bear power = %d, want 1 (mayhem pump hits only opponents' creatures)", got)
	}
	// No exile tail: the sorcery rests in its owner's graveyard like a plain
	// resolution.
	if got := e.G.Obj(qs).Zone; got != state.ZGraveyard {
		t.Fatalf("resolved mayhem sorcery zone = %v, want graveyard", got)
	}
}
