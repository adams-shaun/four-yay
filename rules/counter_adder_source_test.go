package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The source-scoped half of the counter-placement replacement class
// (task addcounter2): ValidSource$ names WHO is putting the counters, a role
// the CounterChange event does not carry, so the matcher reads it from the
// engine's in-flight adder scratch (counterAdder, published at cost/turn-based
// sites) or from the resolving ability's controller. Vorinclex, Monstrous
// Raider is the filing card: its two lines ("if YOU would put ..." doubles,
// "if an OPPONENT would put ..." halves) are mutually exclusive per placement
// because one placement has exactly one adder, so they never compete.
//
// The counter source is authored (never a corpus .txt, per the licensing
// rule) and rides the engine's own effPutCounter emit path; Vorinclex itself
// is the real corpus card.

// vorinclexBoard places Vorinclex on the given seat and returns the engine,
// cfg and Vorinclex's id. It is boardWithCounterReplacement's source-scoped
// cousin: the counter source is placed on sourceSeat and the counter target
// on targetSeat, so the own/opponent directions can be driven independently.
func vorinclexBoard(t *testing.T, seed uint64, sourceSeat, targetSeat state.PlayerID, counters int) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	vori := tokenReplCorpusCard(t, "Vorinclex, Monstrous Raider")
	src := counterReplSource(t, counters)
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	seat0 := []*cards.Card{vori}
	seat1 := []*cards.Card{}
	if sourceSeat == 0 {
		seat0 = append(seat0, src)
	} else {
		seat1 = append(seat1, src)
	}
	if targetSeat == 0 {
		seat0 = append(seat0, target)
	} else {
		seat1 = append(seat1, target)
	}
	e, cfg := tokenReplGameSeats(t, seed, seat0, seat1)
	moveSeededCard(t, e, 0, vori, state.ZBattlefield)
	sourceID := moveSeededCard(t, e, sourceSeat, src, state.ZBattlefield)
	targetID := moveSeededCard(t, e, targetSeat, target, state.ZBattlefield)
	return e, cfg, sourceID, targetID
}

