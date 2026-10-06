package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestHorizonStoneKeepsSnowAndTypedMana(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675401, "Horizon Stone")
	if o := e.G.Obj(ids[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Horizon Stone not on battlefield")
	}
	for _, counter := range []string{"SR", "ArtifactTreasureR", "DesertR"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: counter})
	}
	if p := e.G.Players[0]; p.Pool[state.MR] != 3 || p.Snow[state.MR] != 1 || p.ArtifactTyped[state.TypedTreasure][state.MR] != 1 || p.TypedMana[state.TypedDesert][state.MR] != 1 {
		t.Fatalf("precondition: tagged red pool = %+v", p)
	}
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	p := e.G.Players[0]
	if p.Pool[state.MC] != 3 || p.Pool[state.MR] != 0 || p.Snow[state.MC] != 1 || p.TypedMana[state.TypedTreasure][state.MC] != 1 || p.ArtifactTyped[state.TypedTreasure][state.MC] != 1 || p.TypedMana[state.TypedDesert][state.MC] != 1 {
		t.Fatalf("converted tags lost: pool=%v snow=%v typed=%v artifact=%v", p.Pool, p.Snow, p.TypedMana, p.ArtifactTyped)
	}
	replayCheck(t, e, cfg)
}

func TestLoseManaBoundaryWaitsForChoiceBeforeOtherSeatsAndCombatReset(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675402, "Horizon Stone", "Ozai, the Phoenix King")
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: carrier %d not in battlefield", id)
		}
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "R"})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Amount: 1, Counter: "U"})
	if e.G.Players[0].Pool.Total() != 1 || e.G.Players[1].Pool.Total() != 1 {
		t.Fatal("precondition: two seats must have mana")
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEndCombat})
	e.setStep(state.StepMain2)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
		t.Fatalf("precondition: expected two competing replacements, got %+v", d)
	}
	if e.G.Players[1].Pool.Total() != 1 {
		t.Fatalf("second seat cleared before choice: %v", e.G.Players[1].Pool)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.EndCombatReset {
			t.Fatal("combat reset ran before choice")
		}
	}
	ozai := state.ObjID(0)
	for _, id := range ids {
		if o := e.G.Obj(id); o != nil && o.Card != nil && o.Card.Faces[0].Name == "Ozai, the Phoenix King" {
			ozai = id
		}
	}
	if ozai == 0 || optionForObj(d, ozai) < 0 {
		t.Fatalf("precondition: Ozai not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionForObj(d, ozai)}}); err != nil {
		t.Fatal(err)
	}
	if p := e.G.Players[0].Pool; p.Total() != 1 || p[state.MR] != 0 {
		t.Fatalf("first seat not converted: %v", p)
	}
	if e.G.Players[1].Pool.Total() != 0 {
		t.Fatalf("second seat not cleared after choice: %v", e.G.Players[1].Pool)
	}
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.EndCombatReset {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("end-combat reset count = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}
