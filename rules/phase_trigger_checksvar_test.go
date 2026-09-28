package rules

// The Phase-trigger CheckSVar$/SVarCompare$ gate (rules/trigger_match.go's
// triggerConditionHoldsAs, the CR 603.4 intervening-if): a Mode$ Phase
// trigger whose firing is gated by a named SVar threshold check fires only
// when the comparison holds. Every fixture below drives the REAL compiled
// corpus card (the scripts are GPL and live only in gitignored .cards/).
// The two shapes were picked to be genuinely different:
//
//   - Angelic Accord: CheckSVar$ YouLifeGained | SVarCompare$ GE4, where
//     SVar:YouLifeGained is Count$LifeYouGainedThisTurn — a log-derived
//     count of one player's own life gained this turn (effects.Host's
//     LifeGainedThisTurn).
//   - Land Tax: CheckSVar$ Y | SVarCompare$ GTX, where SVar:Y is
//     PlayerCountOpponents$HighestValid Land.YouCtrl — the highest, over the
//     opponents, of each opponent's controlled-land count (the threshold
//     itself is another SVar name, X = Count$Valid Land.YouCtrl), so the
//     gate is a per-player valid-count extreme compared against a
//     SVar-resolved threshold.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPhaseTriggerCheckSVarLifeGainedGatesAngelicAccord drives the real
// corpus Angelic Accord across the CR 603.4 gate: at an end step where you
// gained 3 life (< 4), the trigger does not fire at all; after one more
// gain (4 this turn), it fires, resolves and creates the Angel token.
func TestPhaseTriggerCheckSVarLifeGainedGatesAngelicAccord(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Angelic Accord")
	acc := crAbortMove(t, e, 0, "Angelic Accord", state.ZBattlefield)
	tr := crTriggerFixture(t, e, acc, "Phase", "Token")
	if tr.Params["CheckSVar"] != "YouLifeGained" || tr.Params["SVarCompare"] != "GE4" {
		t.Fatal("Angelic Accord seq 0: fixture changed (CheckSVar$/SVarCompare$ missing)")
	}

	// Three life gained this turn: below the GE4 threshold, the trigger
	// never fires (CR 603.4: the condition is checked when the trigger event
	// occurs; false means it does not trigger).
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	e.pending = nil
	e.priorityRound()
	if n := crTriggerStackCount(e, acc); n != 0 {
		t.Fatalf("Angelic Accord: 3 life gained (< 4) queued %d trigger(s), want 0", n)
	}
	if angels := angelTokens(e, 0); len(angels) != 0 {
		t.Fatalf("Angelic Accord: suppressed run created %d Angel token(s)", len(angels))
	}

	// One more gain makes the turn's total 4: the gate now holds, the
	// trigger fires and resolves, and the token appears.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	e.pending = nil
	e.priorityRound()
	if n := crTriggerStackCount(e, acc); n != 1 {
		t.Fatalf("Angelic Accord: 4 life gained (GE4) queued %d trigger(s), want 1", n)
	}
	crTriggerPassRound(t, e, "Angelic Accord")
	passUntilStackEmpty(t, e, 20)
	angels := angelTokens(e, 0)
	if len(angels) != 1 {
		t.Fatalf("Angelic Accord: %d Angel token(s) after the gated trigger resolved, want 1", len(angels))
	}
	tok := e.G.Obj(angels[0])
	if tok.Face() == nil || tok.Face().Power() != 4 || tok.Face().Toughness() != 4 || !e.HasKeyword(angels[0], "Flying") {
		t.Fatalf("Angelic Accord: token %v is not the 4/4 flying Angel", tok.Face())
	}
}

