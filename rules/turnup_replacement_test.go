// turnup_replacement_test.go — the "as this is turned face up" replacement
// class (R:Event$ TurnFaceUp, CR 614.1a with CR 708.6/702.36e) and the
// T:Mode$ TurnFaceUp trigger observed off the morph-family special action,
// task cli-20260924T031747Z-6d0658fc. Each corpus carrier proves, end to
// end:
//
//   - Hooded Hydra: its "As Hooded Hydra is turned face up, put five +1/+1
//     counters on it" replacement runs as the turn-up happens — the
//     permanent finishes face UP with exactly five P1/P1 counters (0/0 base
//   - 5 = 5/5), the turn-up was never prevented, and the game replays.
//   - Trail of Mystery: its "Whenever a permanent you control is turned
//     face up, if it's a creature, it gets +2/+2 until end of turn" trigger
//     observes the morph special action's single TurnFaceUp event exactly
//     once and pumps the turned-up creature (1/1 → 3/3).
//   - Karlov Watchdog: "Permanents your opponents control can't be turned
//     face up during your turn" is a live CantHappen turn-up replacement —
//     it makes the opponent's turn_face_up special action ILLEGAL while it
//     is that controller's turn (the option is not even offered), leaves
//     the opponent's own-turn turn-up alone, and the permanent still turns
//     up on the opponent's own turn and replays.
//
// The decks are compiled corpus cards only (no Forge script text is
// committed here). The helpers come from morph_test.go, morph_turnup_test.go,
// cast_test.go, search_library_test.go and ninjutsu_seat0_defender_test.go.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// turnFaceUpOfferedFor reports whether seat p's legal actions carry a
// turn_face_up option for id.
func turnFaceUpOfferedFor(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) bool {
	t.Helper()
	for _, o := range e.legalActions(p) {
		if o.Kind == "turn_face_up" && o.Obj == id {
			return true
		}
	}
	return false
}

// turnFaceUpIndexSeat is turnFaceUpIndex for an arbitrary seat: the pending
// decision must be seat p's priority, and it must carry the turn_face_up
// option for id.
func turnFaceUpIndexSeat(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	if d.Player != p {
		t.Fatalf("priority decision is seat %d's, want seat %d's", d.Player, p)
	}
	for _, o := range d.Options {
		if o.Kind == "turn_face_up" && o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("no turn_face_up option for %d: %+v", id, d.Options)
	return -1
}

// castFromHandByName casts the named card from seat 0's hand with its plain printed
// cast option and resolves it. It asserts the card reached the battlefield —
// a vacuous cast must fail loudly here, not in a later assertion.
func castFromHandByName(t *testing.T, e *Engine, name, symbols string) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, symbols)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no plain cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s did not reach the battlefield (zone=%v)", name, o)
	}
	return id
}

// morphDownCastSeat is morphDownCast for an arbitrary seat: seat p casts its
// own copy of name face down (mode) and pays the {3}, ending at poolAfter.
// It asserts the PAIR precondition (the printed cast option is offered
// beside the face-down one) and no target ask (CR 708.4), exactly as the
// seat-0 helper does.
func morphDownCastSeat(t *testing.T, e *Engine, p state.PlayerID, name, mode, symbols string, poolAfter int) state.ObjID {
	t.Helper()
	id := searchMoveByNameSeat(t, e, p, name, state.ZHand)
	addMana(t, e, p, symbols)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	if d.Player != p {
		t.Fatalf("priority decision is seat %d's, want seat %d's (morphing down on someone else's turn)", d.Player, p)
	}
	plain, down := -1, -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			if o.Mode == "" {
				plain = o.Index
			}
			if o.Mode == mode {
				down = o.Index
			}
		}
	}
	if plain < 0 {
		t.Fatalf("the printed cast option for %s is missing — the face-down option must be an ADDITION to it: %+v", name, d.Options)
	}
	if down < 0 {
		t.Fatalf("no (%s) face-down cast option for %s: %+v", mode, name, d.Options)
	}
	submitChoices(t, e, down)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("face-down cast posed a target ask: %+v", d)
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[p].Pool.Total(); got != int32(poolAfter) {
		t.Fatalf("pool after face-down cast = %d, want %d (exactly the {3} spent)", got, poolAfter)
	}
	if o := e.G.Obj(id); o == nil || !o.FaceDown || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s is not a face-down battlefield permanent (obj=%+v)", name, o)
	}
	return id
}

