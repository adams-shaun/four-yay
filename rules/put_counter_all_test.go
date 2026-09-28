package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The PutCounterAll primitive (the "put a counter on each <thing>" sweep,
// 298 raw corpus lines over 291 files) did not exist before this file's
// fix: effects/counters.go registered only PutCounter/RemoveCounterAll/
// Regenerate, so every such line fell into the unimplemented-API fallback
// Note and placed nothing. These tests pin the implemented shapes on real
// corpus cards and keep the exotic shapes loud.

// putCounterTable seats the prepared seat-0/seat-1 decks (protagonist and
// extras already appended by the caller) with seat 0 as the starting player
// and drives to seat 0's first priority ask. Card sources must be distinct
// by name for the finders below to address them.
func putCounterTable(t *testing.T, seed uint64, seat0, seat1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	cfg := seatZeroStart(Config{Seed: seed,
		Names:  []string{"a", "b"},
		Decks:  [][]*cards.Card{append(mountainDeck(t, 40), seat0...), append(mountainDeck(t, 40), seat1...)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// findAndMoveToBattlefield moves the named card from its holder's hand or
// library onto the battlefield, through the event the replay folds, and
// returns its object id.
func findAndMoveToBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				return id
			}
		}
	}
	t.Fatalf("%s not in seat %d's hand or library", name, p)
	return 0
}

// counterBear returns the card source for a distinctly named 2/2 Bear.
func counterBear(name string) string {
	return "Name:" + name + "\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
}

// counterChanges returns the log's CounterChange events for obj.
func counterChanges(e *Engine, obj state.ObjID) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == obj {
			out = append(out, ev)
		}
	}
	return out
}

