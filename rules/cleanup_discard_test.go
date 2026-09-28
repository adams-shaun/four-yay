package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file is the CR 514.1 regression spec (Task D1): the cleanup step
// that used to do nothing about an oversized hand must now ask the active
// player to discard down to maxHandSize, and must handle the exactly-at-the-
// limit and non-active cases. The regression tests were written before the
// implementation; each one names the failure it would catch.

// onHand places a card straight into a player's hand, appended after their
// existing hand in hand-zone order, the same direct-setup stance as onBoard
// for the battlefield. A test that needs a player mid-turn to hold more than
// maxHandSize adds a few of these on top of the opening hand.
func onHand(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	return o.ID
}

// answerIfDiscard answers a pending cleanup KChoose "discard" decision with
// the naive first-Max options (the same policy botpolicy uses, findings ck/cl),
// returning true. It returns false when nothing discard-shaped is pending, so
// a game-driving loop that normally only answers priority (passAll,
// driveToStep, driveTriggerGame) can call it at the top and keep driving
// across cleanup steps -- the discard is a real decision every seat must
// answer now that CR 514.1 is implemented, but it is not a priority decision.
func answerIfDiscard(t *testing.T, e *Engine) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "discard" {
		return false
	}
	e.submitDiscardChoice(t, d)
	return true
}

// submitDiscardChoice submits the first d.Max discard options, the naive
// shared with botpolicy; split out so it is usable even when the caller does
// not want the boolean wrapper of answerIfDiscard.
func (e *Engine) submitDiscardChoice(t *testing.T, d *decision.Decision) {
	t.Helper()
	choices := make([]int, 0, d.Max)
	for i := 0; i < len(d.Options) && i < d.Max; i++ {
		choices = append(choices, d.Options[i].Index)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit discard: %v", err)
	}
}

// discardIntent answers the pending cleanup discard decision with exactly
// the given hand-card indices (matched by object identity, so a test can
// name non-contiguous cards) and returns the pending decision for assertion.
func submitDiscard(t *testing.T, e *Engine, wantIDs ...state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("expected a pending discard decision, got none")
	}
	if d.Kind != decision.KChoose {
		t.Fatalf("expected a KChoose discard decision, got kind %q", d.Kind)
	}
	var choices []int
	for _, id := range wantIDs {
		found := -1
		for _, o := range d.Options {
			if o.Obj == id {
				found = o.Index
				break
			}
		}
		if found < 0 {
			t.Fatalf("discard option for object %d not offered", id)
		}
		choices = append(choices, found)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit discard: %v", err)
	}
	return d
}