// p1p1Counters counts the +1/+1 counters on id.
func p1p1Counters(t *testing.T, e *Engine, id state.ObjID) int32 {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("object %d is gone", id)
	}
	var n int32
	for _, c := range o.Counters {
		if c.Kind == "P1P1" {
			n += c.N
		}
	}
	return n
}

// TestHoodedHydraTurnFaceUpReplacementPutsFiveCounters proves the R:Event$
// TurnFaceUp replacement class: Hooded Hydra's "As CARDNAME is turned face
// up, put five +1/+1 counters on it" applies as the morph special action
// turns it up — exactly five P1/P1 counters on a permanent that IS face up
// (the augmenting replacement never prevented the flip), funded by the
// keyword's {3}{G}{G} turn-up cost, and the game replays byte-identically.
func TestHoodedHydraTurnFaceUpReplacementPutsFiveCounters(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Hooded Hydra")
	// The face-down {3} plus the printed {X}{G}{G} ride together out of a
	// 2C+6G pool; the down cast leaves 5 mana, exactly the {3}{G}{G} the
	// turn-up costs.
	id := morphDownCast(t, e, "Hooded Hydra", "morphed", "CCCGGGGG", 5)
	// Precondition: a face-down 2/2 with NO +1/+1 counters (the etbCounter
	// X-counters belong to the printed cast, which never happened).
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: Hooded Hydra is not face down")
	}
	if n := p1p1Counters(t, e, id); n != 0 {
		t.Fatalf("precondition: Hooded Hydra carries %d P1P1 counters face down, want 0", n)
	}

	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("after the turn-up Hooded Hydra is in %s, want the battlefield", o.Zone)
	}
	if o.FaceDown {
		t.Fatalf("Hooded Hydra FaceDown=true after the turn-up, want false — an augmenting turn-up replacement must never prevent the flip")
	}
	if n := p1p1Counters(t, e, id); n != 5 {
		t.Fatalf("Hooded Hydra carries %d P1P1 counters after its turn-up replacement, want 5", n)
	}
	// The counters are live: 0/0 base + 5 = 5/5.
	if der := e.Derived(id); der.Power != 5 || der.Toughness != 5 {
		t.Fatalf("after the turn-up replacement Hooded Hydra is %d/%d, want its printed 0/0 with five +1/+1 counters", der.Power, der.Toughness)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the turn-up = %d, want 0 (the {3}{G}{G} morph cost was paid)", got)
	}
	replayCheck(t, e, cfg)
}

// TestTrailOfMysteryTurnFaceUpTriggerPumpsOnce proves the T:Mode$ TurnFaceUp
// trigger observes the morph special action's single TurnFaceUp event
// exactly once: Trail of Mystery's "Whenever a permanent you control is
// turned face up, if it's a creature, it gets +2/+2 until end of turn" fires
// once for one turn-up (1/1 → 3/3; a second firing would read 5/5, none
// would read 1/1), pushed exactly one trigger onto the stack, and the game
// replays. Trail of Mystery is cast AFTER the morph-down cast so its other
// trigger ("whenever a face-down creature you control enters") is not in
// play when the warden enters face down.
func TestTrailOfMysteryTurnFaceUpTriggerPumpsOnce(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Kin-Tree Warden", "Trail of Mystery")
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	trail := castFromHandByName(t, e, "Trail of Mystery", "CG")
	// Preconditions: the observer is a battlefield enchantment, the observed
	// permanent is a face-down creature, and the pool holds the {G} turn-up
	// cost.
	if o := e.G.Obj(trail); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Trail of Mystery is not on the battlefield (zone=%s)", o.Zone)
	}
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: Kin-Tree Warden is not face down")
	}

	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	// Exactly one trigger was pushed off the one TurnFaceUp event.
	pushes := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.TriggerPush {
			pushes++
		}
	}
	if pushes != 1 {
		t.Fatalf("Trail of Mystery pushed %d trigger(s) for one turn-up, want exactly 1", pushes)
	}
	// The pump landed: printed 1/1 + one +2/+2 = 3/3 (until end of turn).
	if der := e.Derived(id); der.Power != 3 || der.Toughness != 3 {
		t.Fatalf("after the turn-up trigger Kin-Tree Warden is %d/%d, want 3/3 (printed 1/1 with one +2/+2 pump)", der.Power, der.Toughness)
	}
	replayCheck(t, e, cfg)
}

