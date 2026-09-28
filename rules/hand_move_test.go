package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The handmove1 end-to-end leaves: a Burgeoning-shaped "choose N cards
// matching ChangeType$ from Origin$ Hand" ChangeZone reached through the real
// engine (opponent plays a land, the optional trigger accepted) poses the
// hand-choice KChoose, the answer moves exactly the picked card, and the
// whole game replays from the log. The effects-package halves (the ask shape,
// the re-entry contract, the R-9 no-host fallback, the fx42 scoping) live in
// effects/hand_move_test.go.

// burgeoningSrc is Burgeoning's own script shape: an optional trigger on an
// opponent's land play whose Execute$ is the broken ChangeZone shape --
// Origin$ Hand, a ChangeType$ filter, no Defined$/DefinedPlayer$/ValidTgts$.
const burgeoningSrc = "Name:Burgeoning\nTypes:Enchantment\n" +
	"T:Mode$ LandPlayed | ValidCard$ Land.OppCtrl | TriggerZones$ Battlefield | OptionalDecider$ You | Execute$ TrigDropLand\n" +
	"SVar:TrigDropLand:DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land\nOracle:Whenever an opponent plays a land, you may put a land card from your hand onto the battlefield.\n"

// burgeoningFixture seats Burgeoning on seat 0's battlefield and arranges
// seat 0's hand to hold EXACTLY handLands Mountains: the mountain-deck filler
// leaves the opening hand full of them, so handLands == 0 first bridges every
// hand Mountain back to the library, then exactly handLands are bridged out
// of the library. All zone changes go through the event path.
func burgeoningFixture(t *testing.T, seed uint64, handLands int) (*Engine, Config) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, burgeoningSrc)
	// Empty the hand of Mountains first, whatever the seeded shuffle drew.
	for _, oid := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...) {
		if o := e.G.Obj(oid); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZHand, To: state.ZLibrary, Player: 0})
		}
	}
	// Bridge exactly handLands Mountains from the library into the hand.
	moved := 0
	for _, oid := range e.G.Zone(state.ZLibrary, 0) {
		if moved >= handLands {
			break
		}
		if o := e.G.Obj(oid); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZLibrary, To: state.ZHand, Player: 0})
			moved++
		}
	}
	if moved != handLands {
		t.Fatalf("fixture library lacks Mountains: bridged %d, want %d", moved, handLands)
	}
	// Seat the enchantment (came back from the fixture's own hand/library
	// bridge; find it wherever it is now) on the battlefield.
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
	}
	// The setup ran after Advance built the upkeep priority snapshot; the
	// snapshot's options are stale. Clear and re-drive (the same net state
	// the live Submit loop reaches).
	e.pending = nil
	e.Advance()
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepMain1)
	return e, cfg
}

// opponentPlaysALand drives to seat 1's own turn (a land can be played only
// during its controller's own main phase, CR 305.1) and has seat 1 play one
// Mountain, returning the pending decision after the play -- for a Burgeoning
// game that is seat 0's trigger_optional ask.
func opponentPlaysALand(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	driveToStep(t, e, 2, 1, state.StepMain1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("expected seat 1's priority on their own main1, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			idx = o.Index
			break
		}
	}
	if idx < 0 {
		t.Fatalf("seat 1 has no play_land option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The land play puts the trigger on the stack; drain the priority rounds
	// until the trigger reaches its resolution ask.
	return passUntilNonPriority(t, e, 20)
}

// handToBattlefieldMoves counts seat 0 hand->battlefield MoveZone events for
// one object id anywhere in the log.
func handToBattlefieldMoves(log []events.Event, id state.ObjID) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZHand && ev.To == state.ZBattlefield {
			n++
		}
	}
	return n
}

