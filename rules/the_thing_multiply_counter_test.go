package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Thing pins `DB$ MultiplyCounter` end to end on the original report's
// corpus carrier (agent-20260918T232250Z-d339a2a3). The registration itself
// landed on main in 9ecaecec; Lily Bowen's pin (rules/multiply_counter_test.go)
// covers the named-kind `CounterType$ P1P1 | Defined$ Self` shape. The Thing
// exercises the two shapes Lily does not:
//
//   - an ABSENT CounterType$ on a multi-kind carrier: "double the number of
//     each kind of counter" multiplies EVERY kind the object positively holds
//     (P1P1 and DEFENSE here), not one named kind;
//   - the OPTIONAL ANY-NUMBER target ask (`ValidTgts$ Permanent.YouCtrl |
//     TargetMin$ 0 | TargetMax$ MaxTargets`, MaxTargets being a
//     `Count$Valid Permanent.YouCtrl`) reached through the paid
//     `AB$ ImmediateTrigger` ("you may pay {R}{G}{W}{U}. When you do, ...").
//
// The card is real corpus (searchCorpusCard), never inline Forge text: the
// scripts are GPL-3.0 and must not enter the Apache-2.0 tree.
//
// SBA note: seeding P1P1 and M1M1 on one creature would annihilate in pairs at
// the next SBA check (rules/sba.go annihilateOppositeCounters, CR 704.5q), so
// the two kinds are non-opposite (P1P1 and DEFENSE).

// thingEngine deals seat 0 a deck opening with The Thing, one of each basic
// (so the {R}{G}{W}{U} cost is payable) and a Grizzly Bears (the second
// controlled permanent to target), the rest Mountains. seatZeroStart makes
// seat 0 the starting player; the effective seed travels in the returned
// Config, which replayCheck must be handed.
func thingEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	thing := searchCorpusCard(t, reg, "The Thing")
	forest := searchCorpusCard(t, reg, "Forest")
	plains := searchCorpusCard(t, reg, "Plains")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{thing, forest, plains, island, mountain, bear}
	for len(deck) < 40 {
		deck = append(deck, mountain)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 4242, Names: []string{"thing", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// thingBoard is the fixture: The Thing and Grizzly Bears on seat 0's
// battlefield, The Thing carrying two non-opposite kinds (P1P1 x2, DEFENSE x1),
// the Bears carrying P1P1 x1. It asserts its own preconditions -- the objects
// are where the rule reads them and the seeded counts are exactly what the
// doubling assertions subtract from -- so a vacuous setup fails loudly.
func thingBoard(t *testing.T, e *Engine) (thingID, bearID state.ObjID) {
	t.Helper()
	thingID = searchMoveByName(t, e, "The Thing", state.ZBattlefield)
	bearID = searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	// The four basics the {R}{G}{W}{U} cost is paid from, one per colour.
	for _, name := range []string{"Forest", "Plains", "Island", "Mountain"} {
		searchMoveByName(t, e, name, state.ZBattlefield)
	}
	for _, id := range []state.ObjID{thingID, bearID} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("fixture object %d not on seat 0's battlefield: %+v", id, o)
		}
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: thingID, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: thingID, Counter: "DEFENSE", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.pending = nil
	e.priorityRound()
	if got := e.G.Obj(thingID).Counter("P1P1"); got != 2 {
		t.Fatalf("The Thing P1P1 precondition = %d, want 2", got)
	}
	if got := e.G.Obj(thingID).Counter("DEFENSE"); got != 1 {
		t.Fatalf("The Thing DEFENSE precondition = %d, want 1", got)
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 1 {
		t.Fatalf("Grizzly Bears P1P1 precondition = %d, want 1", got)
	}
	return thingID, bearID
}

// thingAttackToTargetAsk drives the whole paid flow up to the optional target
// ask and returns it: declare The Thing as the only attacker, tap one land per
// colour symbol of `Cost$ R G W U` in the mana-activation window, answer PAY,
// and stop at the KChoose target ask.
//
// The mana-activation window re-renders after each activation (the tapped land
// disappears from the options), so each round re-reads e.Pending(); the colour
// is chosen from the untapped land's name so a wrong colour can never fund the
// cost.
func thingAttackToTargetAsk(t *testing.T, e *Engine, thingID state.ObjID) *decision.Decision {
	t.Helper()
	driveToStep(t, e, 3, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, thingID)

	// The activation / pay window: tap one land per colour symbol of the
	// {R}{G}{W}{U} cost until the engine offers the paid branch, then take it.
	// The prompt names the FULL cost (it does not decrement), so the symbols
	// already produced are tracked locally; the tapped land disappears from
	// the options, so each round re-reads e.Pending().
	needed := []string{"W", "U", "R", "G"}
	for i := 0; i < 20; i++ {
		d := passUntilNonPriority(t, e, 40)
		if d.Kind != decision.KChoose {
			t.Fatalf("expected the KChoose cost window, got %+v", d)
		}
		paid := false
		for _, o := range d.Options {
			if o.Kind == "trigger_cost_pay" {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
					t.Fatalf("submit pay: %v", err)
				}
				paid = true
				break
			}
		}
		if paid {
			break
		}
		// Not payable yet: tap the first still-needed colour's land (labels
		// are "Tap <Basic> for mana").
		next := -1
		for _, o := range d.Options {
			if o.Kind != "activate" {
				continue
			}
			for ni, sym := range needed {
				if strings.Contains(o.Label, "Tap "+thingLandName(sym)) {
					next = ni
					break
				}
			}
			if next >= 0 {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
					t.Fatalf("submit activate: %v", err)
				}
				needed = append(needed[:next], needed[next+1:]...)
				break
			}
		}
		if next < 0 {
			t.Fatalf("cost window offers no untapped land for any of %v (pool cannot reach {R}{G}{W}{U}): %+v", needed, d)
		}
	}
	if len(needed) > 0 {
		t.Fatalf("the cost window never offered the paid branch; still needed %v", needed)
	}
	return passUntilNonPriority(t, e, 40)
}