// activateCounterSourceAs drives a priority round and submits the counter
// source's ability from the given seat, targeting `target`. It is
// activateCounterSource generalized to the opponent direction: the seat must
// receive priority (APNAP hand-off from the active player) before its own
// ability option is offered.
func activateCounterSourceAs(t *testing.T, e *Engine, seat state.PlayerID, source, target state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "")
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while waiting for seat %d's priority", seat)
		}
		if d.Player == seat {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while waiting for seat %d", d, seat)
		}
		passPriorityOnce(t, e)
	}
	submitChoices(t, e, abilityOption(t, e, source, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision for the counter source: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("target %d not offerable to seat %d: %+v", target, seat, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// TestVorinclexValidSourceDoublesOwnAndHalvesOpponents is the filing card end
// to end, both directions. A 3-counter placement by Vorinclex's controller
// doubles to 6 (ValidSource$ You); the same placement by an opponent halves,
// rounded down, to 1 (ValidSource$ Opponent, HalfDown). The two lines are
// mutually exclusive on one placement -- there is one adder -- so the counts
// are exact, not a union.
func TestVorinclexValidSourceDoublesOwnAndHalvesOpponents(t *testing.T) {
	t.Parallel()
	// Own placement: seat 0 owns everything.
	e, cfg, source, target := vorinclexBoard(t, 211, 0, 0, 3)
	activateCounterSource(t, e, source, target)
	if got := e.G.Obj(target).Counter("P1P1"); got != 6 {
		t.Fatalf("Vorinclex own 3-counter placement = %d, want 6 (ValidSource$ You doubles)", got)
	}
	replayCheck(t, e, cfg)

	// Opponent placement: seat 1 owns the counter source and the target.
	e2, cfg2, src2, tgt2 := vorinclexBoard(t, 213, 1, 1, 3)
	activateCounterSourceAs(t, e2, 1, src2, tgt2)
	if got := e2.G.Obj(tgt2).Counter("P1P1"); got != 1 {
		t.Fatalf("Vorinclex opponent 3-counter placement = %d, want 1 (ValidSource$ Opponent halves down)", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestCounterAdderValidSourceYouFiresOwnNotOpponent isolates the scope in one
// game: Vorinclex sits on seat 0 and only ONE placement happens at a time, so
// the own direction doubles and the opponent direction does not. The negative
// half is the load-bearing one -- before addcounter2 both lines failed closed,
// so an opponent's placement was untouched (never halved); the positive half
// proves the own line now fires.
func TestCounterAdderValidSourceYouFiresOwnNotOpponent(t *testing.T) {
	t.Parallel()
	// Own half: seat 0 places, so "You" fires and "Opponent" must not.
	e, cfg, source, target := vorinclexBoard(t, 215, 0, 0, 1)
	activateCounterSource(t, e, source, target)
	if got := e.G.Obj(target).Counter("P1P1"); got != 2 {
		t.Fatalf("own 1-counter placement = %d, want 2 (You doubles; the Opponent half must never fire)", got)
	}
	replayCheck(t, e, cfg)

	// Opponent half: seat 1 places, so "Opponent" fires and "You" must not.
	e2, cfg2, src2, tgt2 := vorinclexBoard(t, 217, 1, 0, 2)
	activateCounterSourceAs(t, e2, 1, src2, tgt2)
	if got := e2.G.Obj(tgt2).Counter("P1P1"); got != 1 {
		t.Fatalf("opponent 2-counter placement = %d, want 1 (Opponent halves; the You half must never fire)", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestCounterAdderFailClosedWithoutPublishedAdder pins the conservative
// default the brief requires: a placement with NO published adder and NO stack
// cause (the SBA / turn-based shape) must never match a ValidSource$ line.
// Vorinclex's two lines both carry ValidSource$, so a bare emit on an empty
// stack leaves the placement verbatim (3 stays 3) rather than guessing an
// adder -- even though Vorinclex's controller is the obvious candidate.
func TestCounterAdderFailClosedWithoutPublishedAdder(t *testing.T) {
	t.Parallel()
	e, cfg, _, target := vorinclexBoard(t, 219, 0, 0, 1)
	// Force an empty stack so actionCause() is 0 and no adder is published.
	e.G.Stack = nil
	e.emit(events.Event{Kind: events.CounterChange, Obj: target, Counter: "P1P1", Amount: 3})
	if got := e.G.Obj(target).Counter("P1P1"); got != 3 {
		t.Fatalf("source-scoped placement with no adder = %d, want 3 (fail closed, never matched)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCounterAdderPublishesAtCostSite pins the explicit publication the cost
// sites need. A planeswalker's [+1] loyalty cost is paid before the ability
// wrapper reaches the stack, so actionCause() cannot attribute the loyalty
// counter; without the payCast publish, Vorinclex's ValidSource$ You line
// would fail closed and the walker would gain only one loyalty. With it, the
// one loyalty counter the cost adds doubles, and the walker enters at 3 and
// ends at 5 (3 + 2).
func TestCounterAdderPublishesAtCostSite(t *testing.T) {
	t.Parallel()
	walkerSrc := "Name:Test Walker\nTypes:Planeswalker Test\nLoyalty:3\n" +
		"A:AB$ GainLife | Cost$ AddCounter<1/LOYALTY> | LifeAmount$ 2 | Planeswalker$ True | SpellDescription$ You gain 2 life.\n" +
		"Oracle:x\n"

	// Control: without Vorinclex the walker gains exactly one loyalty (3 -> 4).
	walker := card(t, walkerSrc)
	e, cfg := tokenReplGame(t, 221, walker)
	walkerID := moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, walkerID, 0).Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(walkerID).Counter("LOYALTY"); got != 4 {
		t.Fatalf("control walker [+1] loyalty = %d, want 4", got)
	}
	replayCheck(t, e, cfg)

	// Under Vorinclex, the cost's one loyalty counter is doubled (3 -> 5).
	vori := tokenReplCorpusCard(t, "Vorinclex, Monstrous Raider")
	walker2 := card(t, walkerSrc)
	e2, cfg2 := tokenReplGame(t, 223, vori, walker2)
	moveSeededCard(t, e2, 0, vori, state.ZBattlefield)
	walker2ID := moveSeededCard(t, e2, 0, walker2, state.ZBattlefield)
	addMana(t, e2, 0, "")
	submitChoices(t, e2, abilityOption(t, e2, walker2ID, 0).Index)
	passUntilStackEmpty(t, e2, 20)
	if got := e2.G.Obj(walker2ID).Counter("LOYALTY"); got != 5 {
		t.Fatalf("Vorinclex walker [+1] loyalty = %d, want 5 (the cost's one counter doubles)", got)
	}
	replayCheck(t, e2, cfg2)
}

// zabazReplFixtureSrc authors Zabaz, the Glimmerwasp's replacement card. The
// real corpus card cannot be placed on the battlefield in this build: Zabaz is
// a 0/0 whose entry +1/+1 counter comes from K:Modular, and kw:Modular is a
// separate, still-missing expansion, so the real card dies to the toughness
// SBA before any counter is placed and its ActiveZones$ Battlefield gate then
// fails closed -- the brief's "the real card stays unplayable on the separate
// kw:Modular gap". This fixture carries Zabaz's R: line and both SVars
// verbatim, on a survivable 2/2, so the ValidCause$ Triggered.Modular arm is
// exercised on the real compiled shape without the unrelated blocker.
func zabazReplFixtureSrc() string {
	return "Name:Zabaz Fixture\nTypes:Legendary Artifact Creature Insect\nPT:2/2\n" +
		"R:Event$ AddCounter | ActiveZones$ Battlefield | ValidCause$ Triggered.Modular | ValidCard$ Creature.YouCtrl+inZoneBattlefield | ValidCounterType$ P1P1 | ReplaceWith$ AddOneMoreCounter | Description$ If a modular triggered ability would put one or more +1/+1 counters on a creature you control, that many plus one +1/+1 counters are put on it instead.\n" +
		"SVar:AddOneMoreCounter:DB$ ReplaceCounter | ValidCounterType$ P1P1 | ChooseCounter$ True | Amount$ X\n" +
		"SVar:X:ReplaceCount$CounterNum/Plus.1\n" +
		"Oracle:x\n"
}

// modularFixtureSrc is the authored Zabaz probe: a creature carrying K:Modular
// whose ETB trigger is a DB$ PutCounter, so the trigger's wrapper is on the
// stack when the counters are placed and actionCause() names a real triggered
// ability whose source carries the Modular keyword. The real Zabaz stays
// unplayable on the separate kw:Modular expansion gap, so this fixture pins
// the ValidCause$ Triggered.Modular arm itself.
func modularFixtureSrc(withKeyword bool) string {
	kw := ""
	if withKeyword {
		kw = "K:Modular:1\n"
	}
	return "Name:Modular Fixture\nTypes:Creature\nPT:1/1\n" + kw +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigPut | TriggerDescription$ x\n" +
		"SVar:TrigPut:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1\n" +
		"Oracle:x\n"
}

// TestCounterModularCauseTriggered is Zabaz's arm: a modular triggered ability
// putting +1/+1 counters on a creature you control is doubled by
// AddOneMoreCounter. The fixture's own ETB trigger carries K:Modular, so the
// wrapper's source satisfies ValidCause$ Triggered.Modular; the 1-counter put
// becomes 2.
func TestCounterModularCauseTriggered(t *testing.T) {
	t.Parallel()
	zabaz := card(t, zabazReplFixtureSrc())
	fixture := card(t, modularFixtureSrc(true))
	e, cfg := tokenReplGame(t, 225, zabaz, fixture)
	moveSeededCard(t, e, 0, zabaz, state.ZBattlefield)
	id := moveSeededCard(t, e, 0, fixture, state.ZBattlefield)
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("the modular fixture's ETB trigger did not fire")
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("Zabaz on a modular trigger's 1-counter put = %d, want 2 (Triggered.Modular matches)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCounterModularCauseTriggeredFailsClosed is the negative half: the SAME
// trigger shape on a creature that does NOT print K:Modular must not satisfy
// ValidCause$ Triggered.Modular, so the 1-counter put stays 1. This pins that
// the qualifier reads the keyword, not merely "some triggered ability".
func TestCounterModularCauseTriggeredFailsClosed(t *testing.T) {
	t.Parallel()
	zabaz := card(t, zabazReplFixtureSrc())
	fixture := card(t, modularFixtureSrc(false))
	e, cfg := tokenReplGame(t, 227, zabaz, fixture)
	moveSeededCard(t, e, 0, zabaz, state.ZBattlefield)
	id := moveSeededCard(t, e, 0, fixture, state.ZBattlefield)
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("the fixture's ETB trigger did not fire")
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("Zabaz on a non-modular trigger's 1-counter put = %d, want 1 (Triggered.Modular fails closed)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCounterModularCauseAbsentFailsClosed pins the other fail-closed edge: a
// ValidCause$ line with NO cause at all (an empty stack, no published adder)
// never matches. Zabaz's line is emitted over a bare placement with the stack
// cleared, so the 1-counter put stays 1.
func TestCounterModularCauseAbsentFailsClosed(t *testing.T) {
	t.Parallel()
	zabaz := card(t, zabazReplFixtureSrc())
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 229, zabaz, target)
	moveSeededCard(t, e, 0, zabaz, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	e.G.Stack = nil
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 1})
	if got := e.G.Obj(targetID).Counter("P1P1"); got != 1 {
		t.Fatalf("ValidCause$ with no cause = %d, want 1 (fail closed)", got)
	}
	replayCheck(t, e, cfg)
}

// TestStatusMarkerGuardCoversShieldAndDeathtouched re-pins the status-marker
// guard the source-scoped work must not weaken: the engine's two internal
// markers ("Shield" from regeneration, "Deathtouched" from combat) ride a
// positive CounterChange and a kind-blind replacement (Doubling Season,
// Winding Constrictor's object line) names no ValidCounterType$, so without
// the guard either one would be doubled. Both markers stay at 1.
func TestStatusMarkerGuardCoversShieldAndDeathtouched(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		seed   uint64
		marker string
	}{
		{"Doubling Season", 231, "Shield"},
		{"Doubling Season", 233, "Deathtouched"},
		{"Winding Constrictor", 235, "Shield"},
		{"Winding Constrictor", 237, "Deathtouched"},
	} {
		repl := tokenReplCorpusCard(t, tc.name)
		target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e, cfg := tokenReplGame(t, tc.seed, repl, target)
		moveSeededCard(t, e, 0, repl, state.ZBattlefield)
		targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
		e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: tc.marker, Amount: 1})
		if got := e.G.Obj(targetID).Counter(tc.marker); got != 1 {
			t.Fatalf("%s + one %s marker = %d, want 1 (a status marker is never a counter placement)", tc.name, tc.marker, got)
		}
		replayCheck(t, e, cfg)
	}
}