// TestBurgeoningAsksForTheHandLandAndHonoursTheAnswer is the core leaf: with
// two lands in hand (a strict superset of ChangeNum 1), answering "yes" to
// the optional trigger poses a real KChoose over the hand lands, and the
// answer -- here the SECOND option, proving the choice is honoured, not a
// first-eligible default -- moves exactly that card onto the battlefield.
func TestBurgeoningAsksForTheHandLandAndHonoursTheAnswer(t *testing.T) {
	t.Parallel()
	e, cfg := burgeoningFixture(t, 71, 2)
	handBefore := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)

	d := opponentPlaysALand(t, e)
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0) // yes

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "hand_move" {
		t.Fatalf("expected a pending hand_move KChoose, got %+v", d)
	}
	if d.Player != 0 || d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max/Player = %d/%d/%d, want 0/1/0 (Burgeoning's optional take: none is a legal answer, the hand's owner asks)", d.Min, d.Max, d.Player)
	}
	if d.ResumeKind != "hand_move" {
		t.Fatalf("ResumeKind = %q, want \"hand_move\"", d.ResumeKind)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %d, want 2 (the two Mountains; nothing else is pickable)", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Label != "Mountain" {
			t.Fatalf("option label = %q, want the face name", o.Label)
		}
	}
	picked := d.Options[1].Obj
	submitChoices(t, e, 1)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(picked); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the picked land is not on the battlefield: %+v", o)
	}
	if n := handToBattlefieldMoves(e.L.Events, picked); n != 1 {
		t.Fatalf("picked land moved hand->battlefield %d times, want exactly 1", n)
	}
	handAfter := e.G.Zone(state.ZHand, 0)
	if len(handAfter) != len(handBefore)-1 {
		t.Fatalf("hand size %d, want %d", len(handAfter), len(handBefore)-1)
	}
	for _, oid := range handAfter {
		if oid == picked {
			t.Fatalf("the picked land is still in hand: %v", handAfter)
		}
	}
	replayCheck(t, e, cfg)
}

// TestBurgeoningNoLandInHandResolvesSilently is the zero-eligible leaf:
// answering "yes" with no land in hand resolves the trigger doing nothing --
// no ask, no move, no wedge. Seat 1's own land play is a legitimate
// hand->battlefield move; the guard is that none of SEAT 0's hand objects
// moved.
func TestBurgeoningNoLandInHandResolvesSilently(t *testing.T) {
	t.Parallel()
	e, cfg := burgeoningFixture(t, 72, 0)
	handIDs := make(map[state.ObjID]bool)
	for _, oid := range e.G.Zone(state.ZHand, 0) {
		handIDs[oid] = true
	}

	d := opponentPlaysALand(t, e)
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0) // yes

	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected the resolution to complete back at priority, got %+v", d)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZBattlefield && handIDs[ev.Obj] {
			t.Fatalf("a seat 0 hand object moved hand->battlefield with an empty-land hand: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestBurgeoningSingleLandCanBeDeclined is the one-eligible optional leaf:
// even when taking its single land is the only nonempty selection, "you may"
// makes declining a distinct legal answer. The hand move therefore poses a
// Min 0 / Max 1 KChoose, and its empty answer leaves the land in hand.
func TestBurgeoningSingleLandCanBeDeclined(t *testing.T) {
	t.Parallel()
	e, cfg := burgeoningFixture(t, 73, 1)
	handBefore := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)

	d := opponentPlaysALand(t, e)
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0) // accept the trigger; decline its hand move below

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" ||
		d.Player != 0 || d.Min != 0 || d.Max != 1 || len(d.Options) != 1 {
		t.Fatalf("single-land optional hand move = %+v, want one-option Min/Max 0/1 KChoose", d)
	}
	submitChoices(t, e) // the legal empty answer: decline to put it in play
	passUntilStackEmpty(t, e, 20)

	land := handBefore[0]
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the declined single hand land is on %+v, want hand", o)
	}
	if n := handToBattlefieldMoves(e.L.Events, land); n != 0 {
		t.Fatalf("declined land moved hand->battlefield %d times, want 0", n)
	}
	replayCheck(t, e, cfg)
}

// --- rv2b: the Brainstorm-shaped whole-hand put-back, end to end on the real
// corpus scripts. ---

// brainFinds returns the option index whose Obj is id in a pending hand_move
// KChoose.
func handMoveOption(t *testing.T, d *decision.Decision, id state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("hand card %d not offered in %+v", id, d.Options)
	return -1
}