// TestKarlovWatchdogTurnFaceUpCantHappenGatesTheOpponentsTurnUp proves the
// CantHappen turn-up replacement end to end across the turn boundary.
// Karlov Watchdog sits on seat 0's battlefield; seat 1's Kin-Tree Warden is
// face down:
//
//   - on seat 1's OWN turn the prohibition is dormant (PlayerTurn$ True is
//     the controller's turn) and the turn_face_up option IS offered — the
//     guard against over-blocking;
//   - on seat 0's turn the "can't be turned face up" prohibition makes the
//     special action ILLEGAL (CR 614.1a), so the option is not offered to
//     seat 1 at all;
//   - back on seat 1's turn the option returns, the action turns the warden
//     up for its {G}, and the game replays byte-identically.
func TestKarlovWatchdogTurnFaceUpCantHappenGatesTheOpponentsTurnUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// Each seat holds its own carrier: seat 0 the prohibition's source, seat
	// 1 the face-down morph target (manifestEngine deals every fixture into
	// SEAT 0's hand, so this scenario builds its own deck pair). All corpus
	// cards, replayable from cfg's genesis.
	forest := searchCorpusCard(t, reg, "Forest")
	fill := func(name string) []*cards.Card {
		deck := []*cards.Card{searchCorpusCard(t, reg, name)}
		for len(deck) < 40 {
			deck = append(deck, forest)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 7701, Names: []string{"karlov", "opponent"},
		Decks: [][]*cards.Card{fill("Karlov Watchdog"), fill("Kin-Tree Warden")}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// The plain {3}{W} cast needs 4 mana out of a 3C+3W pool (a vacuous cast
	// must fail the precondition, not silently skip the prohibition's source).
	karlov := castFromHandByName(t, e, "Karlov Watchdog", "CCCWWW")
	// Precondition: the prohibition's source is on seat 0's battlefield.
	if o := e.G.Obj(karlov); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Karlov Watchdog is not on the battlefield (zone=%s)", o.Zone)
	}
	// Seat 1 morphs its warden down on its OWN first turn (sorcery timing).
	driveToStep(t, e, 2, 1, state.StepMain1)
	id := morphDownCastSeat(t, e, 1, "Kin-Tree Warden", "morphed", "CCCG", 1)

	// Seat 1's own turn: the prohibition is dormant, the action is offered.
	if !turnFaceUpOfferedFor(t, e, 1, id) {
		t.Fatalf("turn_face_up not offered on the morph permanent's controller's own turn — the CantHappen replacement over-blocked (PlayerTurn$ True binds only during Karlov Watchdog's controller's turn)")
	}

	// Seat 0's turn: CR 614.1a — the action is illegal, never offered. Seat
	// 1 is FRESHLY FUNDED with the {G} the turn-up costs, so the absence of
	// the option is the prohibition's doing — an unfunded pool would make
	// this clause vacuously pass even without the gate. Karlov is on the
	// battlefield, so the drive answers the combat asks (empty attack).
	driveToStepAny(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 1, "G")
	if turnFaceUpOfferedFor(t, e, 1, id) {
		t.Fatalf("turn_face_up offered to seat 1 during seat 0's turn: Karlov Watchdog's CantHappen replacement must make the special action illegal (CR 614.1a)")
	}
	if reason := e.priorityOptionStale(1, decision.Option{Kind: "turn_face_up", Obj: id}); reason == "" {
		t.Fatal("stale turn_face_up option accepted after Karlov Watchdog's CantHappen became active")
	}

	// Seat 1's turn again: the action returns and completes. The {G} pool is
	// refilled — pools empty at the turn boundary, so the funded option is
	// the one whose presence this clause pins.
	driveToStepAny(t, e, 4, 1, state.StepMain1)
	addMana(t, e, 1, "G")
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: Kin-Tree Warden is no longer face down")
	}
	idx := turnFaceUpIndexSeat(t, e, 1, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.FaceDown {
		t.Fatalf("Kin-Tree Warden FaceDown=true after its own-turn turn-up, want false")
	}
	assertTurnUpEventOnce(t, e, id, mark)
	if der := e.Derived(id); der.Power != 1 || der.Toughness != 1 {
		t.Fatalf("after the turn-up Kin-Tree Warden is %d/%d, want its printed 1/1", der.Power, der.Toughness)
	}
	replayCheck(t, e, cfg)
}

