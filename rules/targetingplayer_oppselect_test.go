package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Tests for the TargetingPlayer$ Opponent controller-selection ask added by
// agent-20260925T085158Z-c861188d. See the file-level comments for the full
// rationale.

// oppSelectBoard deals a `seats`-seat table (seat 0 is the controller) whose
// deck opens with the named corpus carrier plus one Grizzly Bears and basics;
// every other seat's deck holds one Grizzly Bears and Mountains.
func oppSelectBoard(t *testing.T, reg *cards.Registry, seats int, carrier string, extras ...*cards.Card) (*Engine, Config) {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck0 := []*cards.Card{searchCorpusCard(t, reg, carrier), bear}
	deck0 = append(deck0, extras...)
	for i := 0; i < 8; i++ {
		deck0 = append(deck0, forest, mountain)
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, bear)
	}
	decks := [][]*cards.Card{deck0}
	names := []string{"caster"}
	for i := 1; i < seats; i++ {
		d := []*cards.Card{bear}
		d = append(d, extras...)
		for len(d) < 40 {
			d = append(d, mountain)
		}
		decks = append(decks, d)
		names = append(names, "opponent-"+strconv.Itoa(i))
	}
	cfg := seatZeroStart(Config{Seed: 7731, Names: names, Decks: decks, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// oppPickOptions asserts the pending decision is the controller's selection
// ask (ResumeKind "opp_pick") and returns the option indexes of the named
// seats.
func oppPickOptions(t *testing.T, e *Engine, controller state.PlayerID, wantSeats ...state.PlayerID) []int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending -- the selection ask was never posed")
	}
	if d.Kind != decision.KChoose || d.ResumeKind != "opp_pick" {
		t.Fatalf("pending ask = %+v, want the TargetingPlayer$ Opponent selection ask", d)
	}
	if d.Player != controller {
		t.Fatalf("selection ask posed to seat %d, want the controller seat %d", d.Player, controller)
	}
	idx := make([]int, 0, len(wantSeats))
	for _, want := range wantSeats {
		found := false
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == want {
				idx = append(idx, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("selection ask for seat %d missing: %+v", want, d.Options)
		}
	}
	return idx
}

// TestThreeSeatOpponentSelectionPreacher drives the three-seat Preacher
// activation flow: seat 0's which-opponent selection ask is posed FIRST, the
// controller answers seat 2, and ONLY seat 2 receives the target ask.
func TestThreeSeatOpponentSelectionPreacher(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	carrier := mustCorpusCard(t, reg, "Preacher")
	if sa := saWithTargetingPlayer(carrier, "AB", "Player.Opponent"); sa == nil {
		t.Fatal("Preacher fixture premise broken")
	}
	e, cfg := oppSelectBoard(t, reg, 3, "Preacher")
	preacher := searchMoveByNameSeat(t, e, 0, "Preacher", state.ZBattlefield)
	passToNextOwnTurn(t, e)
	own := searchMoveByNameSeat(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bear1 := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	bear2 := searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)

	po := e.G.Obj(preacher)
	if po == nil || po.Zone != state.ZBattlefield || po.Tapped {
		t.Fatalf("Preacher precondition failed: %+v", po)
	}
	if own == bear1 || bear1 == bear2 || own == bear2 {
		t.Fatalf("fixture target candidates are not distinct: %d/%d/%d", own, bear1, bear2)
	}
	if e.G.Obj(own).Controller != 0 || e.G.Obj(bear1).Controller != 1 || e.G.Obj(bear2).Controller != 2 {
		t.Fatalf("candidate fixture controllers: %d/%d/%d",
			e.G.Obj(own).Controller, e.G.Obj(bear1).Controller, e.G.Obj(bear2).Controller)
	}
	alive := e.G.AliveFrom(0)
	if len(alive) != 3 {
		t.Fatalf("fixture wants 3 living seats, got %v", alive)
	}

	submitChoices(t, e, abilityOptionFor(t, e, preacher).Index)

	pick := e.Pending()
	idx := oppPickOptions(t, e, 0, 1, 2)
	if len(idx) != 2 {
		t.Fatalf("selection ask offered %d options, want 2", len(idx))
	}
	if err := pick.Validate(decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{len(pick.Options)}}); err == nil {
		t.Fatal("selection accepted an option index outside the offered set")
	}
	if err := e.Submit(decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{idx[1]}}); err != nil {
		t.Fatalf("submit the seat-2 selection: %v", err)
	}

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the selection, ask = %+v, want the target ask", d)
	}
	if d.Player != 2 {
		t.Fatalf("target ask posed to seat %d, want seat 2", d.Player)
	}
	idx2 := -1
	for _, o := range d.Options {
		if o.Obj == bear2 {
			idx2 = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx2}}); err != nil {
		t.Fatalf("submit the selected opponent's target choice: %v", err)
	}
	if o := e.G.Obj(preacher); o == nil || !o.Tapped {
		t.Fatalf("Preacher was not tapped by the activation: %+v", o)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bear2).Controller; got != 0 {
		t.Fatalf("chosen bear controller = %d, want seat 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestThreeSeatOpponentSelectionTriggerPlacement drives the actual pending
// trigger drain through TriggerPush and verifies the controller selects the
// answerer before the triggered ability's placement target ask.
func TestThreeSeatOpponentSelectionTriggerPlacement(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card := mustCorpusCard(t, reg, "Karplusan Minotaur")
	triggerIndex := -1
	for i, tr := range card.Faces[0].Triggers {
		if tr.Effect != nil && tr.Effect.Params["TargetingPlayer"] == "Opponent" {
			triggerIndex = i
		}
	}
	if triggerIndex < 0 {
		t.Fatal("Karplusan Minotaur lacks its Opponent-targeting trigger body")
	}
	e, cfg := oppSelectBoard(t, reg, 3, "Karplusan Minotaur")
	source := searchMoveByNameSeat(t, e, 0, "Karplusan Minotaur", state.ZBattlefield)
	searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{Source: source, Controller: 0,
		Idx: triggerIndex, SA: card.Faces[0].Triggers[triggerIndex].Effect})
	if !e.putTriggersOnStack() {
		t.Fatal("pending trigger unexpectedly drained without a target selection ask")
	}
	pick := e.Pending()
	idx := oppPickOptions(t, e, 0, 1, 2)
	if len(idx) != 2 {
		t.Fatalf("selection ask offered %d opponents, want 2", len(idx))
	}
	if err := e.Submit(decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{idx[1]}}); err != nil {
		t.Fatalf("select seat 2 for triggered target ask: %v", err)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Player != 2 {
		t.Fatalf("trigger target ask = %+v, want selected seat 2", d)
	}
	if len(d.Options) == 0 {
		t.Fatal("trigger target ask has no legal target candidates")
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

// TestThreeSeatOpponentSelectionDeadSeatNoAsk: seat 1 left, so only ONE
// living opponent remains and the selection ask is never posed -- seat 2
// receives the target ask directly.
func TestThreeSeatOpponentSelectionDeadSeatNoAsk(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := oppSelectBoard(t, reg, 3, "Preacher")
	preacher := searchMoveByNameSeat(t, e, 0, "Preacher", state.ZBattlefield)
	passToNextOwnTurn(t, e)
	searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)
	if e.G.Players[1].Lost {
		t.Fatal("seat 1 already lost before the fixture marked it")
	}
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "conceded"})
	if len(e.G.AliveFrom(0)) != 2 {
		t.Fatalf("alive after seat 1 left = %v, want two seats", e.G.AliveFrom(0))
	}

	submitChoices(t, e, abilityOptionFor(t, e, preacher).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("ask = %+v, want the target ask posed directly", d)
	}
	if d.Player != 2 {
		t.Fatalf("chooser = %d, want the sole living opponent seat 2", d.Player)
	}
	if d.ResumeKind == "opp_pick" {
		t.Fatal("a selection ask was posed with only one living opponent")
	}
}

