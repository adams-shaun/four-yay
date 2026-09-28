package rules

// Task fdn-trigger-gates: two trigger-side requirement parameters were unread,
// so their triggers fired when they must not.
//
//   - `OpponentTurn$ True` on `T:Mode$ SpellCast` / `T:Mode$ Drawn`: the mirror
//     of the existing `PlayerTurn$ True` gate in triggerMatches. Before it the
//     trigger fired on its controller's own turn too (the over-fire direction).
//     Driver: Brineborn Cutthroat.
//   - `Threshold$ True` on `T:Mode$ Attacks` / `AttackersDeclared` /
//     `ChangesZone`: the intervening-if "seven or more cards in your
//     graveyard", read through the ONE thresholdHolds census the Continuous
//     static gate already reads. Drivers: Crypt Feaster (required pump) and
//     Kiora, the Rising Tide (optional token).
//
// Each test drives the REAL compiled corpus card (never a re-written copy) and
// asserts the precondition the assertion depends on, so a vacuous setup fails
// loudly.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// fdnBrinebornEngine seeds seat 0 with the real Brineborn Cutthroat on the
// battlefield (its controller) plus a Lightning Bolt in hand to cast, and
// returns the engine, its Config, the Cutthroat id and the Bolt id.
func fdnBrinebornEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	bolt := lookup(t, reg, "Lightning Bolt")
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Brineborn Cutthroat"), bolt},
		[]*cards.Card{})
	ct := moveByName(t, e, 0, "Brineborn Cutthroat", state.ZBattlefield)
	spellID := moveByName(t, e, 0, "Lightning Bolt", state.ZHand)
	// Preconditions: the source is on the battlefield under seat 0, the spell
	// is in seat 0's hand, and no counter is on the Cutthroat yet.
	if e.G.Obj(ct).Zone != state.ZBattlefield || e.G.Obj(ct).Controller != 0 {
		t.Fatalf("precondition failed: Brineborn zone=%s controller=%d", e.G.Obj(ct).Zone, e.G.Obj(ct).Controller)
	}
	if e.G.Obj(spellID).Zone != state.ZHand {
		t.Fatalf("precondition failed: Lightning Bolt zone=%s, want hand", e.G.Obj(spellID).Zone)
	}
	if got := e.G.Obj(ct).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition failed: Brineborn P1P1=%d, want 0", got)
	}
	return e, cfg, ct, spellID
}