// TestMasterOfPearlsTurnFaceUpSelfTriggerFiresOnce is the acceptance clause's
// real morph carrier: Master of Pearls, turned face up by the morph special
// action, fires its own "When CARDNAME is turned face up, creatures you
// control get +2/+2 until end of turn" trigger exactly once — exactly one
// TriggerPush off the one TurnFaceUp event, the pump lands (printed 2/2 →
// 4/4 until end of turn), and the game replays byte-identically. A second
// firing would read 6/6, none would read 2/2.
func TestMasterOfPearlsTurnFaceUpSelfTriggerFiresOnce(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Master of Pearls")
	// The face-down {3} plus the printed morph turn-up cost {3}{W}{W} ride
	// together out of a 5C+3W pool; the down cast leaves 5 mana, exactly the
	// turn-up cost.
	id := morphDownCast(t, e, "Master of Pearls", "morphed", "CCCCCWWW", 5)
	// Precondition: a face-down 2/2 with no pump and no pending trigger.
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: Master of Pearls is not face down")
	}
	if der := e.Derived(id); der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("precondition: face-down Master of Pearls is %d/%d, want the face-down 2/2", der.Power, der.Toughness)
	}

	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	// The special action emitted exactly one TurnFaceUp and used no stack;
	// the card's own turn-up trigger was pushed exactly once off it.
	assertTurnUpEventOnce(t, e, id, mark)
	pushes := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.TriggerPush {
			pushes++
		}
	}
	if pushes != 1 {
		t.Fatalf("Master of Pearls pushed %d trigger(s) for one turn-up, want exactly 1", pushes)
	}
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("after the turn-up Master of Pearls is in %s, want the battlefield", o.Zone)
	}
	if o.FaceDown {
		t.Fatalf("Master of Pearls FaceDown=true after the turn-up, want false")
	}
	// The pump landed: printed 2/2 + one +2/+2 = 4/4 (until end of turn).
	if der := e.Derived(id); der.Power != 4 || der.Toughness != 4 {
		t.Fatalf("after the turn-up trigger Master of Pearls is %d/%d, want 4/4 (printed 2/2 with one +2/+2 pump)", der.Power, der.Toughness)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the turn-up = %d, want 0 (the {3}{W}{W} morph cost was paid)", got)
	}
	replayCheck(t, e, cfg)
}

