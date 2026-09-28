package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 611.2a (5771-5773): without a stated duration, a resolution-created
// continuous effect lasts until the end of the game.
// CR 613.1f / 613.4b-c: ability removal is layer 6; set P/T precedes modifiers.
// Both leaves pass with the conformance flag on, so both run in the ordinary lane.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCR611IndefinitePumpSurvivesCleanup(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	e := crResolutionEngine(t, []string{"Riding the Dilu Horse"}, nil)
	horse := crAbortMove(t, e, 0, "Riding the Dilu Horse", state.ZHand)
	target := crAbortMove(t, e, 0, "Delver of Secrets", state.ZBattlefield)
	sa := e.G.Obj(horse).Face().SpellAbility()
	if sa == nil || sa.API != "Pump" || sa.Params["Duration"] != "Permanent" || sa.Params["NumAtt"] != "+2" || sa.Params["NumDef"] != "+2" {
		t.Fatal("CR 611.2a Riding the Dilu Horse seq 0: fixture changed")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 3})
	e.askPriority(0)
	crAbortAnswer(t, e, "Riding the Dilu Horse", crAbortOption(t, e, "Riding the Dilu Horse", "cast", horse))
	crAbortAnswer(t, e, "Riding the Dilu Horse", crAbortOption(t, e, "Riding the Dilu Horse", "permanent", target))
	crResolutionRound(t, e)
	if e.Power(target) != 3 || e.Toughness(target) != 3 || !e.HasKeyword(target, "Horsemanship") || e.G.Obj(horse).Zone != state.ZGraveyard {
		t.Fatalf("CR 611.2a Riding the Dilu Horse seq %d: initial +2/+2 and horsemanship did not resolve", len(e.L.Events))
	}
	// Invoke the actual expiry owner, not a synthetic effect or private slice edit.
	e.EndOfTurnCleanup()
	if e.Power(target) != 3 || e.Toughness(target) != 3 || !e.HasKeyword(target, "Horsemanship") {
		t.Errorf("CR 611.2a Riding the Dilu Horse seq %d: indefinite effect lost at cleanup, P/T=%d/%d horsemanship=%t", len(e.L.Events), e.Power(target), e.Toughness(target), e.HasKeyword(target, "Horsemanship"))
	}
}

func TestCR613HumilitySetsBaseBeforePumpAndRemovesAbilities(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	e := crResolutionEngine(t, []string{"Humility", "Giant Growth"}, nil)
	target := crAbortMove(t, e, 1, "Serra Avenger", state.ZBattlefield)
	growth := crAbortMove(t, e, 0, "Giant Growth", state.ZHand)
	hum := crAbortMove(t, e, 0, "Humility", state.ZBattlefield)
	guarded := false
	for _, st := range e.G.Obj(hum).Face().Statics {
		if st.Mode == "Continuous" && st.Params["SetPower"] == "1" && st.Params["SetToughness"] == "1" && st.Params["RemoveAllAbilities"] == "True" {
			guarded = true
		}
	}
	if !guarded || !e.G.Obj(target).Face().HasKeyword("Flying") {
		t.Fatal("CR 613 Humility/Serra Avenger seq 0: fixture changed")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.askPriority(0)
	crAbortAnswer(t, e, "Giant Growth", crAbortOption(t, e, "Giant Growth", "cast", growth))
	crAbortAnswer(t, e, "Giant Growth", crAbortOption(t, e, "Giant Growth", "permanent", target))
	crResolutionRound(t, e)
	if e.G.Obj(growth).Zone != state.ZGraveyard {
		t.Fatalf("CR 613 Giant Growth seq %d: spell did not finish", len(e.L.Events))
	}
	// Fixed oracle: base 1/1 (7b), then +3/+3 (7c), and no printed abilities (6).
	if e.Power(target) != 4 || e.Toughness(target) != 4 || e.HasKeyword(target, "Flying") || e.HasKeyword(target, "Vigilance") {
		t.Errorf("CR 613.1f/613.4b-c Humility/Giant Growth/Serra Avenger seq %d: got %d/%d flying=%t vigilance=%t; want 4/4 with neither ability", len(e.L.Events), e.Power(target), e.Toughness(target), e.HasKeyword(target, "Flying"), e.HasKeyword(target, "Vigilance"))
	}
}