// thingLandName maps a mana symbol to the basic land whose activation produces
// it (the only producers in this fixture).
func thingLandName(sym string) string {
	switch sym {
	case "W":
		return "Plains"
	case "U":
		return "Island"
	case "B":
		return "Swamp"
	case "R":
		return "Mountain"
	case "G":
		return "Forest"
	}
	return ""
}

// thingCounterChangeCount returns how many CounterChange events match obj, counter
// and amount. Used to prove the per-kind change reached the log, not just the
// final object state.
func thingCounterChangeCount(e *Engine, obj state.ObjID, counter string, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == obj && ev.Counter == counter && ev.Amount == amount {
			n++
		}
	}
	return n
}

// TestTheThingMultiplyCounterDoublesEachKind is the report's own card: pay
// {R}{G}{W}{U}, target The Thing and a second controlled permanent, and both
// seed kinds double on both objects.
func TestTheThingMultiplyCounterDoublesEachKind(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := thingEngine(t, reg)
	thingID, bearID := thingBoard(t, e)

	// Seed events, so the assertions below measure the DOUBLING delta even
	// where the seed and the add share an amount (Thing P1P1: seed +2, add +2).
	seedThingP1P1 := thingCounterChangeCount(e, thingID, "P1P1", 2)
	seedThingDefense := thingCounterChangeCount(e, thingID, "DEFENSE", 1)
	seedBearP1P1 := thingCounterChangeCount(e, bearID, "P1P1", 1)

	d := thingAttackToTargetAsk(t, e, thingID)

	// Structural pin of the optional-any-number shape: TargetMin$ 0 and
	// TargetMax$ MaxTargets, where MaxTargets = Count$Valid Permanent.YouCtrl
	// (every permanent seat 0 controls: The Thing, the Bears and the basics).
	controls := len(e.G.Zone(state.ZBattlefield, 0))
	// CR 603.12: the paid "when you do" half is a reflexive triggered ability,
	// so its targets are a placement target ask as it goes on the stack.
	if d.Kind != decision.KTarget {
		t.Fatalf("target ask kind = %s, want target (the reflexive ability's placement ask): %+v", d.Kind, d)
	}
	if d.Min != 0 {
		t.Fatalf("target ask Min = %d, want 0 (TargetMin$ 0)", d.Min)
	}
	if d.Max != controls {
		t.Fatalf("target ask Max = %d, want %d (TargetMax$ MaxTargets = Count$Valid Permanent.YouCtrl)", d.Max, controls)
	}
	var thingOpt, bearOpt = -1, -1
	for _, o := range d.Options {
		switch o.Obj {
		case thingID:
			thingOpt = o.Index
		case bearID:
			bearOpt = o.Index
		}
	}
	if thingOpt < 0 || bearOpt < 0 {
		t.Fatalf("target ask did not offer both fixture permanents (thing=%d bear=%d): %+v", thingOpt, bearOpt, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{thingOpt, bearOpt}}); err != nil {
		t.Fatalf("submit targets: %v", err)
	}
	passUntilStackEmpty(t, e, 40)

	// Final counts.
	if got := e.G.Obj(thingID).Counter("P1P1"); got != 4 {
		t.Fatalf("The Thing P1P1 = %d after doubling, want 4 (2 doubled)", got)
	}
	if got := e.G.Obj(thingID).Counter("DEFENSE"); got != 2 {
		t.Fatalf("The Thing DEFENSE = %d after doubling, want 2 (1 doubled; absent CounterType$ doubles EACH kind)", got)
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 2 {
		t.Fatalf("Grizzly Bears P1P1 = %d after doubling, want 2 (1 doubled; the second target)", got)
	}

	// The log records the per-kind `(Multiplier-1) x current` add, not just
	// the final count: exactly ONE more matching CounterChange than the seed.
	if got := thingCounterChangeCount(e, thingID, "P1P1", 2) - seedThingP1P1; got != 1 {
		t.Fatalf("The Thing logged %d new CounterChange(P1P1, +2), want 1 (2 doubled)", got)
	}
	if got := thingCounterChangeCount(e, thingID, "DEFENSE", 1) - seedThingDefense; got != 1 {
		t.Fatalf("The Thing logged %d new CounterChange(DEFENSE, +1), want 1 (1 doubled)", got)
	}
	if got := thingCounterChangeCount(e, bearID, "P1P1", 1) - seedBearP1P1; got != 1 {
		t.Fatalf("Grizzly Bears logged %d new CounterChange(P1P1, +1), want 1 (1 doubled)", got)
	}

	replayCheck(t, e, cfg)
}

// TestTheThingMultiplyCounterZeroTargets pins the optionality: TargetMin$ 0
// makes the empty answer legal, and it doubles nothing.
func TestTheThingMultiplyCounterZeroTargets(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := thingEngine(t, reg)
	thingID, bearID := thingBoard(t, e)

	d := thingAttackToTargetAsk(t, e, thingID)
	if d.Kind != decision.KTarget || d.Min != 0 {
		t.Fatalf("target ask = %+v, want the reflexive ability's target ask, Min 0", d)
	}
	// The "nothing happens" test must prove the feature's handler actually
	// ran: reaching this ask at all means the paid ImmediateTrigger chain
	// resolved to MultiplyCounter, and the unregistered fallback would have
	// emitted its loud Note instead.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "MultiplyCounter") {
			t.Fatalf("unimplemented-API Note on the event stream: %q", ev.Text)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
		t.Fatalf("submit empty target selection: %v", err)
	}
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(thingID).Counter("P1P1"); got != 2 {
		t.Fatalf("The Thing P1P1 = %d, want 2 unchanged (zero targets)", got)
	}
	if got := e.G.Obj(thingID).Counter("DEFENSE"); got != 1 {
		t.Fatalf("The Thing DEFENSE = %d, want 1 unchanged (zero targets)", got)
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 1 {
		t.Fatalf("Grizzly Bears P1P1 = %d, want 1 unchanged (zero targets)", got)
	}

	replayCheck(t, e, cfg)
}