// TestBrainstormPutsTwoChosenCardsBackOnTop casts the real corpus Brainstorm
// at a hand holding it and two bears: it draws three, then the mandatory
// ChangeZoneDB put-back asks over the WHOLE hand (no ChangeType$), and the
// answer -- the two bears named in REVERSE hand order, proving the choice and
// the order are honoured -- lands on TOP of the library in answer order.
func TestBrainstormPutsTwoChosenCardsBackOnTop(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	brainstorm := mustCorpusCard(t, reg, "Brainstorm")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := Config{Seed: 5, Names: []string{"a", "b"}, Tokens: map[string]*cards.Card{},
		Decks: [][]*cards.Card{append([]*cards.Card{brainstorm, bear, bear}, mountainDeck(t, 37)...),
			mountainDeck(t, 40)}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	// Arrange the hand through LOGGED zone moves (the setup every cast
	// fixture uses, so the log-only replay reconstructs it): Brainstorm and
	// both bears into hand, one blue mana into the pool, then drive to seat
	// 0's own main1.
	var bID state.ObjID
	var bearIDs []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 {
			continue
		}
		switch o.Card {
		case brainstorm:
			bID = o.ID
		case bear:
			bearIDs = append(bearIDs, o.ID)
		}
	}
	if bID == 0 || len(bearIDs) != 2 {
		t.Fatalf("fixture deck lacks the cards: brainstorm %d, bears %v", bID, bearIDs)
	}
	for _, id := range append([]state.ObjID{bID}, bearIDs...) {
		if o := e.G.Obj(id); o.Zone != state.ZHand {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZHand})
		}
	}
	addMana(t, e, 0, "U")
	e.pending = nil
	e.Advance()
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepMain1)

	submitChoices(t, e, passToCast(t, e, bID))
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("expected the hand_move put-back ask, got %+v", d)
	}
	if d.Min != 2 || d.Max != 2 || d.Player != 0 {
		t.Fatalf("Min/Max/Player = %d/%d/%d, want 2/2/0 (Mandatory$ True takes two)", d.Min, d.Max, d.Player)
	}
	// The whole hand is offered: the two bears + three drawn Mountains (the
	// cast Brainstorm itself is on the stack, not in the hand).
	handAtAsk := e.G.Zone(state.ZHand, 0)
	if len(d.Options) != len(handAtAsk) {
		t.Fatalf("options = %d, want the whole hand (%d: no ChangeType$ filter)", len(d.Options), len(handAtAsk))
	}
	var bearList []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Grizzly Bears" {
			bearList = append(bearList, id)
		}
	}
	if len(bearList) != 2 {
		t.Fatalf("hand holds %d bears, want 2", len(bearList))
	}
	// Answer in REVERSE hand order: the order the answer gives is the order
	// on top (Brainstorm's Reorder$ True "in any order").
	secondFirst := bearList[1]
	firstSecond := bearList[0]
	submitChoices(t, e, handMoveOption(t, d, secondFirst), handMoveOption(t, d, firstSecond))
	passUntilStackEmpty(t, e, 20)

	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 2 || lib[0] != secondFirst || lib[1] != firstSecond {
		t.Fatalf("library top = %v..., want [%d %d] in answer order on top", lib[:2], secondFirst, firstSecond)
	}
	if len(e.G.Zone(state.ZHand, 0)) != len(handAtAsk)-2 {
		t.Fatalf("hand size %d, want %d (hand at ask minus the two put back)",
			len(e.G.Zone(state.ZHand, 0)), len(handAtAsk)-2)
	}
	moved := 0
	var sawOrder bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && (ev.Obj == secondFirst || ev.Obj == firstSecond) &&
			ev.From == state.ZHand && ev.To == state.ZLibrary {
			moved++
		}
		if ev.Kind == events.LibraryOrder && ev.Player == 0 {
			sawOrder = true
		}
	}
	if moved != 2 || !sawOrder {
		t.Fatalf("put-back moves=%d (want 2), LibraryOrder=%v", moved, sawOrder)
	}
	replayCheck(t, e, cfg)
}

