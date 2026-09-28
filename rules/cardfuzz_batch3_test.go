package rules

// Regressions for four cardfuzz batch-3 failure classes, each pinned on
// real corpus cards:
//
//   - a ceased ability object reaching a graveyard's card list and a mana
//     ability's ExileFromGrave cost dereferencing its nil Face
//     (continueManaDiscard; Rubble Rouser);
//   - a Menace attacker whose second blocker the bot could not keep (a
//     tap-costed blocker it drops, a Max$ 1 trim), leaving a lone blocker
//     the engine rejects (Boggart Brute vs Hollow Warrior; Familiar Ground);
//   - a repeatable modal ask with fewer legal modes than CharmNum$ that the
//     bot under-filled (Mystic Confluence with only its draw mode legal);
//   - Krenko, Mob Boss's doubling activation read as a livelock: X identical
//     TokenCreate events in a row matched the watcher's period-1 detector.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestManaExileCostNeverOffersACeasedAbility: a resolved ability that a
// later exile->graveyard move names (the cardfuzz shape) must not join the
// graveyard's card list, so Rubble Rouser's "{T}, Exile a card from your
// graveyard: Add {R}" offers only real cards and never panics on a nil Face.
func TestManaExileCostNeverOffersACeasedAbility(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	e.G.Step = state.StepEnd
	e.finishEnteredStep()
	if e.putTriggersOnStack() || len(e.G.Stack) != 1 {
		t.Fatalf("precondition: monarch draw trigger not on the stack: %v", e.G.Stack)
	}
	ab := e.G.Stack[0]
	e.resolveTop()
	// The move an exile walker would make: the ability's logged exile
	// parking to its owner's graveyard.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ab, From: state.ZExile, To: state.ZGraveyard})
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if id == ab {
			t.Fatalf("ceased ability %d joined the graveyard list", ab)
		}
	}
	// Two real graveyard cards, so the exile cost poses a choice.
	lib := e.G.Zone(state.ZLibrary, 0)
	cardsIn := []state.ObjID{lib[0], lib[1]}
	for _, id := range cardsIn {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	rouser := onBoardCard(t, e, 0, corpusCard(t, "Rubble Rouser"))
	e.G.Obj(rouser).SummonSick = false
	var ma *cards.SA
	for _, a := range e.G.Obj(rouser).Face().Abilities {
		if a.API == "Mana" {
			ma = a
		}
	}
	if ma == nil {
		t.Fatal("Rubble Rouser has no mana ability")
	}
	e.G.Step = state.StepMain1
	e.pending = nil
	e.resolveManaAbility(0, rouser, ma, false)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("exile-cost decision = %+v, want a choice over the two graveyard cards", d)
	}
	for _, o := range d.Options {
		if o.Obj == ab || e.G.Obj(o.Obj).Face() == nil {
			t.Fatalf("exile cost offered non-card object %d: %+v", o.Obj, d.Options)
		}
	}
	submitChoices(t, e, 0)
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after Rubble Rouser = %d, want 1", got)
	}
}

// menaceBlockEngine is a combat fixture: seat 1 attacks seat 0 with the
// corpus Boggart Brute (3/2 Menace).
func menaceBlockEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := combatEngine(t)
	e.G.Active = 1
	brute := onBoardReadyCard(t, e, 1, corpusCard(t, "Boggart Brute"))
	if !e.HasKeyword(brute, "Menace") {
		t.Fatal("precondition: corpus Boggart Brute lacks Menace")
	}
	return e, brute
}

func declareAndAskBlockers(e *Engine, attacker state.ObjID) *decision.Decision {
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	return e.Pending()
}

// TestMenaceFloorIsPublishedAndBotNeverSubmitsALoneBlocker: the cardfuzz
// shape. Of the defender's two blockers one (Hollow Warrior) carries a tap
// block cost the bot's guard drops, which used to leave a lone blocker on a
// Menace attacker. The engine now publishes Menace as MinBlockers 2 on the
// option, and the bot's answer always validates.
func TestMenaceFloorIsPublishedAndBotNeverSubmitsALoneBlocker(t *testing.T) {
	t.Parallel()
	e, brute := menaceBlockEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	onBoardCard(t, e, 0, corpusCard(t, "Hollow Warrior"))
	onBoardCard(t, e, 0, corpusCard(t, "Bloodghast")) // can't block; pays the tap
	e.G.Players[0].Life = 3                           // unblocked Brute is lethal
	d := declareAndAskBlockers(e, brute)
	if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
		t.Fatalf("blockers decision = %+v, want Bears and Hollow Warrior against the Brute", d)
	}
	taps := 0
	for _, o := range d.Options {
		if o.MinBlockers != 2 {
			t.Fatalf("option %+v does not publish the Menace floor (MinBlockers 2)", o)
		}
		if o.CostTaps > 0 {
			taps++
		}
	}
	if taps != 1 {
		t.Fatalf("precondition: want exactly one tap-costed option, got %d: %+v", taps, d.Options)
	}
	in := newTestBot(1).answer(e, d)
	if len(in.Choices) == 1 {
		t.Fatalf("bot declared a lone blocker on a Menace attacker: %+v", in)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot block declaration rejected: %v", err)
	}
}