// TestThreeSeatOpponentSelectionEvangelize drives the real corpus SPELL
// Evangelize through the full cast flow on a three-seat table.
func TestThreeSeatOpponentSelectionEvangelize(t *testing.T) {
	reg := searchTestRegistry(t)
	evangelize := searchCorpusCard(t, reg, "Evangelize")
	if sa := saWithTargetingPlayer(evangelize, "SP", "Player.Opponent"); sa == nil {
		t.Fatal("Evangelize fixture premise broken")
	}
	e, cfg := oppSelectBoard(t, reg, 3, "Evangelize")
	evangelizeID := searchMoveByName(t, e, "Evangelize", state.ZHand)
	bear1 := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	bear2 := searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)
	own := searchMoveByNameSeat(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(own).Controller != 0 || e.G.Obj(bear1).Controller != 1 || e.G.Obj(bear2).Controller != 2 {
		t.Fatalf("candidate fixture controllers: %d/%d/%d",
			e.G.Obj(own).Controller, e.G.Obj(bear1).Controller, e.G.Obj(bear2).Controller)
	}
	addMana(t, e, 0, "WWWCC") // {4}{W}

	submitChoices(t, e, castCardOption(t, e, evangelizeID).Index)

	pick := e.Pending()
	idx := oppPickOptions(t, e, 0, 1, 2)
	if len(idx) != 2 {
		t.Fatalf("selection ask offered %d options, want 2", len(idx))
	}
	if err := e.Submit(decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{idx[1]}}); err != nil {
		t.Fatalf("submit the seat-2 selection: %v", err)
	}

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the selection, ask = %+v, want the Evangelize target ask", d)
	}
	if d.Player != 2 {
		t.Fatalf("target ask posed to seat %d, want the SELECTED opponent seat 2", d.Player)
	}
	for _, id := range []state.ObjID{own, bear1, bear2} {
		if !targetOptionContains(d.Options, id) {
			t.Fatalf("candidate %d missing from options %+v", id, d.Options)
		}
	}
	idx2 := -1
	for _, o := range d.Options {
		if o.Obj == bear2 {
			idx2 = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx2}}); err != nil {
		t.Fatalf("submit the selected opponent's target choice: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	after := e.G.Obj(bear2)
	if after == nil || after.Zone != state.ZBattlefield || after.Controller != 0 {
		t.Fatalf("after Evangelize resolved the bear is %+v, want under seat 0", after)
	}
	replayCheck(t, e, cfg)
}

// TestThreeSeatVolcanicOfferingMidResolutionSelection: Volcanic Offering's
// root SP$ Pump (no TargetingPlayer$) is answered by the CASTER, then the
// depth-2 DB$DestroyLand sub carries `TargetingPlayer$ Player.Opponent`, so
// the controller's selection ask fires before the sub's target ask.
func TestThreeSeatVolcanicOfferingMidResolutionSelection(t *testing.T) {
	reg := searchTestRegistry(t)
	offering := searchCorpusCard(t, reg, "Volcanic Offering")
	if !cardHasSubTargetingPlayer(offering, "Player.Opponent", "Land.nonBasic") {
		t.Fatal("Volcanic Offering fixture premise broken")
	}
	furnace := searchCorpusCard(t, reg, "Great Furnace")
	e, cfg := oppSelectBoard(t, reg, 3, "Volcanic Offering", furnace)
	offeringID := searchMoveByName(t, e, "Volcanic Offering", state.ZHand)
	furnaceID := searchMoveByNameSeat(t, e, 1, "Great Furnace", state.ZBattlefield)
	fo := e.G.Obj(furnaceID)
	if fo == nil || fo.Zone != state.ZBattlefield || fo.Controller != 1 {
		t.Fatalf("furnace precondition: %+v (want a nonbasic land under seat 1)", fo)
	}
	addMana(t, e, 0, "CCCCR") // {4}{R}
	submitChoices(t, e, castCardOption(t, e, offeringID).Index)

	// Root ask: the caster (seat 0) answers it.
	root := e.Pending()
	if root == nil || root.Kind != decision.KTarget || root.Player != 0 {
		t.Fatalf("root target ask = %+v, want the caster seat 0 answering the root", root)
	}
	rootIdx := -1
	for _, o := range root.Options {
		if o.Obj == furnaceID {
			rootIdx = o.Index
		}
	}
	if rootIdx < 0 {
		t.Fatalf("root ask did not offer seat 1's furnace %d: %+v", furnaceID, root.Options)
	}
	submitChoices(t, e, rootIdx)

	// The selection ask for DBDestroyLand, posed to the controller.
	pick := passPriorityUntilNonPriority(t, e)
	if pick.Kind != decision.KChoose || pick.ResumeKind != "opp_pick" {
		t.Fatalf("mid-resolution ask = %+v, want the which-opponent selection", pick)
	}
	if pick.Player != 0 {
		t.Fatalf("selection ask posed to seat %d, want the controller seat 0", pick.Player)
	}
	idx := -1
	for _, o := range pick.Options {
		if o.Kind == "player" && o.Player == 2 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("selection ask did not offer seat 2: %+v", pick.Options)
	}
	if err := e.Submit(decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit the seat-2 selection: %v", err)
	}

	// The sub's target ask must now be posed to the SELECTED opponent.
	ask := passPriorityUntilNonPriority(t, e)
	if ask.Kind != decision.KChoose || ask.ResumeKind != "tgts" {
		t.Fatalf("mid-resolution ask = %+v, want the mvts1 \"tgts\" KChoose for DBDestroyLand", ask)
	}
	if ask.Player != 2 {
		t.Fatalf("mid-resolution sub target ask posed to seat %d, want the SELECTED opponent seat 2", ask.Player)
	}
	offered := false
	for _, o := range ask.Options {
		if o.Obj == furnaceID {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("sub ask did not offer the controller-relative legal target %d: %+v", furnaceID, ask.Options)
	}
	replayCheck(t, e, cfg)
}

// TestOpponentPickerResolverContract pins the shared resolver: the sole
// living opponent answers directly; a controller with no living opponent
// fails closed (no stuck decision).
func TestOpponentPickerResolverContract(t *testing.T) {
	e, _ := combatTriggerBoard(t, testutil.CorpusRegistry(t), nil, nil, nil, nil)
	const controller state.PlayerID = 0
	if got := len(e.G.AliveFrom(0)); got != 2 {
		t.Fatalf("fixture wants 2 living seats, got %d", got)
	}
	if who, ok, pick := e.opponentPicker(controller); !ok || pick || who != 1 {
		t.Fatalf("sole-opponent = (%d, %v, %v), want (1, true, false)", who, ok, pick)
	}
	e.G.Players[1].Lost = true
	if who, ok, pick := e.opponentPicker(controller); ok || pick || who != controller {
		t.Fatalf("no-opponent = (%d, %v, %v), want (%d, false, false)", who, ok, pick, controller)
	}
}

// TestOpponentPickBotAnswerIsValidated pins the one legal-answer rule: the
// bot's KChoose default arm (the clamp fallback, which answers option 0)
// submits an answer the validator accepts, because every offered option is a
// living opponent.
func TestOpponentPickBotAnswerIsValidated(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := oppSelectBoard(t, reg, 3, "Preacher")
	preacher := searchMoveByNameSeat(t, e, 0, "Preacher", state.ZBattlefield)
	passToNextOwnTurn(t, e)
	searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)
	submitChoices(t, e, abilityOptionFor(t, e, preacher).Index)
	pick := e.Pending()
	if pick == nil || pick.ResumeKind != "opp_pick" {
		t.Fatalf("pending = %+v, want the selection ask", pick)
	}
	in := decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{pick.Options[0].Index}}
	if err := pick.Validate(in); err != nil {
		t.Fatalf("bot's own answer rejected: %v", err)
	}
	chosenSeat := pick.Options[0].Player
	if chosenSeat == pick.Player {
		t.Fatal("option 0 names the controller itself, not an opponent")
	}
	if e.G.Players[chosenSeat].Lost {
		t.Fatalf("option 0 names dead seat %d", chosenSeat)
	}
}
