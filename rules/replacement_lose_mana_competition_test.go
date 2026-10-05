package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestLoseManaCompetingCarriersReapplyAfterChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675320, "Horizon Stone", "Ozai, the Phoenix King")
	if len(ids) != 2 {
		t.Fatalf("precondition: got %d carrier objects, want 2", len(ids))
	}
	for i, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: replacement %d is not on battlefield", i)
		}
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 2, Counter: "R"})
	if e.G.Players[0].Pool.Total() != 2 {
		t.Fatal("precondition: two unspent red mana not established")
	}
	e.pending = nil // Exercise the step-boundary event directly, outside priority.
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
		t.Fatalf("initial pending = %+v, want replacement-order choice over both carriers", d)
	}
	// Apply Ozai first; the still-applicable Horizon Stone then applies to the
	// rewritten loss, converting the result to colorless without another choice.
	ozaiID := state.ObjID(0)
	for _, id := range ids {
		if o := e.G.Obj(id); o != nil && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0].Name == "Ozai, the Phoenix King" {
			ozaiID = id
		}
	}
	if ozaiID == 0 || optionForObj(d, ozaiID) < 0 {
		t.Fatalf("precondition: Ozai not among replacement options: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionForObj(d, ozaiID)}}); err != nil {
		t.Fatal(err)
	}
	if d := e.Pending(); d == nil || d.Kind == decision.KReplacement {
		t.Fatalf("a single remaining replacement should apply without another order choice: %+v", d)
	}
	if got := e.G.Players[0].Pool; got[state.MC] != 2 || got[state.MR] != 0 {
		t.Fatalf("pool after ordered replacements = %v, want two colorless", got)
	}
	replayCheck(t, e, cfg)
}
