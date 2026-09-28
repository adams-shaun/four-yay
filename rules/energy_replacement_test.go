package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The player-counter (ENERGY) carriers of the DB$ ReplaceCounter class pinned
// end to end on the real corpus cards: Aether Refinery (Twice) and Izzet
// Generatorium (ReplaceCount$CounterNum/Plus.1). Both carry
// `R:Event$ AddCounter | ValidPlayer$ You | ValidCounterType$ ENERGY` with a
// `DB$ ReplaceCounter` body, so the event intercepted is the
// PlayerCounterChange shape — the object-counter carriers (Hardened Scales,
// Branching Evolution) are pinned in addcounter_replacement_test.go and do
// not reach the player-form gate.
//
// The energy source is an authored fixture artifact (never a corpus .txt,
// per the licensing rule) whose ability targets a PLAYER, so every placement
// rides the engine's own effPutCounter emit path.

// energyReplSource builds the authored energy artifact: activate it and it
// puts n {E} on the chosen player through effPutCounter's real
// PlayerCounterChange path.
func energyReplSource(t testing.TB, n int) *cards.Card {
	return card(t, "Name:Energy Source\nTypes:Artifact\n"+
		"A:AB$ PutCounter | Cost$ T | ValidTgts$ Player | CounterType$ ENERGY | CounterNum$ "+strconv.Itoa(n)+" | SpellDescription$ The chosen player gets {E}.\n"+
		"Oracle:x\n")
}

// boardWithEnergyReplacement seeds the named corpus replacement card and the
// authored energy source onto seat 0's battlefield.
func boardWithEnergyReplacement(t *testing.T, seed uint64, replName string, placed int) (*Engine, Config, state.ObjID) {
	t.Helper()
	repl := tokenReplCorpusCard(t, replName)
	src := energyReplSource(t, placed)
	e, cfg := tokenReplGame(t, seed, repl, src)
	moveSeededCard(t, e, 0, repl, state.ZBattlefield)
	sourceID := moveSeededCard(t, e, 0, src, state.ZBattlefield)
	return e, cfg, sourceID
}

// activateEnergySourceOn activates the energy source and targets the given
// seat, then drains the stack.
func activateEnergySourceOn(t *testing.T, e *Engine, source state.ObjID, seat state.PlayerID) {
	t.Helper()
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, source, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision for the energy source: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == seat {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("player %d not offerable as a target: %+v", seat, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// TestAetherRefineryDoublesEnergy is the filing card, end to end: a real
// PlayerCounterChange ENERGY placement on its controller doubles (1 -> 2,
// 3 -> 6), while the same placement on the OPPONENT does not (ValidPlayer$
// You) and neither does a non-ENERGY kind (ValidCounterType$).
func TestAetherRefineryDoublesEnergy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		placed, want int32
	}{
		{1, 2},
		{3, 6},
	} {
		e, cfg, source := boardWithEnergyReplacement(t, 4102, "Aether Refinery", int(tc.placed))
		activateEnergySourceOn(t, e, source, 0)
		if got := e.G.Players[0].Counter("ENERGY"); got != tc.want {
			t.Fatalf("Aether Refinery: %d ENERGY placed on its controller = %d, want %d", tc.placed, got, tc.want)
		}
		if got := e.G.Players[1].Counter("ENERGY"); got != 0 {
			t.Fatalf("Aether Refinery: opponent ENERGY = %d, want 0 (leak)", got)
		}
		replayCheck(t, e, cfg)
	}

	// The ValidPlayer$ You gate: the SAME ability targeting seat 1 leaves the
	// opponent's energy untouched.
	e, cfg, source := boardWithEnergyReplacement(t, 4103, "Aether Refinery", 1)
	activateEnergySourceOn(t, e, source, 1)
	if got := e.G.Players[1].Counter("ENERGY"); got != 1 {
		t.Fatalf("Aether Refinery: 1 ENERGY placed on the opponent = %d, want 1 (ValidPlayer$ You must not fire on another player)", got)
	}
	replayCheck(t, e, cfg)

	// The ValidCounterType$ gate: a P1P1 placement is untouched.
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e2, cfg2 := tokenReplGame(t, 4104, target)
	targetID := moveSeededCard(t, e2, 0, target, state.ZBattlefield)
	e2.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 1})
	if got := e2.G.Obj(targetID).Counter("P1P1"); got != 1 {
		t.Fatalf("Aether Refinery rewrote a non-ENERGY placement: 1 P1P1 = %d, want 1", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestIzzetGeneratoriumAddsOneEnergy pins the Plus.1 body: a real placement
// on its controller becomes that many plus one (1 -> 2), and the same
// placement on the opponent does not (ValidPlayer$ You).
func TestIzzetGeneratoriumAddsOneEnergy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		placed, want int32
	}{
		{1, 2},
		{2, 3},
	} {
		e, cfg, source := boardWithEnergyReplacement(t, 4105, "Izzet Generatorium", int(tc.placed))
		activateEnergySourceOn(t, e, source, 0)
		if got := e.G.Players[0].Counter("ENERGY"); got != tc.want {
			t.Fatalf("Izzet Generatorium: %d ENERGY placed on its controller = %d, want %d", tc.placed, got, tc.want)
		}
		if got := e.G.Players[1].Counter("ENERGY"); got != 0 {
			t.Fatalf("Izzet Generatorium: opponent ENERGY = %d, want 0 (leak)", got)
		}
		replayCheck(t, e, cfg)
	}

	e, cfg, source := boardWithEnergyReplacement(t, 4106, "Izzet Generatorium", 1)
	activateEnergySourceOn(t, e, source, 1)
	if got := e.G.Players[1].Counter("ENERGY"); got != 1 {
		t.Fatalf("Izzet Generatorium: 1 ENERGY placed on the opponent = %d, want 1 (ValidPlayer$ You must not fire on another player)", got)
	}
	replayCheck(t, e, cfg)
}

