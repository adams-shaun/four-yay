package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// 603.2 (4936-4937): trigger at condition time, do nothing yet.
// 603.3c/d (5000-5008): modes/targets when placed, not at resolution.
// 603.4 (5010-5023): intervening-if checked BOTH at occurrence and resolution.
// 603.5 (5025-5030): optional effects still go on stack; choose at resolution.
// 603.8 (5127-5138): state triggers fire as soon as their condition holds.
// Every leaf in this file passes with the conformance flag on and runs in the
// ordinary lane; the conformance flag gates nothing here.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func crTriggerFixture(t *testing.T, e *Engine, id state.ObjID, mode, api string) cards.Trigger {
	t.Helper()
	for _, tr := range e.G.Obj(id).Face().Triggers {
		if tr.Mode == mode && tr.Effect != nil && tr.Effect.API == api {
			return tr
		}
	}
	t.Fatalf("CR 603 %s seq %d: missing compiled %s/%s trigger", e.G.Obj(id).Face().Name, len(e.L.Events), mode, api)
	return cards.Trigger{}
}

func crTriggerStackCount(e *Engine, source state.ObjID) int {
	n := 0
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Ability != nil && o.Source == source {
			n++
		}
	}
	return n
}

// Resolve one stack object through actual priority intents, not resolveTop.
func crTriggerPassRound(t *testing.T, e *Engine, name string) {
	t.Helper()
	for i := 0; i < 2; i++ {
		if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
			t.Fatalf("CR 603 %s seq %d: expected priority before pass, got %+v", name, len(e.L.Events), d)
		}
		crAbortAnswer(t, e, name, crAbortOption(t, e, name, "pass", 0))
	}
}

func TestCR603LegalActivationTriggersRings(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Azure Mage", "Rings of Brighthearth")
	mage := crAbortMove(t, e, 0, "Azure Mage", state.ZBattlefield)
	rings := crAbortMove(t, e, 0, "Rings of Brighthearth", state.ZBattlefield)
	tr := crTriggerFixture(t, e, rings, "AbilityCast", "CopySpellAbility")
	if tr.Params["ValidActivatingPlayer"] != "You" || tr.Params["ValidSA"] != "SpellAbility.!ManaAbility" {
		t.Fatal("CR 603.2 Rings of Brighthearth seq 0: fixture changed")
	}
	crActivationSA(t, e, mage, "Draw", "3 U")
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 4})
	e.askPriority(0)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Azure Mage", crAbortOption(t, e, "Azure Mage", "ability", mage))
	if crTriggerStackCount(e, mage) != 1 {
		t.Fatalf("CR 603.2 Azure Mage seq %d: legal activation did not complete", start)
	}
	found := crTriggerStackCount(e, rings)
	for _, pt := range e.pendingTriggers {
		if pt.Source == rings {
			found++
		}
	}
	if found != 1 {
		t.Errorf("CR 603.2/602.2b Rings of Brighthearth seq %d: completed Azure Mage activation but trigger count=%d, want 1; stack=%v next=%+v", start, found, e.G.Stack, e.Pending())
	}
}

// Graduated: passes with the conformance flag on; runs in the ordinary lane.
func TestCR603OptionalEffectWaitsForResolution(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Stoneforge Mystic")
	start := len(e.L.Events)
	id := crAbortMove(t, e, 0, "Stoneforge Mystic", state.ZBattlefield)
	tr := crTriggerFixture(t, e, id, "ChangesZone", "ChangeZone")
	if tr.Params["OptionalDecider"] != "You" {
		t.Fatal("CR 603.5 Stoneforge Mystic seq 0: optional ETB fixture changed")
	}
	e.pending = nil // finish fixture setup and enter next priority boundary
	e.priorityRound()
	if crTriggerStackCount(e, id) != 1 || e.Pending() == nil || e.Pending().Kind != decision.KPriority {
		t.Errorf("CR 603.5 Stoneforge Mystic seq %d: may-search must be on stack before any optional answer; stack=%v next=%+v", start, e.G.Stack, e.Pending())
	}
}

func TestCR603TriggerModesChosenAtPlacement(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Knight of Autumn")
	start := len(e.L.Events)
	id := crAbortMove(t, e, 0, "Knight of Autumn", state.ZBattlefield)
	tr := crTriggerFixture(t, e, id, "ChangesZone", "Charm")
	if tr.Effect.Params["Choices"] == "" {
		t.Fatal("CR 603.3c Knight of Autumn seq 0: no compiled modes")
	}
	e.pending = nil
	e.priorityRound()
	// The counter and life-gain modes are legal with this board. A player
	// must choose before opponents can respond to the triggered ability.
	if d := e.Pending(); d == nil || d.Kind != decision.KModes {
		t.Errorf("CR 603.3c Knight of Autumn seq %d: priority returned before mode announcement; stack=%v next=%+v", start, e.G.Stack, d)
	}
}