// TestPhaseTriggerCheckSVarHighestValidGatesLandTax drives the real corpus
// Land Tax across the gate's other shape: an opponents' highest
// controlled-land count compared against an SVar-resolved threshold (your
// own land count) with GTX. With the best opponent's land count at or below
// yours the trigger does not fire; once an opponent controls more lands than
// you, it fires, resolves, and its OptionalDecider$ ask and library search
// run.
func TestPhaseTriggerCheckSVarHighestValidGatesLandTax(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Land Tax")
	tax := crAbortMove(t, e, 0, "Land Tax", state.ZBattlefield)
	crAbortMove(t, e, 0, "Island", state.ZBattlefield) // you control 1 land
	plains := crAbortMove(t, e, 1, "Plains", state.ZBattlefield)
	tr := crTriggerFixture(t, e, tax, "Phase", "ChangeZone")
	if tr.Params["CheckSVar"] != "Y" || tr.Params["SVarCompare"] != "GTX" {
		t.Fatal("Land Tax seq 0: fixture changed (CheckSVar$/SVarCompare$ missing)")
	}
	if e.G.Obj(plains) == nil || e.G.Obj(plains).Controller != 1 {
		t.Fatal("Land Tax seq 0: fixture opponent Plains missing")
	}

	// Equal land counts (best opponent 1, you 1): GTX fails, no trigger.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.pending = nil
	e.priorityRound()
	if n := crTriggerStackCount(e, tax); n != 0 {
		t.Fatalf("Land Tax: 1 opponent land vs 1 of yours queued %d trigger(s), want 0", n)
	}

	// Two more Plains: the best opponent now controls 3 > your 1, the gate
	// holds and the trigger fires.
	crAbortMove(t, e, 1, "Plains", state.ZBattlefield)
	crAbortMove(t, e, 1, "Plains", state.ZBattlefield)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.pending = nil
	e.priorityRound()
	if n := crTriggerStackCount(e, tax); n != 1 {
		t.Fatalf("Land Tax: 3 opponent lands vs 1 of yours queued %d trigger(s), want 1", n)
	}

	// CR 603.5: the optional trigger went on the stack and its decider (you)
	// chooses whether to apply the effect as it resolves.
	crTriggerPassRound(t, e, "Land Tax")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional || d.Player != 0 {
		t.Fatalf("Land Tax: pending decision after the trigger resolved is %+v, want seat 0's optional ask", d)
	}
	if got := crAbortOption(t, e, "Land Tax", "yes", tax); got != 0 {
		t.Fatalf("Land Tax: optional ask's yes option is index %d, want 0", got)
	}
	before := handBasics(e, 0)
	crAbortAnswer(t, e, "Land Tax", 0)

	// The accepted effect is TrigChange's hidden-library search for up to
	// three basic lands: the chooser's private ask, then the lands in hand.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("Land Tax: pending decision after accepting is %+v, want seat 0's library search", d)
	}
	crAbortAnswer(t, e, "Land Tax", 0)
	// ShuffleNonMandatory$: the fetch's may-shuffle confirm (searchmay1) —
	// accept it, matching the unconditional shuffle this test was written
	// around.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search_mayshuffle" || d.Player != 0 {
		t.Fatalf("Land Tax: pending decision after the search pick is %+v, want the may-shuffle confirm", d)
	}
	crAbortAnswer(t, e, "Land Tax", 0) // yes — shuffle
	passUntilStackEmpty(t, e, 20)
	if n := handBasics(e, 0); n <= before {
		t.Fatalf("Land Tax: %d basic land(s) in hand after the search, want more than the %d before", n, before)
	}
	if n := crTriggerStackCount(e, tax); n != 0 {
		t.Fatalf("Land Tax: %d trigger(s) still on the stack after resolution, want 0", n)
	}
}

// angelTokens lists seat p's battlefield objects whose face is an Angel
// creature (the token TrigToken creates; token faces carry the name).
func angelTokens(e *Engine, p state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().IsCreature() && strings.Contains(o.Face().Name, "Angel") {
			out = append(out, id)
		}
	}
	return out
}

// handBasics counts seat p's hand cards that are basic lands.
func handBasics(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZHand, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().IsLand() && o.Face().IsBasic() {
			n++
		}
	}
	return n
}