func morphDownCastWithoutPrintedOption(t *testing.T, e *Engine, name, symbols string) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, symbols)
	d := e.Pending()
	var idx = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "morphed" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no face-down cast option for %s: %+v", name, d)
	}
	submitChoices(t, e, idx)
	for d := e.Pending(); d != nil && d.Kind == decision.KModes; d = e.Pending() {
		if len(d.Options) == 0 {
			t.Fatalf("empty replacement mode choice: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		t.Fatalf("precondition: %s did not resolve face down: %+v", name, o)
	}
	return id
}

// TestVesuvanShapeshifterCanDeclineItsTurnFaceUpReplacement pins Optional$:
// declining the compiled replacement still completes the turn-up without
// cloning, and the answer is replayable.
func TestVesuvanShapeshifterCanDeclineItsTurnFaceUpReplacement(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Vesuvan Shapeshifter", "Grizzly Bears")
	id := morphDownCastWithoutPrintedOption(t, e, "Vesuvan Shapeshifter", "CCCUU")
	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || d.Options[1].Kind != "decline" {
		t.Fatalf("expected the optional replacement's yes/no choice, got %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard || o.FaceDown {
		t.Fatalf("declining the optional replacement should turn up the 0/0 Vesuvan before SBA death: %+v", o)
	}
	assertTurnUpEventOnce(t, e, id, mark)
	replayCheck(t, e, cfg)
}

// TestVesuvanShapeshifterCanAcceptItsTurnFaceUpReplacement pins the yes arm
// of Optional$: the compiled Clone body resolves before the turn-up folds.
func TestVesuvanShapeshifterCanAcceptItsTurnFaceUpReplacement(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Vesuvan Shapeshifter", "Grizzly Bears")
	bear := oppBear(t, e)
	id := morphDownCastWithoutPrintedOption(t, e, "Vesuvan Shapeshifter", "CCCUU")
	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || d.Options[0].Kind != "apply" {
		t.Fatalf("expected the optional replacement's yes/no choice, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil {
		t.Fatal("accepting the optional Clone replacement did not ask which creature to copy")
	}
	copyChoice := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			copyChoice = o.Index
			break
		}
	}
	if copyChoice < 0 {
		t.Fatalf("Clone choice omitted the battlefield Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, e, copyChoice)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.FaceDown || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
		t.Fatalf("accepting the optional replacement should turn Vesuvan face up as Grizzly Bears: %+v", o)
	}
	assertTurnUpEventOnce(t, e, id, mark)
	replayCheck(t, e, cfg)
}

// TestTurnFaceUpReplacementAskParksTheTransition proves an asking ReplaceWith
// body cannot expose the turn-up while its answer is outstanding.
func TestTurnFaceUpReplacementAskParksTheTransition(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := onBoardCard(t, e, 0, lookup(t, reg, "Aquamorph Entity"))
	e.emit(events.Event{Kind: events.TurnFaceDown, Obj: id})
	if o := e.G.Obj(id); o == nil || !o.FaceDown {
		t.Fatalf("precondition: Aquamorph Entity did not turn face down: %+v", o)
	}
	mark := len(e.L.Events)
	e.emit(events.Event{Kind: events.TurnFaceUp, Obj: id})
	if d := e.Pending(); d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected Aquamorph's replacement choice, got %+v", d)
	}
	if o := e.G.Obj(id); o == nil || !o.FaceDown {
		t.Fatalf("precondition: transition folded before the replacement answer: %+v", o)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.TurnFaceUp && ev.Obj == id {
			t.Fatal("TurnFaceUp event was logged while replacement choice was pending")
		}
	}
	d := e.Pending()
	if len(d.Options) == 0 {
		t.Fatalf("replacement choice has no options: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.FaceDown {
		t.Fatalf("answered replacement did not complete turn-up: %+v", o)
	}
	assertTurnUpEventOnce(t, e, id, mark)
}

// TestTurnFaceUpStaleOptionRechecksCantHappen pins the submitted-option guard
// separately from the offer gate: an offered option becomes stale once the
// prohibition appears, even if the player still submits the old option.
func TestTurnFaceUpStaleOptionRechecksCantHappen(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	forest := searchCorpusCard(t, reg, "Forest")
	fill := func(name string) []*cards.Card {
		deck := []*cards.Card{searchCorpusCard(t, reg, name)}
		for len(deck) < 40 {
			deck = append(deck, forest)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 7702, Names: []string{"karlov", "opponent"},
		Decks: [][]*cards.Card{fill("Karlov Watchdog"), fill("Kin-Tree Warden")}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	warden := castFromHandByName(t, e, "Karlov Watchdog", "CCCWWW")
	if o := e.G.Obj(warden); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Karlov Watchdog is not on battlefield: %+v", o)
	}
	driveToStep(t, e, 2, 1, state.StepMain1)
	probe := morphDownCastSeat(t, e, 1, "Kin-Tree Warden", "morphed", "CCCG", 1)
	opt := decision.Option{Kind: "turn_face_up", Obj: probe}
	if got := e.priorityOptionStale(1, opt); got != "" {
		t.Fatalf("precondition: actual morph turn-up option was already stale: %s", got)
	}
	driveToStepAny(t, e, 3, 0, state.StepMain1)
	if got := e.priorityOptionStale(1, opt); got == "" {
		t.Fatal("stale turn_face_up option accepted after Karlov Watchdog's CantHappen became active")
	}
}

// TestTurnFaceUpIsRegistered pins the RegisterNonAPI registration: the
// coverage report (cmd/forgec) reads effects.Supported(), so a revert of the
// trigger_match.go registration line would silently drop trig:TurnFaceUp from
// the measured corpus coverage — not just the census.
func TestTurnFaceUpIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:TurnFaceUp"] {
		t.Fatal(`effects.Supported() lacks "trig:TurnFaceUp" — the trigger_match.go registration was reverted`)
	}
	if !effects.Supported()["repl:TurnFaceUp"] {
		t.Fatal(`effects.Supported() lacks "repl:TurnFaceUp" — the replacement.go registration was reverted`)
	}
}

// TestTurnFaceUpCantHappenStopsTheRawEmitRoute is the defence-in-depth arm:
// the morph special action never even OFFERS a turn-up a live CantHappen
// prohibition blocks (the offer gate in rules/legal.go), but the
// effect-driven route (AB$ SetState | Mode$ TurnFaceUp — Woolly Loxodon and
// its 22 corpus siblings) emits events.TurnFaceUp without ever consulting
// that offer. There the dispatch's CantHappen arm is the only thing that
// stops the flip: Karlov Watchdog's prohibition, live on seat 0's turn, must
// prevent a raw-emitted turn-up of seat 1's face-down permanent — the
// FaceDown marker survives and a Note records the prevention.
func TestTurnFaceUpCantHappenStopsTheRawEmitRoute(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// onBoardCard is eventless direct placement (the panoptic_projektor_test
	// precedent), so this scenario has no replayable genesis — the raw-emit
	// prevention is what is being pinned here, not a replay chain.
	e := corpusEngine(t, reg, nil, nil)
	karlov := onBoardCard(t, e, 0, lookup(t, reg, "Karlov Watchdog"))
	probe := onBoardCard(t, e, 1, lookup(t, reg, "Kin-Tree Warden"))
	if o := e.G.Obj(karlov); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Karlov Watchdog is not on the battlefield (obj=%+v)", o)
	}
	if o := e.G.Obj(probe); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the probe is not on the battlefield (obj=%+v)", o)
	}
	e.emit(events.Event{Kind: events.TurnFaceDown, Obj: probe})
	if o := e.G.Obj(probe); !o.FaceDown {
		t.Fatalf("precondition: the TurnFaceDown event did not put the probe face down")
	}
	e.emit(events.Event{Kind: events.TurnFaceUp, Obj: probe})
	if !e.G.Obj(probe).FaceDown {
		t.Fatal("the raw-emitted turn-up went through: the CantHappen dispatch arm must prevent it (CR 614.1a) — only the offer gate stood between the rules")
	}
	// The prevention is silent: ONE matching replacement needs no CR 616.1
	// order ask. The generic fall-through would park the event on a
	// KReplacement order choice the controller never owes (repl:TurnFaceUp
	// registration reverted or the dispatch arm removed), so a replacement
	// ask here is the regression signal too.
	if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
		t.Fatalf("the CantHappen prevention parked the turn-up on a CR 616.1 replacement-order ask (%+v) — one matching replacement prevents silently", d)
	}
	answerQuiet(t, e, 60)
	if !e.G.Obj(probe).FaceDown {
		t.Fatal("the probe turned up after the drain — the turn-up was never prevented")
	}
}