// TestBrinebornCutthroatOpponentTurnGate is the two-direction pin: on the
// controller's OWN turn the SpellCast trigger must not fire (no counter), and
// on the opponent's turn it must (one counter). The own-turn half is the
// regression the ticket fixes; the opponent-turn half proves the gate is not
// simply denying every spell.
func TestBrinebornCutthroatOpponentTurnGate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	t.Run("own turn does not fire", func(t *testing.T) {
		e, cfg, ct, spellID := fdnBrinebornEngine(t, reg)
		if e.G.Active != e.controllerOf(ct) {
			t.Fatalf("precondition failed: active=%d controller=%d, want the controller's own turn", e.G.Active, e.controllerOf(ct))
		}
		fdnPutSpellOnStack(e, spellID)
		if n := len(e.pendingTriggers); n != 0 {
			t.Fatalf("Brineborn's SpellCast trigger queued %d instance(s) on its own turn, want 0", n)
		}
		e.putTriggersOnStack()
		if n := len(e.pendingTriggers); n != 0 {
			t.Fatalf("Brineborn's SpellCast trigger queued %d instance(s) on its own turn, want 0", n)
		}
		if got := e.G.Obj(ct).Counter("P1P1"); got != 0 {
			t.Fatalf("Brineborn P1P1 after casting on its own turn = %d, want 0", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("opponent turn fires", func(t *testing.T) {
		e, cfg, ct, spellID := fdnBrinebornEngine(t, reg)
		// Drive with real priority submissions to seat 1's turn so the replay
		// rebuilds the same active player (a bare e.G.Active write would not).
		driveToStep(t, e, 2, 1, state.StepMain1)
		if e.G.Active == e.controllerOf(ct) {
			t.Fatalf("precondition failed: active=%d controller=%d, want an opponent's turn", e.G.Active, e.controllerOf(ct))
		}
		fdnPutSpellOnStack(e, spellID)
		if n := len(e.pendingTriggers); n != 1 {
			t.Fatalf("Brineborn's SpellCast trigger queued %d instance(s) on the opponent's turn, want 1", n)
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("Brineborn's trigger did not reach the stack on the opponent's turn")
		}
		e.resolveTop()
		if got := e.G.Obj(ct).Counter("P1P1"); got != 1 {
			t.Fatalf("Brineborn P1P1 after casting on the opponent's turn = %d, want 1", got)
		}
		replayCheck(t, e, cfg)
	})
}

// fdnPutSpellOnStack puts the spell on the stack from seat 0's hand, matching
// the PutOnStack event the SpellCast trigger mode fires off.
func fdnPutSpellOnStack(e *Engine, spellID state.ObjID) {
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spellID, Player: 0, From: state.ZHand, To: state.ZStack})
}

// fdnAttackEngine seeds seat 0 with the real `name` creature on the
// battlefield, drains any enters-the-battlefield trigger it queued, moves
// `grave` Mountains from its library to its graveyard, and returns the
// engine, Config and the creature id. The active player is seat 0 (from
// corpusEngineCfg), so a subsequent DeclareAttackers is buildable.
func fdnAttackEngine(t *testing.T, reg *cards.Registry, name string, grave int) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, name)}, []*cards.Card{})
	id := moveByName(t, e, 0, name, state.ZBattlefield)
	fdnDrainStack(t, e)
	// Top the graveyard up to exactly `grave` cards: an ETB that discards
	// (Kiora draws two then discards two) may already have put cards there,
	// and a fixed move count would overshoot and silently break threshold.
	for len(e.G.Zone(state.ZGraveyard, 0)) < grave {
		moveByName(t, e, 0, "Mountain", state.ZGraveyard)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != grave {
		t.Fatalf("precondition failed: seat 0 graveyard holds %d cards, want %d", got, grave)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Controller != 0 {
		t.Fatalf("precondition failed: %s zone=%s controller=%d", name, e.G.Obj(id).Zone, e.G.Obj(id).Controller)
	}
	if n := len(e.pendingTriggers); n != 0 {
		t.Fatalf("precondition failed: %d trigger(s) still queued before the declaration", n)
	}
	return e, cfg, id
}

// fdnDrainStack settles every ability already on the stack (an ETB like
// Kiora's draw-two-discard-two), answering any mid-resolution decision with
// its first option. It never passes priority, so it cannot advance the step.
func fdnDrainStack(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) == 0 {
			return
		}
		if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
			choices := make([]int, 0, d.Min)
			for j := 0; j < d.Min && j < len(d.Options); j++ {
				choices = append(choices, j)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("drain submit (%s min=%d): %v", d.Kind, d.Min, err)
			}
			continue
		}
		e.resolveTop()
	}
	t.Fatalf("the stack did not settle draining (%d pending, %d on the stack)", len(e.pendingTriggers), len(e.G.Stack))
}

