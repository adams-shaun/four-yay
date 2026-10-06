package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A parked boundary must resume the turn-based action of the step it entered,
// not just clear the remaining players' mana.
func TestLoseManaChoiceResumesDrawStep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675420, "Horizon Stone", "Ozai, the Phoenix King")
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: carrier %d absent", id)
		}
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	before := len(e.G.Zone(state.ZHand, 0))
	if e.G.Turn != 2 || e.G.Step != state.StepUpkeep || e.G.Players[0].Pool[state.MR] != 1 || len(e.G.Zone(state.ZLibrary, 0)) == 0 {
		t.Fatal("precondition: turn-2 upkeep with red mana and nonempty library required")
	}
	e.advanceStep()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || e.G.Step != state.StepDraw || len(e.G.Zone(state.ZHand, 0)) != before {
		t.Fatalf("precondition: draw boundary did not park before turn action: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("draw action after parked boundary: hand %d, want %d", got, before+1)
	}
	replayCheck(t, e, cfg)
}

// A converted ManaAdd may itself park a ProduceMana replacement. The boundary
// belongs to the last choice, regardless of which replacement kind it poses.
func TestLoseManaBoundaryResumesAfterProduceManaChoice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675421, "Horizon Stone", "Ozai, the Phoenix King", "Pulse of Llanowar", "Nyxbloom Ancient")
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: carrier %d absent", id)
		}
	}
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	if o := e.G.Obj(mountain); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: producer not a battlefield land")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "R", Amount: 1})
	if e.G.Players[0].Pool.Total() != 1 || e.G.Players[1].Pool.Total() != 1 {
		t.Fatal("precondition: both players need mana")
	}
	e.pending = nil
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
		t.Fatalf("precondition: LoseMana order not parked: %+v", d)
	}
	// Bind the production context for the converted ManaAdd to exercise the
	// nested replacement-choice path (normally boundary conversion is not a tap).
	e.manaProducer, e.manaFromTap = mountain, true
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionForObj(d, ids[0])}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || len(e.replChoices) == 0 || e.replChoices[0].kind != replChoiceMana || e.replChoices[0].manaBoundary == nil {
		t.Fatalf("precondition: converted mana did not park ProduceMana order ask: %+v", d)
	}
	if e.G.Players[1].Pool.Total() != 1 {
		t.Fatal("remaining seat cleared before nested answer")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionForObj(d, ids[2])}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 5 || len(e.replChoices) == 0 || e.replChoices[0].kind != replChoiceManaColor || e.replChoices[0].manaBoundary == nil || e.G.Players[1].Pool.Total() != 1 {
		t.Fatalf("precondition: colour follow-up lost boundary ownership: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatal(err)
	}
	e.manaProducer, e.manaFromTap = 0, false
	if e.G.Players[1].Pool.Total() != 0 || e.Pending() != nil && e.Pending().Kind == decision.KReplacement {
		t.Fatalf("boundary stalled after ProduceMana choice: pool=%v pending=%+v", e.G.Players[1].Pool, e.Pending())
	}
	replayCheck(t, e, cfg)
}