// TestMenaceWithMaxOneBlockerIsUnblockable: Menace (at least two) and
// Familiar Ground's Max$ 1 (at most one) leave no legal blocking
// declaration, so the engine offers no pair against the attacker at all --
// previously it offered Min 0/Max 1 pairs and the bot's Max trim produced
// the rejected lone block.
func TestMenaceWithMaxOneBlockerIsUnblockable(t *testing.T) {
	t.Parallel()
	e, brute := menaceBlockEngine(t)
	onBoardCard(t, e, 1, corpusCard(t, "Familiar Ground"))
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	d := declareAndAskBlockers(e, brute)
	if d != nil && d.Kind == decision.KBlockers {
		for _, o := range d.Options {
			if o.Attacker == brute {
				t.Fatalf("a Menace attacker that can't be blocked by more than one creature was offered a blocker: %+v", d.Options)
			}
		}
	}
}

// TestBotFillsRepeatableModesWithOneLegalMode: Mystic Confluence
// ("Choose three. You may choose the same mode more than once.") on an empty
// board leaves only its draw mode legal. The bot must answer the 3..3 ask
// with that mode three times; it used to submit two picks and wedge.
func TestBotFillsRepeatableModesWithOneLegalMode(t *testing.T) {
	t.Parallel()
	mystic := corpusCard(t, "Mystic Confluence")
	cfg := seatZeroStart(Config{Seed: 3, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{mystic}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	var id state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, c := range e.G.Zone(z, 0) {
			if e.G.Obj(c).Face().Name == "Mystic Confluence" {
				id = c
			}
		}
	}
	if id == 0 {
		t.Fatal("Mystic Confluence not dealt")
	}
	if e.G.Obj(id).Zone == state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	addMana(t, e, 0, "UUUUU1")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes || !d.Repeatable || d.Min != 3 || len(d.Options) != 1 {
		t.Fatalf("mode ask = %+v, want a repeatable 3..3 ask over the lone draw mode", d)
	}
	hand := len(e.G.Zone(state.ZHand, 0))
	in := newTestBot(1).answer(e, d)
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot modal answer rejected: %v (%+v)", err, in)
	}
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+3 {
		t.Fatalf("hand after three draw modes = %d, want %d", got, hand+3)
	}
	replayCheck(t, e, cfg)
}

// TestKrenkoBatchIsNotALivelock: Krenko, Mob Boss creates X identical
// TokenCreate events in one resolution. With the cycle threshold lowered
// below X, the watcher used to abort that legitimate batch as a period-1
// cycle; each mint adds a fresh object, so it is progress, not a loop.
func TestKrenkoBatchIsNotALivelock(t *testing.T) {
	t.Parallel()
	krenko := corpusCard(t, "Krenko, Mob Boss")
	gob := corpusCard(t, "Mons's Goblin Raiders")
	deck := []*cards.Card{krenko}
	for i := 0; i < 7; i++ {
		deck = append(deck, gob)
	}
	cfg := seatZeroStart(Config{Seed: 5, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck, mountainDeck(t, 32)...), mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{
			"r_1_1_goblin": card(t, "Name:Goblin Token\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"),
		},
		LoopGuard: &LoopGuard{CycleEvents: 6},
	})
	e := New(cfg)
	e.Advance()
	var kid state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, c := range append([]state.ObjID(nil), e.G.Zone(z, 0)...) {
			n := e.G.Obj(c).Face().Name
			if n != "Krenko, Mob Boss" && n != "Mons's Goblin Raiders" {
				continue
			}
			if n == "Krenko, Mob Boss" {
				kid = c
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: c, From: z, To: state.ZBattlefield})
		}
	}
	e.pending = nil
	e.Advance()
	goblins := func() int {
		n := 0
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			switch e.G.Obj(id).Face().Name {
			case "Krenko, Mob Boss", "Mons's Goblin Raiders", "Goblin Token":
				n++
			}
		}
		return n
	}
	if goblins() != 8 {
		t.Fatalf("precondition: %d goblins on the battlefield, want 8", goblins())
	}
	driveToStep(t, e, 3, 0, state.StepMain1)
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == kid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Krenko's ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if got := goblins(); got != 16 {
		t.Fatalf("goblins after Krenko = %d, want 16 (X = 8)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMenaceAgainstOneLegalBlockerPosesNoPair: a Menace attacker facing a
// defender with a single creature that can block has no legal blocking
// declaration but the empty one (CR 702.111b), so no pair is offered -- the
// shape that removed a forced-empty blockers ask from host's committed
// overshoot capture (Undead Augur with Lord of the Accursed's menace vs a
// lone untapped Lathliss).
func TestMenaceAgainstOneLegalBlockerPosesNoPair(t *testing.T) {
	t.Parallel()
	e, brute := menaceBlockEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	d := declareAndAskBlockers(e, brute)
	if d != nil && d.Kind == decision.KBlockers {
		for _, o := range d.Options {
			if o.Attacker == brute {
				t.Fatalf("a lone blocker was offered against a Menace attacker: %+v", d.Options)
			}
		}
	}
}