// TestCryptFeasterThresholdAttacksGate pins Crypt Feaster's required
// "Threshold -- ... gets +2/+0" attacks trigger in both directions: six cards
// in the graveyard is below threshold and must not pump, seven is at threshold
// and must pump 3/4 -> 5/4.
func TestCryptFeasterThresholdAttacksGate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	t.Run("below threshold does not pump", func(t *testing.T) {
		e, cfg, id := fdnAttackEngine(t, reg, "Crypt Feaster", 6)
		if base := e.Derived(id).Power; base != 3 {
			t.Fatalf("precondition failed: Crypt Feaster power=%d, want printed 3", base)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
		if n := len(e.pendingTriggers); n != 0 {
			t.Fatalf("Crypt Feaster's Threshold attacks trigger queued %d instance(s) with 6 graveyard cards, want 0", n)
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) != 0 {
			t.Fatalf("Crypt Feaster's trigger reached the stack below threshold (%d entries)", len(e.G.Stack))
		}
		if got := e.Derived(id).Power; got != 3 {
			t.Fatalf("Crypt Feaster power with 6 graveyard cards = %d, want 3 (unpumped)", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("at threshold pumps", func(t *testing.T) {
		e, cfg, id := fdnAttackEngine(t, reg, "Crypt Feaster", 7)
		if base := e.Derived(id).Power; base != 3 {
			t.Fatalf("precondition failed: Crypt Feaster power=%d, want printed 3", base)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
		if n := len(e.pendingTriggers); n != 1 {
			t.Fatalf("Crypt Feaster's Threshold attacks trigger queued %d instance(s) with 7 graveyard cards, want 1", n)
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("Crypt Feaster's Threshold attacks trigger did not reach the stack")
		}
		e.resolveTop()
		if got := e.Derived(id).Power; got != 5 {
			t.Fatalf("Crypt Feaster power with 7 graveyard cards = %d, want 5 (+2/+0)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestKioraThresholdOptionalTokenGate pins Kiora, the Rising Tide's OPTIONAL
// "Threshold -- ... you may create Scion of the Deep" attacks trigger: below
// threshold the optional ask is never offered, at threshold it is.
func TestKioraThresholdOptionalTokenGate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	t.Run("below threshold no optional ask", func(t *testing.T) {
		e, cfg, id := fdnAttackEngine(t, reg, "Kiora, the Rising Tide", 6)
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition failed: Kiora zone=%s", e.G.Obj(id).Zone)
		}
		before := e.Pending()
		if before == nil || before.Kind != decision.KPriority {
			t.Fatalf("precondition failed: expected a priority decision before the declaration, got %+v", before)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
		if n := len(e.pendingTriggers); n != 0 {
			t.Fatalf("Kiora's Threshold trigger queued %d instance(s) with 6 graveyard cards, want 0", n)
		}
		e.putTriggersOnStack()
		if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOptional {
			t.Fatalf("Kiora's optional token ask was offered below threshold: %+v", d)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("at threshold offers the optional ask", func(t *testing.T) {
		e, cfg, id := fdnAttackEngine(t, reg, "Kiora, the Rising Tide", 7)
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 7 {
			t.Fatalf("precondition failed: seat 0 graveyard holds %d cards, want 7", got)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
		if n := len(e.pendingTriggers); n != 1 {
			t.Fatalf("Kiora's Threshold trigger queued %d instance(s) with 7 graveyard cards, want 1", n)
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("Kiora's Threshold trigger did not reach the stack")
		}
		e.resolveTop()
		d := e.Pending()
		if d == nil || d.Kind != decision.KTriggerOptional {
			t.Fatalf("Kiora's optional token ask = %+v, want KTriggerOptional at threshold", d)
		}
		replayCheck(t, e, cfg)
	})
}

// TestTriggerOpponentTurnAndThresholdAreRead pins the parameter census the
// ticket's "Done means" names: the derived read set for the modes the corpus
// carries the two params on must include them, so a later deletion of either
// read makes the census flag the param again. Brineborn's own card is not in a
// repo deck, so the deck-level ratchet cannot see this; this pins the
// scan-level read directly.
func TestTriggerOpponentTurnAndThresholdAreRead(t *testing.T) {
	t.Parallel()
	_, d := measureParamCensus(t, nil)
	for _, tc := range []struct{ mode, param string }{
		{"SpellCast", "OpponentTurn"},
		{"Drawn", "OpponentTurn"},
		{"Attacks", "Threshold"},
		{"AttackersDeclared", "Threshold"},
		{"ChangesZone", "Threshold"},
	} {
		read := d.trig[tc.mode]
		if read == nil {
			t.Fatalf("census has no derived read set for trig:%s", tc.mode)
		}
		if !read[tc.param] {
			t.Errorf("trig:%s does not read %s$ -- the trigger-side gate is not registered (still unread)", tc.mode, tc.param)
		}
	}
}

// TestFdnTriggerGateCarriersExist is the population precondition: every card
// this file drives really carries the parameter under test, so a corpus pin
// move that drops one makes the fixture stale loudly instead of testing
// nothing.
func TestFdnTriggerGateCarriersExist(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	for _, tc := range []struct{ name, mode, param string }{
		{"Brineborn Cutthroat", "SpellCast", "OpponentTurn"},
		{"Crypt Feaster", "Attacks", "Threshold"},
		{"Kiora, the Rising Tide", "Attacks", "Threshold"},
	} {
		c := lookup(t, reg, tc.name)
		found := false
		for _, f := range c.Faces {
			for _, tr := range f.Triggers {
				if tr.Mode == tc.mode && strings.EqualFold(strings.TrimSpace(tr.Params[tc.param]), "True") {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s carries no Mode$ %s %s$ True trigger at this corpus pin", tc.name, tc.mode, tc.param)
		}
	}
}