// TestEnergyReplacementsComposeScanOrder pins that BOTH carriers live on one
// board compose on the running total with no interference, in deterministic
// scan order (battlefield insertion order: Aether Refinery's Twice body first,
// Izzet Generatorium's Plus.1 second): 1 -> 2 -> 3, the CR 616.1e read the
// object-counter pins (TestAddCounterReplacementsComposeScanOrder) already
// assert for Hardened Scales + Branching Evolution.
func TestEnergyReplacementsComposeScanOrder(t *testing.T) {
	t.Parallel()
	refinery := tokenReplCorpusCard(t, "Aether Refinery")
	generatorium := tokenReplCorpusCard(t, "Izzet Generatorium")
	src := energyReplSource(t, 1)
	e, cfg := tokenReplGame(t, 4107, refinery, generatorium, src)
	moveSeededCard(t, e, 0, refinery, state.ZBattlefield)
	moveSeededCard(t, e, 0, generatorium, state.ZBattlefield)
	sourceID := moveSeededCard(t, e, 0, src, state.ZBattlefield)
	activateEnergySourceOn(t, e, sourceID, 0)
	if got := e.G.Players[0].Counter("ENERGY"); got != 3 {
		t.Fatalf("Aether Refinery + Izzet Generatorium: 1 ENERGY composed = %d, want 3 (double then plus-one, scan order)", got)
	}
	replayCheck(t, e, cfg)
}

// TestEnergyReplacementsPoseNoDecisionWhenTrivial guards the strict-supersets
// convention: with the counter kind fixed by the R: line's ValidCounterType$,
// the rewrite is a pure event rewrite and poses NO mid-resolution ask — the
// activation drains to an empty stack and the pending decision is the plain
// priority pass, not a KChoose/KModes a rewrite wrongly posed.
func TestEnergyReplacementsPoseNoDecisionWhenTrivial(t *testing.T) {
	t.Parallel()
	for _, replName := range []string{"Aether Refinery", "Izzet Generatorium"} {
		e, _, source := boardWithEnergyReplacement(t, 4108, replName, 1)
		activateEnergySourceOn(t, e, source, 0)
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("%s: pending decision after the activation drained is %s, want priority (a rewrite must not pose an ask): %+v", replName, d.Kind, d)
		}
	}
}