// TestJaceTheMindSculptorZeroAbilityPutsTwoBackOnTop activates the real
// corpus Jace's [0] on a board with two bears in hand: it draws three, then
// the mandatory put-back (ChangeType$ Card, ChangeNum$ 2, LibraryPosition$
// 0) asks over the whole hand, and the answered two bears land on TOP in
// answer order, with the walker's loyalty untouched by the free [+0] cost.
func TestJaceTheMindSculptorZeroAbilityPutsTwoBackOnTop(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	zero := jaceAbility(t, jaceCard, "AddCounter", 0)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", bear, bear)
	// BOTH bear copies into hand (cardToHand early-returns on a copy already
	// in hand, so it cannot be called twice for two copies).
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == bear && o.Zone != state.ZHand {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZHand})
		}
	}
	e.pending = nil
	e.Advance()

	opt := abilityOption(t, e, jace, zero)
	submitChoices(t, e, opt.Index)
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("expected the [0] put-back ask, got %+v", d)
	}
	if d.Min != 2 || d.Max != 2 || d.Player != 0 {
		t.Fatalf("Min/Max/Player = %d/%d/%d, want 2/2/0", d.Min, d.Max, d.Player)
	}
	handAtAsk := e.G.Zone(state.ZHand, 0)
	if len(d.Options) != len(handAtAsk) {
		t.Fatalf("options = %d, want the whole hand (%d)", len(d.Options), len(handAtAsk))
	}
	var bearIDs []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Grizzly Bears" {
			bearIDs = append(bearIDs, id)
		}
	}
	submitChoices(t, e, handMoveOption(t, d, bearIDs[1]), handMoveOption(t, d, bearIDs[0]))
	passUntilStackEmpty(t, e, 20)

	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 2 || lib[0] != bearIDs[1] || lib[1] != bearIDs[0] {
		t.Fatalf("library top = %v, want [%d %d] in answer order on top", lib, bearIDs[1], bearIDs[0])
	}
	if got := e.G.Obj(jace).Counter("LOYALTY"); got != 3 {
		t.Fatalf("Jace loyalty = %d, want 3 (the [+0] cost adds none)", got)
	}
	if len(e.G.Zone(state.ZHand, 0)) != len(handAtAsk)-2 {
		t.Fatalf("hand size %d, want %d (the hand at ask minus the two put back)",
			len(e.G.Zone(state.ZHand, 0)), len(handAtAsk)-2)
	}
	replayCheck(t, e, cfg)
}

// --- rv2b r2: the owner-SELECTED hidden-hand shape, end to end on the real
// corpus Kynaios and Tiro of Meletis -- the finding's breaking card. ---

