package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// kr4WardTarget seeds a seat-1 targeting object at warded and resolves the
// ward trigger under a kernel probe, leaving its pay election posed.
func kr4WardTarget(e *Engine, warded state.ObjID) {
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.pending = nil
	e.resolveTop()
}

// TestWardBlightMayUseATappedCreatureAndPoisonLosesKernel: Auntie Ool's
// Ward—Blight 2 may be paid with a TAPPED creature (two -1/-1 counters land
// on it), and The Serpent Society's poison ward paid at 5 poison loses the
// game at 10.
func TestWardBlightMayUseATappedCreatureAndPoisonLosesKernel(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Auntie Ool, Cursewretch"))
	blighted := onBoard(t, e, 1, "Name:Tapped payment\nTypes:Creature\nPT:3/3\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: blighted})
	kr4WardTarget(e, warded)
	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("ward pay election not posed to seat 1: %+v", d)
	}
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != blighted {
		t.Fatalf("tapped Blight candidate = %+v, want tapped creature", d)
	}
	submitChoices(t, e, 0)
	if got := e.G.Obj(blighted).Counter("M1M1"); got != 2 {
		t.Fatalf("Blight counters = %d, want 2", got)
	}

	e2 := combatEngine(t)
	serpent := onBoardCard(t, e2, 0, corpusKeywordCard(t, "The Serpent Society"))
	e2.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 5})
	kr4WardTarget(e2, serpent)
	if d := e2.Pending(); d == nil || d.Player != 1 {
		t.Fatalf("poison ward pay election not posed to seat 1: %+v", d)
	}
	submitChoices(t, e2, 0)
	e2.checkStateBased() // the probed resolution is over: the flow would check SBAs next
	if !e2.G.Players[1].Lost || e2.G.Players[1].Counter("POISON") != 10 {
		t.Fatalf("five poison Ward payment: lost/counters = %v/%d, want true/10", e2.G.Players[1].Lost, e2.G.Players[1].Counter("POISON"))
	}
}

// TestCombatDamageReplacementAsksQueueInEventOrderKernel: two combat damage
// assignments each meet Nefarious Lich's replacement, whose ReplaceWith$ body
// asks for a graveyard exile pick. Both picks are posed in event order (never
// overwriting each other), each exiles 2, and the damage is replaced.
func TestCombatDamageReplacementAsksQueueInEventOrderKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Nefarious Lich"))
	a := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	b := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	for i := 0; i < 5; i++ {
		id := e.G.Zone(state.ZLibrary, 0)[0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	life := e.G.Players[0].Life
	e.combatRound.assignments = []assignment{
		{from: a, toPlayer: 0, amount: 2},
		{from: b, toPlayer: 0, amount: 2},
	}
	e.pending = nil
	e.probe(e.runCombatAssignments)
	for round := 0; round < 2; round++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
			t.Fatalf("round %d: want seat 0's graveyard pick, got %+v", round, d)
		}
		if d.Min != 2 || len(d.Options) < 2 {
			t.Fatalf("round %d: want a pick of exactly 2, got min=%d max=%d options=%d", round, d.Min, d.Max, len(d.Options))
		}
		submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("a third graveyard pick was posed: %+v", d)
	}
	if got := len(e.G.Zone(state.ZExile, 0)); got != 4 {
		t.Fatalf("exiled %d cards from seat 0's graveyard, want 4 (2 per damage event)", got)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 1 {
		t.Fatalf("graveyard holds %d cards, want 1", got)
	}
	if e.G.Players[0].Life != life || e.G.Players[0].Lost {
		t.Fatalf("life %d -> %d lost %v; the damage should have been replaced", life, e.G.Players[0].Life, e.G.Players[0].Lost)
	}
}
