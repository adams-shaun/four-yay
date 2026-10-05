package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestLoseManaBoundaryChoiceClonesAndResumes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675403, "Horizon Stone", "Ozai, the Phoenix King")
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("precondition: need two distinct sources, got %v", ids)
	}
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: replacement source %d not on battlefield", id)
		}
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "R"})
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatal("precondition: no red mana to replace")
	}
	e.pending = nil
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || len(e.replChoices) == 0 || e.replChoices[len(e.replChoices)-1].manaBoundary == nil {
		t.Fatalf("precondition: boundary competition not parked: %+v", d)
	}
	// A phase replacement can have been suspended by this same mana choice.
	// Seed its saved candidate with a mutable remembered slice to verify the
	// generated clone deepens both links of the continuation, not just the queue.
	original := e.replChoices[len(e.replChoices)-1].manaBoundary
	original.phase = &parkedPhaseFinish{ev: events.Event{Kind: events.StepChange, Step: state.StepBeginCombat},
		cands: []replMatch{{remembered: []state.ObjID{ids[0]}}}}
	clone := e.Clone()
	if clone.Pending() == nil || len(clone.replChoices) == 0 || clone.replChoices[len(clone.replChoices)-1].manaBoundary == nil {
		t.Fatal("clone lost parked boundary")
	}
	copied := clone.replChoices[len(clone.replChoices)-1].manaBoundary
	if copied.phase == nil || len(copied.phase.cands) != 1 || len(copied.phase.cands[0].remembered) != 1 {
		t.Fatal("clone lost parked phase and its replacement candidate")
	}
	copied.phase.cands[0].remembered[0] = ids[1]
	if original.phase.cands[0].remembered[0] != ids[0] {
		t.Fatal("clone aliases the original phase candidate")
	}
	// The synthetic phase candidate only tests clone isolation; leave the
	// reachable mana-only continuation for the lockstep answer below.
	original.phase, copied.phase = nil, nil
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
	for _, game := range []*Engine{e, clone} {
		if err := game.Submit(intent); err != nil {
			t.Fatal(err)
		}
		if game.Pending() != nil && game.Pending().Kind == decision.KReplacement {
			t.Fatal("boundary replacement remained parked after answer")
		}
	}
	if e.L.Head() != clone.L.Head() || e.G.Players[0].Pool != clone.G.Players[0].Pool {
		t.Fatalf("clone diverged: heads %s/%s, pools %v/%v", e.L.Head(), clone.L.Head(), e.G.Players[0].Pool, clone.G.Players[0].Pool)
	}
	replayCheck(t, clone, cfg)
}