// TestKynaiosAndTiroAsksEachPlayerForItsOwnLand runs the real corpus
// Kynaios and Tiro of Meletis end to end: seated on seat 0's battlefield and
// driven to its controller's end step, the end-step trigger fires, draws,
// then EachPlayLand (DefinedPlayer$ Player, ChangeType$ Land, ChangeNum$ 1,
// RememberChanged$ True, no Chooser$ -- the hand's owner answers) asks EACH
// player for its own hand in turn: one suspension per owner, the cursor
// chaining the second ask off the first answer, and each answer moving
// exactly that owner's chosen land. Pre-rv2b-r2 the whole sub-ability
// silently no-op'd (the player targets fell off the object path's Origin$
// precondition) and no ask of any kind was ever posed.
func TestKynaiosAndTiroAsksEachPlayerForItsOwnLand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kyn := mustCorpusCard(t, reg, "Kynaios and Tiro of Meletis")
	db := kynAbilities(t, reg)
	if db.Params["DefinedPlayer"] != "Player" || db.Params["ChangeType"] != "Land" ||
		db.Params["ChangeNum"] != "1" || db.Params["RememberChanged"] != "True" ||
		db.Params["Chooser"] != "" {
		t.Fatalf("Kynaios's compiled EachPlayLand drifted: %+v", db.Params)
	}
	cfg := Config{Seed: 5, Names: []string{"a", "b"}, Tokens: map[string]*cards.Card{},
		Decks: [][]*cards.Card{append([]*cards.Card{kyn}, mountainDeck(t, 59)...),
			mountainDeck(t, 60)}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	var kynID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == kyn {
			kynID = o.ID
		}
	}
	if kynID == 0 {
		t.Fatal("fixture deck lacks Kynaios")
	}
	if o := e.G.Obj(kynID); o.Zone != state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: kynID, From: o.Zone, To: state.ZBattlefield})
	}
	e.pending = nil
	e.Advance()
	driveToStep(t, e, e.G.Turn, 0, state.StepEnd)

	// The trigger draws for seat 0, then EachPlayLand asks owner 0.
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("expected the first hand_move ask, got %+v", d)
	}
	if d.Player != 0 || d.ResumeTarget != 0 || d.Min != 0 || d.Max != 1 {
		t.Fatalf("first ask = Player %d target %d Min/Max %d/%d, want 0/0/0/1 (the hand's owner answers its own optional take)", d.Player, d.ResumeTarget, d.Min, d.Max)
	}
	for _, o := range d.Options {
		if o.Player != 0 || o.Label != "Mountain" {
			t.Fatalf("first ask option %+v is not one of seat 0's Mountains", o)
		}
	}
	if len(d.Options) != len(e.G.Zone(state.ZHand, 0)) {
		t.Fatalf("first ask offers %d cards, want seat 0's whole hand (%d)", len(d.Options), len(e.G.Zone(state.ZHand, 0)))
	}
	land0 := e.G.Zone(state.ZHand, 0)[0]
	submitChoices(t, e, handMoveOption(t, d, land0))

	// The continuation: the SECOND ask belongs to seat 1, bound by
	// ResumeTarget, over seat 1's own hand.
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("expected the second hand_move ask (the continuation), got %+v", d)
	}
	if d.Player != 1 || d.ResumeTarget != 1 || d.Min != 0 || d.Max != 1 {
		t.Fatalf("second ask = Player %d target %d Min/Max %d/%d, want 1/1/0/1", d.Player, d.ResumeTarget, d.Min, d.Max)
	}
	land1 := e.G.Zone(state.ZHand, 1)[0]
	submitChoices(t, e, handMoveOption(t, d, land1))
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(land0); o.Zone != state.ZBattlefield {
		t.Fatalf("seat 0's chosen land on %s, want battlefield", o.Zone)
	}
	if o := e.G.Obj(land1); o.Zone != state.ZBattlefield {
		t.Fatalf("seat 1's chosen land on %s, want battlefield", o.Zone)
	}
	for p := state.PlayerID(0); p < 2; p++ {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZBattlefield &&
				e.G.Obj(ev.Obj) != nil && e.G.Obj(ev.Obj).Owner == p && ev.Obj != kynID {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("seat %d moved %d hand lands to the battlefield, want exactly 1", p, n)
		}
	}
	replayCheck(t, e, cfg)
}

// kynAbilities returns the real compiled EachPlayLand ChangeZone sub-ability
// of the real corpus Kynaios and Tiro of Meletis (the end-step trigger's
// TrigDraw chain).
func kynAbilities(t *testing.T, reg *cards.Registry) *cards.SA {
	t.Helper()
	kyn, ok := reg.Lookup("Kynaios and Tiro of Meletis")
	if !ok {
		t.Fatal("corpus has no Kynaios and Tiro of Meletis")
	}
	for _, face := range kyn.Faces {
		for _, ab := range face.Abilities {
			if ab.API == "Draw" && ab.Sub != nil && ab.Sub.API == "ChangeZone" {
				return ab.Sub
			}
		}
		for _, tr := range face.Triggers {
			if tr.Effect != nil && tr.Effect.API == "Draw" && tr.Effect.Sub != nil && tr.Effect.Sub.API == "ChangeZone" {
				return tr.Effect.Sub
			}
		}
	}
	t.Fatal("Kynaios's TrigDraw has no ChangeZone sub-ability in the compiled corpus")
	return nil
}