// TestAronBenaliasRuinPutsCountersOnEachCreatureYouControl drives Aron's
// real AB$ PutCounterAll activation end to end: {W}{B}, {T} and the
// sacrifice of another creature pay, and EVERY creature the controller still
// controls gets exactly one +1/+1 counter (Aron included -- "each creature
// you control"); the sacrificed creature left the battlefield before
// resolution and takes nothing, and seat 1's creatures take nothing. Before
// the fix the resolution emitted "unimplemented API PutCounterAll" and moved
// nothing.
func TestAronBenaliasRuinPutsCountersOnEachCreatureYouControl(t *testing.T) {
	t.Parallel()
	aronSrc := corpusCard(t, "Aron, Benalia's Ruin")
	e, cfg := putCounterTable(t, 196,
		[]*cards.Card{aronSrc, card(t, counterBear("Victim Bear")), card(t, counterBear("Kept Bear"))},
		[]*cards.Card{card(t, counterBear("Enemy Bear"))})
	aron := findAndMoveToBattlefield(t, e, 0, "Aron, Benalia's Ruin")
	victim := findAndMoveToBattlefield(t, e, 0, "Victim Bear")
	kept := findAndMoveToBattlefield(t, e, 0, "Kept Bear")
	enemy := findAndMoveToBattlefield(t, e, 1, "Enemy Bear")
	// The {T} cost needs a non-summoning-sick Aron (CR 302.6); the TurnChange
	// is the event the replay folds to clear it.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	e.priorityRound()

	opt := abilityOption(t, e, aron, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("no sacrifice cost ask after activation: %+v", d)
	}
	sacIdx := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			sacIdx = o.Index
		}
	}
	if sacIdx < 0 {
		t.Fatalf("sacrifice ask does not offer Victim Bear: %+v", d.Options)
	}
	submitChoices(t, e, sacIdx)
	passUntilStackEmpty(t, e, 30)

	if z := e.G.Obj(victim).Zone; z != state.ZGraveyard {
		t.Fatalf("sacrificed Victim Bear zone = %s, want graveyard", z)
	}
	for _, id := range []state.ObjID{aron, kept} {
		if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
			t.Fatalf("creature %d has %d P1P1 counters, want 1", id, got)
		}
	}
	if got := e.G.Obj(enemy).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 1's Enemy Bear took %d counters, want 0", got)
	}
	if n := len(counterChanges(e, victim)); n != 0 {
		t.Fatalf("the sacrificed creature received %d CounterChange events, want 0", n)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool not drained: %d", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterAllSweepFollowsBattlefieldZoneOrder: the sweep walks seats
// in g.AliveFrom(0) order and each seat's battlefield in zone order (the
// effRemoveCounterAll shape), so the per-object CounterChange sequence must
// be exactly the zone order -- never a map range.
func TestPutCounterAllSweepFollowsBattlefieldZoneOrder(t *testing.T) {
	t.Parallel()
	aronSrc := corpusCard(t, "Aron, Benalia's Ruin")
	e, cfg := putCounterTable(t, 197,
		[]*cards.Card{aronSrc, card(t, counterBear("First Bear")), card(t, counterBear("Second Bear")), card(t, counterBear("Third Bear"))},
		[]*cards.Card{card(t, counterBear("Enemy Bear"))})
	aron := findAndMoveToBattlefield(t, e, 0, "Aron, Benalia's Ruin")
	first := findAndMoveToBattlefield(t, e, 0, "First Bear")
	second := findAndMoveToBattlefield(t, e, 0, "Second Bear")
	third := findAndMoveToBattlefield(t, e, 0, "Third Bear")
	enemy := findAndMoveToBattlefield(t, e, 1, "Enemy Bear")
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	e.priorityRound()
	opt := abilityOption(t, e, aron, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("no sacrifice cost ask after activation: %+v", d)
	}
	// Sacrifice the LAST bear in zone order: the survivors' CounterChange
	// sequence is then the surviving zone order, not the full zone order.
	sacIdx := -1
	for _, o := range d.Options {
		if o.Obj == third {
			sacIdx = o.Index
		}
	}
	if sacIdx < 0 {
		t.Fatalf("sacrifice ask does not offer Third Bear: %+v", d.Options)
	}
	submitChoices(t, e, sacIdx)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(third).Zone; z != state.ZGraveyard {
		t.Fatalf("sacrificed Third Bear zone = %s, want graveyard", z)
	}

	want := []state.ObjID{aron, first, second}
	var got []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Counter == "P1P1" && ev.Amount == 1 {
			got = append(got, ev.Obj)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("sweep emitted %d CounterChange events, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sweep order: got %v, want zone order %v", got, want)
		}
	}
	if got := e.G.Obj(enemy).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 1's Enemy Bear took %d counters, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterAllPlayerTargetedSweep pins the ValidTgts$ Player shape on
// its real corpus carrier Meadowboon ("When CARDNAME leaves the battlefield,
// put a +1/+1 counter on each creature target player controls"): the chosen
// target player's creatures get the batch and nobody else's do.
func TestPutCounterAllPlayerTargetedSweep(t *testing.T) {
	t.Parallel()
	meadow := corpusCard(t, "Meadowboon")
	e, cfg := putCounterTable(t, 198,
		[]*cards.Card{meadow, card(t, counterBear("Meadow Bear")), card(t, counterBear("Meadow Second"))},
		[]*cards.Card{card(t, counterBear("Enemy Bear"))})
	meadowID := findAndMoveToBattlefield(t, e, 0, "Meadowboon")
	first := findAndMoveToBattlefield(t, e, 0, "Meadow Bear")
	second := findAndMoveToBattlefield(t, e, 0, "Meadow Second")
	enemy := findAndMoveToBattlefield(t, e, 1, "Enemy Bear")
	e.emit(events.Event{Kind: events.MoveZone, Obj: meadowID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask for Meadowboon's leave trigger: %+v", d)
	}
	submitChoices(t, e, indexOfPlayerOption(d, 0))
	passUntilStackEmpty(t, e, 30)

	for _, id := range []state.ObjID{first, second} {
		if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
			t.Fatalf("target player's creature %d has %d P1P1 counters, want 1", id, got)
		}
	}
	if got := e.G.Obj(enemy).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 1's Enemy Bear took %d counters, want 0", got)
	}
	if n := len(counterChanges(e, meadowID)); n != 0 {
		t.Fatalf("the departed Meadowboon received %d CounterChange events, want 0", n)
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterAllSecondBatchPlacesBothKinds pins the ValidCards2$/
// CounterType2$ shape on its real corpus carrier Brokers Ascendancy ("put a
// +1/+1 counter on each creature you control and a loyalty counter on each
// planeswalker you control"): one resolution, two sweeps, two counter kinds.
func TestPutCounterAllSecondBatchPlacesBothKinds(t *testing.T) {
	t.Parallel()
	brokers := corpusCard(t, "Brokers Ascendancy")
	gideon := corpusCard(t, "Gideon, Ally of Zendikar")
	e, cfg := putCounterTable(t, 199,
		[]*cards.Card{brokers, gideon, card(t, counterBear("Broker Bear"))}, nil)
	findAndMoveToBattlefield(t, e, 0, "Brokers Ascendancy")
	gideonID := findAndMoveToBattlefield(t, e, 0, "Gideon, Ally of Zendikar")
	bear := findAndMoveToBattlefield(t, e, 0, "Broker Bear")
	driveToStepAll(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("Broker Bear has %d P1P1 counters, want 1", got)
	}
	if got := e.G.Obj(gideonID).Counter("LOYALTY"); got != 5 {
		t.Fatalf("Gideon has %d LOYALTY counters, want 5 (entered at 4, sweep added 1)", got)
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterAllExoticShapesStayLoud: registering the API removes the
// generic unimplemented-API fallback, so the shapes the core sweep cannot
// express -- Placer$ (who places), and a ValidZone$ naming a zone other than
// the battlefield (the two suspended-TIME carriers) -- must emit an explicit
// unimplemented-shape Note and place NOTHING, never go silent.
func TestPutCounterAllExoticShapesStayLoud(t *testing.T) {
	t.Parallel()
	placerSrc := "Name:Placer\nManaCost:2 U\nTypes:Creature Wizard\nPT:2/2\n" +
		"A:AB$ PutCounterAll | Cost$ 1 | Placer$ Controller | ValidCards$ Creature | CounterType$ P1P1 | CounterNum$ 1 | SpellDescription$ x\nOracle:x\n"
	zoneSrc := "Name:Timekeeper\nManaCost:2 U\nTypes:Creature Wizard\nPT:2/2\n" +
		"A:AB$ PutCounterAll | Cost$ 1 | ValidCards$ Card | CounterType$ TIME | CounterNum$ 2 | ValidZone$ Exile | SpellDescription$ x\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 220, placerSrc, zoneSrc, counterBear("Loud Bear"))
	placer := moveSeeded(t, e, 0, placerSrc, state.ZBattlefield)
	bear := putCreature(t, e, 0, counterBear("Loud Bear"))
	timekeeper := moveSeeded(t, e, 0, zoneSrc, state.ZBattlefield)

	// Placer$ shape: offered, payable, then loud and inert.
	addMana(t, e, 0, "UU")
	e.Advance()
	opt := abilityOption(t, e, placer, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !hasNote(e, "unimplemented PutCounterAll shape") {
		t.Fatal("no unimplemented-shape note for the Placer$ sweep")
	}

	// ValidZone$ Exile shape: same contract.
	addMana(t, e, 0, "UU")
	e.Advance()
	opt = abilityOption(t, e, timekeeper, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !hasNote(e, "unimplemented PutCounterAll shape") {
		t.Fatal("no unimplemented-shape note for the ValidZone$ Exile sweep")
	}

	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange {
			t.Fatalf("an exotic sweep placed counters anyway: %+v", ev)
		}
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("Loud Bear took %d counters from a declined shape, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterAllZeroCountEmitsNothing pins the sibling discipline: an
// unresolvable CounterNum$ degrades to 0 through Num (present but
// unresolvable degrades to zero, not to the default), and a zero-amount
// batch emits no per-object CounterChange -- one Amount-0 event per match is
// log noise; effRemoveCounterAll skips amt <= 0 for the same reason
// (messenger_jays' CounterNum$ VoteNum with no SVar on the face was the live
// carrier).
func TestPutCounterAllZeroCountEmitsNothing(t *testing.T) {
	t.Parallel()
	zeroer := "Name:Zeroer\nManaCost:2 U\nTypes:Creature Wizard\nPT:2/2\n" +
		"A:AB$ PutCounterAll | Cost$ 1 | ValidCards$ Creature | CounterType$ P1P1 | CounterNum$ NoSuchSVar | SpellDescription$ x\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 221, zeroer, counterBear("Zero Bear"))
	zeroerID := moveSeeded(t, e, 0, zeroer, state.ZBattlefield)
	bear := putCreature(t, e, 0, counterBear("Zero Bear"))
	addMana(t, e, 0, "UU")
	e.Advance()
	opt := abilityOption(t, e, zeroerID, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange {
			t.Fatalf("a zero-amount batch emitted a CounterChange anyway: %+v", ev)
		}
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("Zero Bear has %d P1P1 counters, want 0", got)
	}
	replayCheck(t, e, cfg)
}

func TestPutCounterAllValidTgtsOpponentStaysLoud(t *testing.T) {
	t.Parallel()
	corrosion := corpusCard(t, "Corrosion")
	e, cfg := putCounterTable(t, 201,
		[]*cards.Card{corrosion, card(t, counterBear("Rust Bear"))},
		[]*cards.Card{card(t, counterBear("Foe Bear"))})
	findAndMoveToBattlefield(t, e, 0, "Corrosion")
	bear := findAndMoveToBattlefield(t, e, 0, "Rust Bear")
	enemy := findAndMoveToBattlefield(t, e, 1, "Foe Bear")

	// Drive the REAL upkeep path (Corrosion's Cumulative upkeep fires beside
	// Corrode, so the whole trigger flow is exercised): at seat 0's upkeep on
	// turn 2, order the two triggers so the cumulative one is put on the
	// stack first (resolves LAST) and Corrode resolves first; answer
	// Corrode's player target with seat 1; then pass everything.
	//
	// Before the exotic gate the Opponent value fell through to the
	// whole-table branch and rusted the resolving controller's OWN artifacts
	// too -- wrong-wide and silent. Now: the body emits the
	// unimplemented-shape Note and places nothing (seat 1 was targeted, so
	// even a correct sweep would not have touched seat 0's artifacts, but the
	// shape itself is unimplemented and stays loud).
	asked := false
	for i := 0; i < 8000; i++ {
		if asked && len(e.G.Stack) == 0 && e.G.Step != state.StepUpkeep {
			break
		}
		if e.G.Over {
			t.Fatalf("game ended early at turn %d step %s", e.G.Turn, e.G.Step)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KTriggerOrder:
			// Options: [0] Corrode, [1] Cumulative upkeep. The choice order
			// is the stack order -- chosen first resolves last -- so pick
			// the cumulative trigger first to make Corrode resolve first.
			submitChoices(t, e, 1, 0)
		case decision.KTarget:
			if asked {
				t.Fatalf("second target ask during the upkeep: %+v", d)
			}
			asked = true
			submitChoices(t, e, indexOfPlayerOption(d, 1))
		case decision.KChoose:
			// Corrosion's own cumulative upkeep window (no mana to pay with):
			// sacrifice it. Its leave trigger (RemoveCounterAll RUST) is inert
			// -- nothing was ever placed.
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "cumulative_sac" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("unexpected choose during the upkeep: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision %s during the upkeep: %+v", d.Kind, d)
		}
	}
	if !asked {
		t.Fatal("the ValidTgts$ Opponent trigger never asked for its target")
	}
	if !hasNote(e, "unimplemented PutCounterAll shape") {
		t.Fatal("no unimplemented-shape note for the ValidTgts$ Opponent sweep")
	}
	for _, ev := range e.L.Events {
		// A POSITIVE RUST amount would be a placement; the negative ones here
		// are Corrosion's own leave trigger (RemoveCounterAll RUST, clamped
		// at zero by state.Object.AddCounter -- pre-existing sibling
		// behaviour, not a placement).
		if ev.Kind == events.CounterChange && ev.Counter == "RUST" && ev.Amount > 0 {
			t.Fatalf("the ValidTgts$ Opponent sweep placed rust counters anyway: %+v", ev)
		}
	}
	if got := e.G.Obj(bear).Counter("RUST"); got != 0 {
		t.Fatalf("seat 0's Rust Bear took %d rust counters, want 0", got)
	}
	if got := e.G.Obj(enemy).Counter("RUST"); got != 0 {
		t.Fatalf("seat 1's Foe Bear took %d rust counters, want 0", got)
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 0's Rust Bear took %d +1/+1 counters, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestShalaiVoiceOfPlentyPutsACounterOnEachCreatureYouControl pins the sweep
// on its flagship real-corpus carrier Shalai, Voice of Plenty (a plain
// ValidCards$ Creature.YouCtrl AB$ activation, cost {4}{G}{G}, no tap): every
// creature seat 0 controls takes exactly one +1/+1 counter -- Shalai herself
// included, the "each creature you control" self-inclusion -- seat 1's
// creatures take nothing, and the resolution stays silent (no unimplemented
// Note).
func TestShalaiVoiceOfPlentyPutsACounterOnEachCreatureYouControl(t *testing.T) {
	t.Parallel()
	shalai := corpusCard(t, "Shalai, Voice of Plenty")
	e, cfg := putCounterTable(t, 202,
		[]*cards.Card{shalai, card(t, counterBear("Shalai First Bear")), card(t, counterBear("Shalai Second Bear"))},
		[]*cards.Card{card(t, counterBear("Shalai Enemy Bear"))})
	shalaiID := findAndMoveToBattlefield(t, e, 0, "Shalai, Voice of Plenty")
	first := findAndMoveToBattlefield(t, e, 0, "Shalai First Bear")
	second := findAndMoveToBattlefield(t, e, 0, "Shalai Second Bear")
	enemy := findAndMoveToBattlefield(t, e, 1, "Shalai Enemy Bear")
	// Cost {4}{G}{G}: the two green pips plus four generic the green covers.
	addMana(t, e, 0, "GGGGGG")
	opt := abilityOption(t, e, shalaiID, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)

	for _, id := range []state.ObjID{shalaiID, first, second} {
		if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
			t.Fatalf("creature %d has %d P1P1 counters, want 1 (Shalai included -- 'each creature you control')", id, got)
		}
		if n := len(counterChanges(e, id)); n != 1 {
			t.Fatalf("creature %d received %d CounterChange events, want exactly 1", id, n)
		}
	}
	if got := e.G.Obj(enemy).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 1's Shalai Enemy Bear took %d counters, want 0", got)
	}
	if n := len(counterChanges(e, enemy)); n != 0 {
		t.Fatalf("seat 1's Shalai Enemy Bear received %d CounterChange events, want 0", n)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("the Shalai sweep emitted an unimplemented Note anyway: %s", ev.Text)
		}
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool not drained: %d", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestMethodsOfTheMightyCharmModePutsCounters pins the one repo-deck card
// whose behaviour this registration changes (avengers-assemble.json carries
// Methods of the Mighty): choosing its DB$ PutCounterAll Charm mode places a
// real +1/+1 batch on every creature you control -- before the fix the mode
// emitted the fallback Note and did nothing.
func TestMethodsOfTheMightyCharmModePutsCounters(t *testing.T) {
	t.Parallel()
	methods := corpusCard(t, "Methods of the Mighty")
	e, cfg := putCounterTable(t, 210,
		[]*cards.Card{methods, card(t, counterBear("Charm Bear"))},
		[]*cards.Card{card(t, counterBear("Enemy Bear"))})
	// The spell is an instant: make sure it sits in hand so the cast option
	// is offered from there (the corpus deal may have left it in the library).
	var methodsID state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Methods of the Mighty" {
				methodsID = id
				if z == state.ZLibrary {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZHand})
				}
			}
		}
	}
	if methodsID == 0 {
		t.Fatal("Methods of the Mighty was not dealt")
	}
	bear := findAndMoveToBattlefield(t, e, 0, "Charm Bear")
	enemy := findAndMoveToBattlefield(t, e, 1, "Enemy Bear")
	for _, r := range []string{"W", "W", "W", "W"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: r, Amount: 1})
	}
	e.priorityRound()

	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == methodsID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Methods of the Mighty: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	mode := modeOptionContaining(t, e.Pending(), "counter on each creature you control")
	submitChoices(t, e, mode)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("Charm Bear has %d P1P1 counters, want 1", got)
	}
	if got := e.G.Obj(enemy).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 1's Enemy Bear took %d counters, want 0", got)
	}
	replayCheck(t, e, cfg)
}