func TestCR603InterveningIfCheckedAtTriggerTime(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Felidar Sovereign")
	id := crAbortMove(t, e, 0, "Felidar Sovereign", state.ZBattlefield)
	tr := crTriggerFixture(t, e, id, "Phase", "WinsGame")
	if tr.Params["LifeAmount"] != "GE40" || e.G.Players[0].Life != 20 {
		t.Fatal("CR 603.4 Felidar Sovereign seq 0: fixture changed")
	}
	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	found := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == id {
			found++
		}
	}
	if found != 0 {
		t.Errorf("CR 603.4 Felidar Sovereign seq %d: at 20 life (<40), queued %d win trigger(s), want 0", start, found)
	}
}

func TestCR603InterveningIfRecheckedAtResolution(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Scute Mob", "Beast Within", "Forest", "Forest", "Forest", "Forest", "Forest")
	var lands []state.ObjID
	for i := 0; i < 5; i++ {
		lands = append(lands, crAbortMove(t, e, 0, "Forest", state.ZBattlefield))
	}
	id := crAbortMove(t, e, 0, "Scute Mob", state.ZBattlefield)
	beast := crAbortMove(t, e, 0, "Beast Within", state.ZHand)
	tr := crTriggerFixture(t, e, id, "Phase", "PutCounter")
	if tr.Params["IsPresent"] != "Land.YouCtrl" || tr.Params["PresentCompare"] != "GE5" || e.G.Obj(id).Counter("P1P1") != 0 {
		t.Fatal("CR 603.4 Scute Mob seq 0: fixture changed")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 3})
	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.pending = nil
	e.priorityRound()
	if crTriggerStackCount(e, id) != 1 {
		t.Fatalf("CR 603.4 Scute Mob seq %d: condition true with five Forests, trigger missing", start)
	}
	// A real instant response destroys one of our own Forests. No impossible
	// opponent interleaving during a proposal and no injected condition change.
	crAbortAnswer(t, e, "Beast Within", crAbortOption(t, e, "Beast Within", "cast", beast))
	crAbortAnswer(t, e, "Beast Within", crAbortOption(t, e, "Beast Within", "permanent", lands[0]))
	crTriggerPassRound(t, e, "Beast Within")
	if e.G.Obj(lands[0]).Zone != state.ZGraveyard || crTriggerStackCount(e, id) != 1 {
		t.Fatalf("CR 603.4 Scute Mob seq %d: response did not destroy Forest above waiting trigger", start)
	}
	landCount := 0
	for _, oid := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(oid).Face() != nil && e.G.Obj(oid).Face().IsLand() {
			landCount++
		}
	}
	if landCount != 4 {
		t.Fatalf("CR 603.4 Scute Mob seq %d: expected four lands after response, got %d", start, landCount)
	}
	crTriggerPassRound(t, e, "Scute Mob")
	if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
		t.Errorf("CR 603.4 Scute Mob seq %d: after five -> four lands before resolution, gained %d counters, want 0 (end seq %d)", start, got, len(e.L.Events))
	}
}

func TestCR603StateTriggerFiresWhenConditionBecomesTrue(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Emperor Crocodile")
	if len(e.G.Zone(state.ZBattlefield, 0)) != 0 {
		t.Fatal("CR 603.8 Emperor Crocodile seq 0: initial board not empty")
	}
	start := len(e.L.Events)
	id := crAbortMove(t, e, 0, "Emperor Crocodile", state.ZBattlefield)
	tr := crTriggerFixture(t, e, id, "Always", "Sacrifice")
	if tr.Params["IsPresent"] != "Creature.Other+YouCtrl" || tr.Params["PresentCompare"] != "EQ0" {
		t.Fatal("CR 603.8 Emperor Crocodile seq 0: compiled condition changed")
	}
	// It is our ONLY permanent. The condition holds upon entry, without
	// waiting for a priority grant; no target/cost parser derives this oracle.
	found := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == id {
			found++
		}
	}
	if found != 1 {
		t.Errorf("CR 603.8 Emperor Crocodile seq %d: sole creature entered but state trigger queued %d times, want 1", start, found)
	}
}