// Test3141DiscardDownToSevenIsAsked runs the primary CR 514.1 path: the
// active player ending their turn holding nine cards is asked a KChoose
// "discard" decision over exactly their nine hand cards with Min == Max == 2,
// and after answering ends at seven in hand and two more in the graveyard.
func Test3141DiscardDownToSevenIsAsked(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	// Opening hand is 7; add two more so the hand is 9 at cleanup.
	a := onHand(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b := onHand(t, e, 0, "Name:Wolf\nManaCost:2 G\nTypes:Creature Wolf\nPT:3/2\nOracle:x\n")

	e.priorityRound()

	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 9 {
		t.Fatalf("hand size at cleanup = %d, want 9", len(hand))
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("cleanup step asked no discard decision for a 9-card hand")
	}
	if d.Kind != decision.KChoose {
		t.Fatalf("pending decision kind = %q, want choose", d.Kind)
	}
	if d.Player != 0 {
		t.Fatalf("discard decision for player %d, want active seat 0", d.Player)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("discard Min==Max==%d/%d, want 2/2 (= 9 cards - 7)", d.Min, d.Max)
	}
	if len(d.Options) != 9 {
		t.Fatalf("discard offered %d options, want 9 (one per hand card)", len(d.Options))
	}
	// Options are one per hand card, in hand-zone order, all Kind "discard".
	var optionObjs []state.ObjID
	for i, o := range d.Options {
		if o.Kind != "discard" {
			t.Fatalf("option %d kind = %q, want discard", i, o.Kind)
		}
		optionObjs = append(optionObjs, o.Obj)
	}
	if !reflect.DeepEqual(optionObjs, hand) {
		t.Fatalf("discard options order = %v, want hand order %v", optionObjs, hand)
	}

	// Answer with the two most-recently-drawn (the last two hand cards).
	submitDiscard(t, e, hand[7], hand[8])

	if got := len(e.G.Zone(state.ZHand, 0)); got != 7 {
		t.Fatalf("hand after discard = %d, want 7", got)
	}
	gy := e.G.Zone(state.ZGraveyard, 0)
	if len(gy) != 2 {
		t.Fatalf("graveyard after discard = %d, want 2 (the two discarded cards)", len(gy))
	}
	// The two cards we chose are the ones that moved, by identity.
	if !(gy[0] == hand[7] && gy[1] == hand[8]) && !(gy[0] == hand[8] && gy[1] == hand[7]) {
		t.Fatalf("graveyard = %v, want exactly the two discarded hand cards %v and %v", gy, hand[7], hand[8])
	}
	// The two onHand-dealt extras are hand[7] and hand[8] -- exactly what we
	// chose to discard -- so the identity check above doubles as the proof
	// that choosing mid-hand cards (not the opening seven) works.
	if a != hand[7] || b != hand[8] {
		t.Fatalf("test setup drift: extras a=%d b=%d but hand[7]=%d hand[8]=%d", a, b, hand[7], hand[8])
	}
}

// Test3141ExactlyMaxHandSizeAsksNothing pins decision rule 5: a player
// ending their turn holding EXACTLY seven cards is asked nothing at all --
// no decision is pending, not merely that the hand is unchanged. Before the
// fix the cleanup step never discriminated on hand size (and asked nothing
// regardless); this test guards the exactly-at-the-limit boundary where a
// buggy "<=" instead of ">" would quietly pose a zero-count decision.
func Test3141ExactlyMaxHandSizeAsksNothing(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	if got := len(e.G.Zone(state.ZHand, 0)); got != 7 {
		t.Fatalf("opening hand = %d, want 7 for this test", got)
	}
	e.priorityRound()
	if e.Pending() != nil {
		t.Fatalf("cleanup asked a decision for an exactly-7-card hand (a zero-count ask must never reach a seat): %+v", e.Pending())
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 7 {
		t.Fatalf("hand changed without a discard decision: %d, want 7", got)
	}
}

// Test3141OnlyActivePlayerDiscards pins decision rule 3 (CR 514.1): the
// non-active player holding far more than seven cards is NOT asked during
// the active player's cleanup step. Only the active player's hand is subject
// to the discard.
func Test3141OnlyActivePlayerDiscards(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	// Seat 1 (non-active) ends their turn with 17 cards -- opening 7 + 10.
	for i := 0; i < 10; i++ {
		onHand(t, e, 1, "Name:Moor\nManaCost:1 G\nTypes:Land\nOracle:x\n")
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 17 {
		t.Fatalf("non-active hand = %d, want 17 for this test", got)
	}
	e.priorityRound()
	if e.Pending() != nil {
		t.Fatalf("active player's cleanup asked a discard for the non-active player: %+v", e.Pending())
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 17 {
		t.Fatalf("non-active hand changed during active's cleanup: %d, want 17", got)
	}
}

// Test3141DiscardedCardsAreTheIntentNamed pins decision rule 4: the cards
// that actually move are the ones the intent named, by identity, even when
// the answer names indices that are NOT the first N. (The bot may take the
// first N, but the engine must honour whatever valid indices a client sends.)
func Test3141DiscardedCardsAreTheIntentNamed(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	// Build four extra cards so the hand is 11; discard three that are NOT
	// the first three (the middle three of the extras, i.e. indices 7,8,9).
	extras := make([]state.ObjID, 0, 4)
	for i := 0; i < 4; i++ {
		extras = append(extras, onHand(t, e, 0, "Name:Beast\nManaCost:2 G\nTypes:Creature Beast\nPT:4/4\nOracle:x\n"))
	}
	e.priorityRound()
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 11 {
		t.Fatalf("hand = %d, want 11", len(hand))
	}
	d := e.Pending()
	if d == nil || d.Min != 4 || d.Max != 4 {
		t.Fatalf("expected a discard-4 decision, got %+v", d)
	}
	// Choose cards at hand indices 7,8,9,10 (NOT the first N = 0,1,2,3).
	want := []state.ObjID{hand[7], hand[8], hand[9], hand[10]}
	submitDiscard(t, e, want...)

	gy := e.G.Zone(state.ZGraveyard, 0)
	if len(gy) != 4 {
		t.Fatalf("graveyard = %d, want exactly the 4 named cards", len(gy))
	}
	gySet := map[state.ObjID]bool{}
	for _, id := range gy {
		gySet[id] = true
	}
	for _, id := range want {
		if !gySet[id] {
			t.Fatalf("named discard %d is not in the graveyard %v", id, gy)
		}
	}
	if gySet[hand[0]] {
		t.Fatalf("an un-named first card %d was discarded (engine must honour the intent, not grab the first N)", hand[0])
	}
}

// Test3141ChoosingIsClearedByDiscardCleanup pins that answering the cleanup
// discard does NOT leave e.choosing = chooseCleanup behind (Task D1 finding
// 1). cleanupStep sets e.choosing before it asks and discardCleanup must set
// it back to chooseNone, the invariant the cast flows maintain explicitly
// (cast.go: e.cast, e.choosing = nil, chooseNone at the end of their flow).
// A stale chooseCleanup sitting in the engine would turn handleChoose's
// no-flow default -- which is supposed to catch a KChoose answered with no
// flow waiting and emit a harmless Note -- into a second, destructive
// discardCleanup run (it would move the cards hand -> graveyard, run the CR
// 514.2 body, and advance the step, i.e. a defensive fallback became a
// destructive one).
func Test3141ChoosingIsClearedByDiscardCleanup(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	// Two extra cards -> hand of 9 at cleanup, so a discard IS asked.
	onHand(t, e, 0, "Name:Axe\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	onHand(t, e, 0, "Name:Mace\nManaCost:1\nTypes:Artifact\nOracle:x\n")

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "discard" {
		t.Fatalf("expected a cleanup discard decision, got %+v", d)
	}
	// While the discard is pending the choosing marker must be chooseCleanup.
	if e.choosing != chooseCleanup {
		t.Fatalf("choosing during pending discard = %v, want chooseCleanup", e.choosing)
	}
	// Answer it, then assert the marker is cleared -- the fix under test.
	hand := e.G.Zone(state.ZHand, 0)
	submitDiscard(t, e, hand[len(hand)-2], hand[len(hand)-1])
	if e.choosing != chooseNone {
		t.Fatalf("choosing after the discard answer = %v, want chooseNone (a stale chooseCleanup would make a later no-flow choose hit the destructive discardCleanup instead of the Note fallback)", e.choosing)
	}

	// Prove the downstream consequence behaviourally: a KChoose answered with
	// no flow waiting must still take handleChoose's Note fallback, exactly
	// as it did before CR 514.1 existed. We hand-build the pending decision
	// the way handleChoose's default-arm comment describes ("only reachable
	// from a hand-built decision"), naming a real remaining hand card as the
	// discard option. With a correct reset this hits the default (a Note is
	// emitted and the hand is untouched); the pre-fix stale chooseCleanup
	// instead routes it into discardCleanup, which would move that card out
	// of the hand, re-run the CR 514.2 body and advance the step -- so the
	// hand check below fails.
	keep := e.G.Zone(state.ZHand, 0)
	if len(keep) == 0 {
		t.Fatal("no hand cards left to protect from a no-flow choose")
	}
	stray := &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 0, Max: 1,
		Options: []decision.Option{{Kind: "discard", Label: "Discard the kept card", Obj: keep[0], Player: 0}}}
	e.pending = stray
	if err := e.Submit(decision.Intent{Seq: 0, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit no-flow choose: %v", err)
	}
	var noted bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			noted = true
		}
	}
	if !noted {
		t.Fatal("a no-flow choose answer did not take the Note fallback")
	}
	handAfter := e.G.Zone(state.ZHand, 0)
	if len(handAfter) != len(keep) {
		t.Fatalf("a no-flow choose changed the hand (%d -> %d): it was routed into discardCleanup by a stale choosing marker", len(keep), len(handAfter))
	}
	for _, id := range keep {
		if len(e.G.Zone(state.ZGraveyard, 0)) == 1 && e.G.Zone(state.ZGraveyard, 0)[0] == id {
			t.Fatalf("no-flow choose discarded a kept hand card %d into the graveyard (the stale choosing marker routed into discardCleanup); remaining hand %v", id, handAfter)
		}
	}
}

// Test3143SecondCleanupStep names the CR 514.3 gap (clearly and honestly) for
// the follow-up, per the task brief: when the cleanup step's own actions --
// the 514.1 discard or the 514.2 removal -- leave a triggered ability waiting
// to be put onto the stack, the rules repeat the cleanup step after the
// active player gets priority instead of letting the turn hand over. The
// engine today advances to the next player's turn; the waiting trigger is
// then placed at that player's first priority round, one step late and in the
// wrong turn's sequence. Implementing the repeated-step control flow in the
// no-priority turn loop was judged not a small, clearly-correct addition at
// this point of priorityRound, so this test records the gap rather than
// papering over it. Skip until the turn loop grows the repeat; the skip
// carries the CR reference so the next pass finds it.
func Test3143SecondCleanupStep(t *testing.T) {
	t.Parallel()
	t.Skip("CR 514.3: a trigger created by cleanup (a discard or 514.2 action) is placed at the NEXT player's first priority instead of in a repeated cleanup step. Follow-up: make priorityRound re-run cleanup after granting priority when a cleanup action left pendingTriggers non-empty or performed a state-based action.")
}
