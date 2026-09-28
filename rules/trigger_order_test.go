package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task 27's card fixtures. Gainer and Drainer are deliberately NOT
// commutative: Drainer loses life equal to the current life total, so which
// of the two resolves first is visible in the final life total and not merely
// in the order two log lines appear.
const gainerSrc = `Name:Gainer
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigGain | TriggerDescription$ gain 5 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 5 | Defined$ You
Oracle:x
`

const drainerSrc = `Name:Drainer
ManaCost:B
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigDrain | TriggerDescription$ lose life equal to your life total
SVar:TrigDrain:DB$ LoseLife | LifeAmount$ X | Defined$ You
SVar:X:Count$YourLifeTotal
Oracle:x
`

// wardenSrc is the corpus's TriggeredCardController shape: the trigger's
// controller and its decider are different seats.
const wardenSrc = `Name:Warden
ManaCost:2 W
Types:Enchantment
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature | OptionalDecider$ TriggeredCardController | Execute$ TrigGain | TriggerDescription$ its controller may have you gain 2 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 2 | Defined$ You
Oracle:x
`

const mournerSrc = `Name:Mourner
ManaCost:B
Types:Enchantment
T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | Execute$ TrigMourn | TriggerDescription$ gain 8 life
SVar:TrigMourn:DB$ GainLife | LifeAmount$ 8 | Defined$ You
Oracle:x
`

const mayGainSrc = `Name:Almsgiver
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | OptionalDecider$ You | Execute$ TrigGain | TriggerDescription$ you may gain 4 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 4 | Defined$ You
Oracle:x
`

// chooserSrc is a MODAL upkeep trigger (CR 603.3c): its effect is a Charm
// whose Choices$ sub-abilities the controller announces when the ability is
// put on the stack, not at resolution. It stands in for the optional trigger
// in TestTriggerArrivingDuringADecisionCannotCorruptTheQueue — under CR 603.5
// an OptionalDecider$ trigger no longer interrupts the drain at placement, but
// a modal one still does via the mode ask.
const chooserSrc = `Name:Chooser
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigChoose | TriggerDescription$ choose one
SVar:TrigChoose:DB$ Charm | Choices$ DoGain,DoLose
SVar:DoGain:DB$ GainLife | LifeAmount$ 4 | Defined$ You
SVar:DoLose:DB$ LoseLife | LifeAmount$ 4 | Defined$ You
Oracle:x
`

// submit answers the pending decision and fails the test if the engine
// rejects it.
func submit(t *testing.T, e *Engine, choices ...int) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit %s %v: %v", d.Kind, choices, err)
	}
}

// upkeepEngine puts each src on player 0's battlefield and fires an upkeep,
// leaving whatever those triggers matched in e.pendingTriggers.
func upkeepEngine(t *testing.T, srcs ...string) (*Engine, []state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	ids := make([]state.ObjID, 0, len(srcs))
	for _, src := range srcs {
		ids = append(ids, onBoard(t, e, 0, src))
	}
	e.G.Active = 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	return e, ids
}

// TestOneTriggerIsNeverAskedAbout is definition-of-done item 2. A decision
// with a single legal answer is noise on the wire, and CR 603.3b gives the
// player a choice only when there is one to make.
func TestOneTriggerIsNeverAskedAbout(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, gainerSrc)
	if e.putTriggersOnStack() {
		t.Fatalf("a lone trigger asked a decision: %+v", e.Pending())
	}
	if e.Pending() != nil {
		t.Fatalf("a lone trigger left a decision pending: %+v", e.Pending())
	}
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != ids[0] {
		t.Fatalf("stack = %v, want the one trigger from %d", e.G.Stack, ids[0])
	}
}

// TestTwoSimultaneousTriggersAskTheirController is definition-of-done item 1
// and requirement R1, plus Ruling U2's wire-format claim: the ordering
// decision really is Min == Max == N over that controller's own N triggers,
// so Decision.Validate's existing rules already mean "a permutation".
func TestTwoSimultaneousTriggersAskTheirController(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, gainerSrc, drainerSrc)
	if !e.putTriggersOnStack() {
		t.Fatal("two simultaneous triggers did not ask their controller for an order")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want a trigger-order decision", d)
	}
	if d.Player != 0 {
		t.Fatalf("asked player %d, want the controller (0)", d.Player)
	}
	if d.Min != 2 || d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("min/max/options = %d/%d/%d, want 2/2/2", d.Min, d.Max, len(d.Options))
	}
	if d.Options[0].Obj != ids[0] || d.Options[1].Obj != ids[1] {
		t.Fatalf("options name %d,%d, want the two trigger sources %d,%d",
			d.Options[0].Obj, d.Options[1].Obj, ids[0], ids[1])
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want nothing placed before the player answered", e.G.Stack)
	}
	// The permutation semantics Ruling U2 leans on, asserted rather than
	// assumed: a repeated index and a short answer are both already rejected.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0, 0}}); err == nil {
		t.Error("Validate accepted a duplicate index, so this is not a permutation")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err == nil {
		t.Error("Validate accepted a short answer, so this is not a permutation")
	}
}

// TestTriggerOrderChoiceDecidesResolutionOrder is Ruling U2's direction
// assertion, and it deliberately asserts the order the two effects RUN rather
// than the order they were pushed -- getting the direction backwards is
// silent, and a push-order assertion would agree with either convention.
//
// Documented direction: choice[0] goes on the stack FIRST and therefore
// resolves LAST. Gainer (+5) and Drainer (lose your whole life total) are
// non-commutative from 20 life:
//
//	Gainer chosen first  -> Drainer resolves first -> 20-20 = 0, then +5 -> 5
//	Drainer chosen first -> Gainer resolves first  -> 20+5 = 25, then -25 -> 0
func TestTriggerOrderChoiceDecidesResolutionOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		choices  []int
		wantLife int32
		wantBot  int // which source ends up at Stack[0]
	}{
		{"gainer chosen first resolves last", []int{0, 1}, 5, 0},
		{"drainer chosen first resolves last", []int{1, 0}, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, ids := upkeepEngine(t, gainerSrc, drainerSrc)
			if !e.putTriggersOnStack() {
				t.Fatal("expected an ordering decision")
			}
			submit(t, e, tc.choices...)
			if len(e.G.Stack) != 2 {
				t.Fatalf("stack = %v, want both triggers placed", e.G.Stack)
			}
			// Push order: choice[0] is the bottom of the stack.
			if got := e.G.Obj(e.G.Stack[0]).Source; got != ids[tc.wantBot] {
				t.Errorf("stack bottom is from %d, want %d (choice[0] is pushed first)",
					got, ids[tc.wantBot])
			}
			e.resolveTop()
			e.resolveTop()
			if got := e.G.Players[0].Life; got != tc.wantLife {
				t.Fatalf("life = %d, want %d — the effects resolved in the wrong order",
					got, tc.wantLife)
			}
		})
	}
}

// TestOptionalTriggerNeedsAnExplicitYes is definition-of-done item 3 and
// requirement R2. CR 603.5 moved the choice to resolution: the ability goes
// on the stack regardless, and the decider answers yes/no as it resolves.
func TestOptionalTriggerNeedsAnExplicitYes(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, mayGainSrc)
	// No yes/no at placement any more: putTriggersOnStack pushes the optional
	// trigger unconditionally (CR 603.5), so it returns with nothing pending.
	if e.putTriggersOnStack() {
		t.Fatalf("an optional trigger asked at placement: %+v", e.Pending())
	}
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != ids[0] {
		t.Fatalf("stack = %v, want the optional trigger from %d on the stack", e.G.Stack, ids[0])
	}
	e.resolveTop() // the ability resolves; CR 603.5 poses the yes/no here
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("pending = %+v, want an optional-trigger resolution decision", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("decision = player %d min %d max %d %d options, want 0/1/1/2",
			d.Player, d.Min, d.Max, len(d.Options))
	}
	if d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("options = %q,%q, want yes,no in that order",
			d.Options[0].Kind, d.Options[1].Kind)
	}
	// The inverted pin: the trigger is ON the stack, sourced from ids[0],
	// while the question is outstanding (old build left the stack empty until
	// the yes).
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != ids[0] {
		t.Fatalf("stack = %v, want the trigger on the stack sourced from %d while it is asked",
			e.G.Stack, ids[0])
	}
	submit(t, e, 0) // yes
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the accepted trigger resolved off the stack", e.G.Stack)
	}
	if got := e.G.Players[0].Life; got != 24 {
		t.Fatalf("life = %d, want 24 — the accepted trigger did not actually resolve", got)
	}
}

// TestDecliningAnOptionalTriggerLeavesTheGameUntouched is definition-of-done
// item 4. Asserted as a whole-game diff rather than "the stack is empty",
// because Ruling T20-d exists precisely because stack-depth assertions missed
// real bugs: a "no" must emit nothing that changes state at all.
func TestDecliningAnOptionalTriggerLeavesTheGameUntouched(t *testing.T) {
	t.Parallel()
	e, _ := upkeepEngine(t, mayGainSrc)
	before := e.G.Clone()
	// CR 603.5: the ability goes on the stack regardless, so putTriggersOnStack
	// returns with the trigger placed and nothing pending.
	if e.putTriggersOnStack() {
		t.Fatalf("an optional trigger asked at placement: %+v", e.Pending())
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the optional trigger on the stack before the resolve", e.G.Stack)
	}
	e.resolveTop() // poses the CR 603.5 yes/no as the ability resolves
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("pending = %+v, want the resolution optional decision", e.Pending())
	}
	submit(t, e, 1) // no
	// CR 603.5: the declined ability still RESOLVES, and leaves the stack
	// having done nothing. The stack must be empty; if the decline left it on
	// the stack the diff below would mask that (the strip removes it), so this
	// explicit size pin is what keeps the came-and-gone premise honest.
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the declined ability to have left the stack", e.G.Stack)
	}
	after := e.G.Clone()
	// The declined ability was on the stack and departed to exile having done
	// nothing. Everything else must be byte-identical to before the trigger
	// fired (Ruling T20-d's whole-game diff, kept honest for CR 603.5): strip
	// exactly the ability's own came-and-gone footprint — one transient Objs
	// slot, one exile entry, one NextID increment — and diff the rest.
	stripTransientAbilities(before, after)
	if diff := diffGames(before, after); diff != "" {
		t.Fatalf("declining an optional trigger changed the game:\n%s", diff)
	}
	if len(e.pendingTriggers) != 0 || e.orderedTriggers != 0 {
		t.Fatalf("queue = %d entries / %d ordered, want the declined trigger discarded",
			len(e.pendingTriggers), e.orderedTriggers)
	}
	// The inverted pin (Ruling T20-d, re-authorized for CR 603.5): a declined
	// optional trigger DID go on the stack, so a TriggerPush is emitted even
	// though the effect never runs. The old build asserted the opposite.
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("a declined optional trigger emitted no TriggerPush — CR 603.5 puts it on the stack regardless")
	}
}

// TestOptionalDeciderCanBeSomeoneOtherThanTheController covers the corpus
// finding that OptionalDecider$ is not always "You": 55 of the 1496 T: lines
// that carry it name a different player, and TriggeredCardController (40 of
// them) is the commonest of those.
func TestOptionalDeciderCanBeSomeoneOtherThanTheController(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, wardenSrc)
	// A creature belonging to player 1 enters the battlefield, so the
	// triggering CARD's controller is player 1 while the trigger's own
	// controller is player 0.
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	bear.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{bear.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear.ID,
		From: state.ZHand, To: state.ZBattlefield})

	// CR 603.5 the ability goes on the stack regardless; the decider (the
	// TriggeredCardController, player 1) is asked as it resolves.
	if e.putTriggersOnStack() {
		t.Fatalf("an optional trigger asked at placement: %+v", e.Pending())
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the optional trigger on the stack", e.G.Stack)
	}
	e.resolveTop() // poses the resolution yes/no
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("pending = %+v, want an optional-trigger resolution decision", d)
	}
	if d.Player != 1 {
		t.Fatalf("asked player %d, want player 1 (TriggeredCardController), not the trigger's controller", d.Player)
	}
}

// TestTriggerArrivingDuringADecisionCannotCorruptTheQueue is Ruling U3's own
// hazard, built and measured rather than reasoned about. checkStateBased is a
// fixed-point loop that emits events, every emit runs checkTriggers, and
// Submit runs it AFTER handle -- so a creature dying to a state-based action
// really does append to e.pendingTriggers between an ask and its answer.
//
// The board: three simultaneous upkeep triggers for player 0 (the middle one
// in the chosen order MODAL, so the drain is interrupted at placement by its
// CR 603.3c mode ask with work still settled behind it) and a creature already
// carrying lethal damage, whose death fires a fourth trigger while that mode
// decision is pending.
//
// What must hold afterwards: nothing lost, nothing pushed twice, the settled
// order preserved across the interruption, the late arrival placed AFTER the
// settled group, and the player NOT asked to order the same triggers again.
func TestTriggerArrivingDuringADecisionCannotCorruptTheQueue(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	a := onBoard(t, e, 0, gainerSrc)
	b := onBoard(t, e, 0, chooserSrc)
	c := onBoard(t, e, 0, drainerSrc)
	mourner := onBoard(t, e, 0, mournerSrc)
	doomed := onBoard(t, e, 0, "Name:Doomed\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	e.G.Obj(doomed).Damage = 5 // lethal, but no state-based action has run yet
	e.G.Active = 0

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if got := len(e.pendingTriggers); got != 3 {
		t.Fatalf("queued %d triggers, want the three upkeep triggers", got)
	}
	if !e.putTriggersOnStack() {
		t.Fatal("expected an ordering decision for three simultaneous triggers")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder || len(d.Options) != 3 {
		t.Fatalf("pending = %+v, want a three-way ordering decision", d)
	}
	// Discovery order is Gainer, Chooser, Drainer. Choose Drainer, Chooser,
	// Gainer: the modal one lands in the middle, so after it is pushed (with
	// its mode ask outstanding) exactly one trigger stays settled behind it.
	submit(t, e, 2, 1, 0)

	// Submit ran handle (which pushed Drainer and Chooser and asked Chooser's
	// mode) and then checkStateBased, which is what killed the creature.
	if d := e.Pending(); d == nil || d.Kind != decision.KModes || d.ResumeKind != "modes" {
		t.Fatalf("pending = %+v, want the mode decision for the middle trigger", d)
	}
	if o := e.G.Obj(doomed); o.Zone != state.ZGraveyard {
		t.Fatalf("doomed creature is in %s, want the graveyard — no state-based action ran", o.Zone)
	}
	// CR 603.3c: a modal trigger is pushed (TriggerPush) before its mode ask,
	// so both Drainer and Chooser are on the stack; the drain is paused on the
	// mode question with Gainer still settled behind it.
	if len(e.G.Stack) != 2 || e.G.Obj(e.G.Stack[0]).Source != c || e.G.Obj(e.G.Stack[1]).Source != b {
		t.Fatalf("stack = %v, want Drainer (%d) then Chooser (%d) placed so far", e.G.Stack, c, b)
	}
	// The hazard itself: the queue grew underneath a half-finished drain. The
	// queue now holds the still-settled Gainer plus the newly arrived death
	// trigger (Chooser was already pushed out of it).
	if got := len(e.pendingTriggers); got != 2 {
		t.Fatalf("queue = %d, want 2 (Gainer settled, the death trigger newly arrived)", got)
	}
	if e.orderedTriggers != 1 {
		t.Fatalf("orderedTriggers = %d, want 1 — the settled prefix was not preserved", e.orderedTriggers)
	}
	if e.pendingTriggers[1].Source != mourner {
		t.Fatalf("late arrival landed at index %d's source %d, want it appended after the settled group",
			1, e.pendingTriggers[1].Source)
	}

	// Answer the mode (index 0 = DoGain, +4 life) to resume the drain.
	submit(t, e, 0)

	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want the interrupted priority round to have finished", d)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("queue = %d entries, want fully drained", len(e.pendingTriggers))
	}
	// Nothing lost, nothing duplicated, settled order preserved, late arrival
	// last: the whole of Ruling U3 in one assertion.
	want := []state.ObjID{c, b, a, mourner}
	if len(e.G.Stack) != len(want) {
		t.Fatalf("stack = %v, want four abilities", e.G.Stack)
	}
	for i, src := range want {
		if got := e.G.Obj(e.G.Stack[i]).Source; got != src {
			t.Fatalf("stack[%d] is from %d, want %d (stack = %v)", i, got, src, e.G.Stack)
		}
	}
	// And the effects, not just the depth (Ruling T20-d): resolving the whole
	// stack must run all four. Resolution is top-down: Mourner, Gainer,
	// Chooser (DoGain, +4), Drainer.
	// 20 -> +8 (Mourner) -> +5 (Gainer) -> +4 (Chooser) -> -37 (Drainer) = 0.
	for len(e.G.Stack) > 0 {
		e.resolveTop()
	}
	if got := e.G.Players[0].Life; got != 0 {
		t.Fatalf("life = %d, want 0 — the four abilities did not all resolve in stack order", got)
	}
}

// TestTriggersOfADepartedControllerAreDropped is CR 800.4a: an ability
// controlled by a player who has left the game ceases to exist. Before Task 27
// these sorted after every living seat and went on the stack anyway.
func TestTriggersOfADepartedControllerAreDropped(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	onBoard(t, e, 1, gainerSrc)
	onBoard(t, e, 1, drainerSrc)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if got := len(e.pendingTriggers); got != 2 {
		t.Fatalf("queued %d triggers, want 2", got)
	}
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "test"})
	if e.putTriggersOnStack() {
		t.Fatalf("asked a player who has left the game: %+v", e.Pending())
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want a departed player's triggers to have ceased to exist", e.G.Stack)
	}
}

// TestEliminationDuringATriggerDecisionDoesNotStrandTheEngine is the case the
// brief could not know about. With three seats an elimination does not end the
// game, so a decision pending against the eliminated player can never be
// answered and nothing else will ever run: Advance does nothing while
// e.pending is set, and only that player may Submit. Without
// releasePendingDecisionOfDepartedPlayer this test hangs the match forever.
func TestEliminationDuringATriggerDecisionDoesNotStrandTheEngine(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	onBoard(t, e, 1, gainerSrc)
	onBoard(t, e, 1, mayGainSrc)
	onBoard(t, e, 1, drainerSrc)
	e.G.Active = 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	// Drive player 1 to exactly zero life, but do not run state-based actions
	// yet: the elimination must happen inside the Submit below, between the
	// ask and the answer, which is the only window that exists.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -20})

	// newSeats already advanced into a priority decision; drop it so the
	// ordering decision below is the one pending.
	e.pending = nil
	if !e.putTriggersOnStack() {
		t.Fatal("expected an ordering decision for player 1")
	}
	if d := e.Pending(); d == nil || d.Player != 1 {
		t.Fatalf("pending = %+v, want an ordering decision for player 1", d)
	}
	submit(t, e, 0, 1, 2) // Gainer, then the optional one, then Drainer

	if e.G.Over {
		t.Fatal("a three-seat game ended on one elimination")
	}
	if !e.G.Players[1].Lost {
		t.Fatal("player 1 was not eliminated by the state-based actions Submit ran")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("the engine is stranded: no decision pending and nothing left to run it")
	}
	if e.G.Players[d.Player].Lost {
		t.Fatalf("pending decision %s is against player %d, who has left the game", d.Kind, d.Player)
	}
	if d.Kind != decision.KPriority {
		t.Fatalf("pending = %s, want the interrupted priority round to have finished", d.Kind)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("queue = %d entries, want the departed player's triggers dropped", len(e.pendingTriggers))
	}
	// The match must still be playable, not merely un-stranded.
	if n := passAll(t, e, 6); n == 0 {
		t.Fatal("the match could not take another action after the elimination")
	}
}

// Scholar and Grinder are the replay proof's non-commutative pair: one draws
// the top card of the library and the other mills it, so which resolves first
// decides which ObjID ends up in the hand and which in the graveyard. A replay
// that got the order wrong would be caught by diffGames as a zone difference,
// not merely as a different ability wrapper.
const scholarSrc = `Name:Scholar
ManaCost:U
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigDraw | TriggerDescription$ draw a card
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`

const grinderSrc = `Name:Grinder
ManaCost:B
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigMill | TriggerDescription$ mill a card
SVar:TrigMill:DB$ Mill | NumCards$ 1 | Defined$ You
Oracle:x
`

// findAndPlay moves the named card of p's onto the battlefield through
// ordinary logged events, so a log-only replay can follow it. Genesis's
// shuffle decides whether it landed in the hand or the library.
func findAndPlay(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZHand, To: state.ZBattlefield})
			return id
		}
	}
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZLibrary, To: state.ZHand})
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZHand, To: state.ZBattlefield})
			return id
		}
	}
	t.Fatalf("%s not found in player %d's hand or library", name, p)
	return 0
}

// driveTriggerGame plays the match forward, passing every priority, answering
// each ordering decision with the next permutation from orders (identity once
// they run out) and each optional decision with the next answer from yesNo
// (declining once they run out). Reports how many of each it answered.
func driveTriggerGame(t *testing.T, e *Engine, limit int, orders [][]int, yesNo []bool) (nOrder, nOpt int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		var choices []int
		switch d.Kind {
		case decision.KChoose:
			// CR 514.1 (Task D1): a cleanup discard. Answer it naively
			// (first-Max, the botpolicy policy) and continue -- it is not a
			// priority, ordering or optional decision.
			if len(d.Options) == 0 || d.Options[0].Kind != "discard" {
				t.Fatalf("unexpected KChoose option kind %q", d.Options[0].Kind)
			}
			for k := 0; k < len(d.Options) && k < d.Max; k++ {
				choices = append(choices, k)
			}
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
			choices = []int{idx}
		case decision.KTriggerOrder:
			if nOrder < len(orders) {
				choices = orders[nOrder]
				if len(choices) != len(d.Options) {
					t.Fatalf("scripted order %v does not fit %d options", choices, len(d.Options))
				}
			} else {
				for k := range d.Options {
					choices = append(choices, k)
				}
			}
			nOrder++
		case decision.KTriggerOptional:
			pick := 1 // no
			if nOpt < len(yesNo) && yesNo[nOpt] {
				pick = 0 // yes
			}
			nOpt++
			choices = []int{pick}
		default:
			t.Fatalf("unexpected decision kind %q", d.Kind)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("submit %s %v: %v", d.Kind, choices, err)
		}
	}
	return
}

// TestReplayFromLogAloneReconstructsOrderedAndOptionalTriggers is Ruling U4.
// A real multi-turn game that exercises both a reordering and an optional
// trigger (once accepted, once declined) is folded from its log ALONE into a
// fresh state.Game -- no rules.Engine behind it, exactly what a real
// replay-from-log does -- and must match the live game exactly.
//
// Both decisions reach replay with no new event kind and no new Event field:
// the ordering choice is carried purely by the ORDER of the TriggerPush
// events, and the optional choice purely by whether a TriggerPush was emitted
// at all. The two controls below are what prove the harness discriminates
// rather than trivially agreeing, and the second of them is aimed at exactly
// that claim: swap two TriggerPush events and the replay must diverge.
func TestReplayFromLogAloneReconstructsOrderedAndOptionalTriggers(t *testing.T) {
	t.Parallel()
	deck0 := append([]*cards.Card{card(t, scholarSrc), card(t, grinderSrc), card(t, mayGainSrc)},
		mountainDeck(t, 37)...)
	cfg := seatZeroStart(Config{Seed: 11, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, mountainDeck(t, 40)}})
	e := New(cfg)
	scholar := findAndPlay(t, e, 0, "Scholar")
	grinder := findAndPlay(t, e, 0, "Grinder")
	alms := findAndPlay(t, e, 0, "Almsgiver")
	e.Advance()

	// Discovery order is battlefield order: Scholar, Grinder, Almsgiver.
	// [2,0,1] puts the optional one first (so it is asked before anything is
	// pushed) and reverses the draw/mill pair relative to discovery order.
	nOrder, nOpt := driveTriggerGame(t, e, 400, [][]int{{2, 0, 1}}, []bool{true, false})
	if nOrder < 2 {
		t.Fatalf("answered %d ordering decisions, want at least 2", nOrder)
	}
	if nOpt < 2 {
		t.Fatalf("answered %d optional decisions, want at least 2 (one yes, one no)", nOpt)
	}

	// The choices really did reach the log, and the effects really landed.
	var pushes []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush {
			pushes = append(pushes, ev.Obj)
		}
	}
	if len(pushes) < 5 {
		t.Fatalf("TriggerPush sources = %v, want 3 from the accepted upkeep and 2 from the declined one", pushes)
	}
	if pushes[0] != alms || pushes[1] != scholar || pushes[2] != grinder {
		t.Fatalf("first three TriggerPush sources = %v, want %d,%d,%d — the chosen order did not reach the log",
			pushes[:3], alms, scholar, grinder)
	}
	if pushes[3] == alms || pushes[4] == alms {
		t.Fatalf("the declined optional trigger reached the stack: %v", pushes[3:5])
	}

	fresh := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, fresh); diff != "" {
		t.Fatalf("replay from the log alone diverged from the live game:\n%s", diff)
	}

	// Control 1: a truncated log must be detected as a divergence, or this
	// test could pass vacuously.
	truncated := replayFromLog(t, cfg, e.L.Events[:len(e.L.Events)-1])
	if diff := diffGames(e.G, truncated); diff == "" {
		t.Fatal("control: a truncated log should diverge, but diffGames found no difference")
	}

	// Control 2, aimed at this task's own claim: the ORDER of the TriggerPush
	// events is what carries the ordering choice. Swap the first two and the
	// replay must diverge.
	swapped := append([]events.Event(nil), e.L.Events...)
	var first, second = -1, -1
	for i, ev := range swapped {
		if ev.Kind != events.TriggerPush {
			continue
		}
		if first < 0 {
			first = i
		} else {
			second = i
			break
		}
	}
	if first < 0 || second < 0 {
		t.Fatal("control: fewer than two TriggerPush events in the log")
	}
	swapped[first], swapped[second] = swapped[second], swapped[first]
	if diff := diffGames(e.G, replayFromLog(t, cfg, swapped)); diff == "" {
		t.Fatal("control: swapping two TriggerPush events should diverge, but diffGames found no difference — the ordering choice is not actually carried by event order")
	}
}

// TestOrderingDecisionInTheDrawStepDoesNotDrawTwice is the reason
// putTriggersOnStack resumes into grantPriority rather than into
// priorityRound: an ordering decision asked mid-draw-step, once answered,
// must not cause a second draw.
//
// Ruling T28-a (rewritten for Task 28): before Task 28, the draw step's draw
// lived in priorityRound, and this test drove to its scenario by FORGING the
// draw step's state directly -- raw TurnChange/StepChange/Priority emits,
// never calling advanceStep at all -- because the property under test was
// specifically that priorityRound's OWN draw-step guard is not idempotent:
// nothing between its draw and the priority emit that followed it changed
// Passes or Priority, so re-entering priorityRound mid-round (which is what
// Advance would do if the trigger handlers simply returned and let the
// engine take another step) drew the active player a second card. Task 28
// deleted that guard and moved the draw to advanceStep, run once on entry to
// the step and never again -- which removes this test's old PREMISE (there
// is no more irreversible draw-step work sitting in priorityRound for a
// re-entry to repeat) but not the PROPERTY it exists to pin: answering an
// ordering decision asked in the draw step must still resume the round
// without drawing again. This now drives the real path -- genesis, then
// Advance/Submit passes through turn 1 and into turn 2's own draw step --
// so the draw happens exactly where advanceStep performs it today, and the
// property is pinned against the CURRENT mechanism rather than the deleted
// one. See the Task 28 report for the mutant this now catches (the old one,
// "point resumeTriggerDrain at priorityRound", no longer does, precisely
// because priorityRound no longer draws at all).
func TestOrderingDecisionInTheDrawStepDoesNotDrawTwice(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	onBoard(t, e, 1, drawTriggerSrc)
	onBoard(t, e, 1, drawTriggerSrc)
	// Baseline one step early, at upkeep: the Submit that transitions into
	// StepDraw already performs the (single, correct) draw as part of that
	// same call, before this test can observe hand/library sizes in between.
	driveToStep(t, e, 2, 1, state.StepUpkeep)
	hand := len(e.G.Zone(state.ZHand, 1))
	lib := len(e.G.Zone(state.ZLibrary, 1))

	driveToStep(t, e, 2, 1, state.StepDraw)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want the two draw triggers to ask for an order", d)
	}
	submit(t, e, 1, 0)

	if got := len(e.G.Zone(state.ZHand, 1)); got != hand+1 {
		t.Fatalf("hand = %d, want %d — the draw step ran %d times", got, hand+1, got-hand)
	}
	if got := len(e.G.Zone(state.ZLibrary, 1)); got != lib-1 {
		t.Fatalf("library = %d, want %d — the draw step ran more than once", got, lib-1)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want the interrupted draw step to have finished", d)
	}
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want both draw triggers placed", e.G.Stack)
	}

	passThroughStep(t, e, state.StepDraw, 100)
	if e.G.Step == state.StepDraw {
		t.Fatalf("the draw step never advanced (pending = %+v)", e.Pending())
	}
}

// --- Fix round 1 -----------------------------------------------------------

// TestPriorityIsNeverGrantedWithTriggersStillQueued is CR 117.5, and review
// finding F1(a). Task 27 made grantPriority reachable from inside handle,
// which runs BEFORE Submit's tail checkStateBased -- so a creature that a
// state-based action is about to sweep was still alive when the drain
// finished, and its death trigger reached the stack only after the priority
// holder had already acted. That is a stack order a player can see and lose
// to: an instant cast with that priority ends up UNDER the trigger.
//
// Before the fix: `stack when priority granted = 2, queued = 1`.
func TestPriorityIsNeverGrantedWithTriggersStillQueued(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	a := onBoard(t, e, 0, gainerSrc)
	b := onBoard(t, e, 0, drainerSrc)
	mourner := onBoard(t, e, 0, mournerSrc)
	doomed := onBoard(t, e, 0, "Name:Doomed\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	e.G.Obj(doomed).Damage = 5 // lethal, but no state-based action has swept it
	e.G.Active = 0

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if !e.putTriggersOnStack() {
		t.Fatal("expected an ordering decision")
	}
	submit(t, e, 0, 1)

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority granted", d)
	}
	if n := len(e.pendingTriggers); n != 0 {
		t.Fatalf("priority granted with %d trigger(s) still queued (stack = %d) — CR 117.5",
			n, len(e.G.Stack))
	}
	if e.G.Obj(doomed).Zone != state.ZGraveyard {
		t.Fatal("the lethally-damaged creature was still on the battlefield when priority was granted")
	}
	want := []state.ObjID{a, b, mourner}
	if len(e.G.Stack) != len(want) {
		t.Fatalf("stack = %v, want all three abilities placed before priority", e.G.Stack)
	}
	for i, src := range want {
		if got := e.G.Obj(e.G.Stack[i]).Source; got != src {
			t.Fatalf("stack[%d] is from %d, want %d", i, got, src)
		}
	}
}

// TestPriorityIsNeverGrantedToAPlayerAboutToBeSweptOut is review finding
// F1(b), the hang. Three seats, so the elimination does not end the game.
// Seat 2 holds priority and is already at zero life with no state-based
// action run yet; seat 0 answers an ordering decision. Before the fix,
// handle granted priority to seat 2 while they were still alive, Submit's
// tail checkStateBased then eliminated them, and the old release hook
// covered only the two trigger kinds -- leaving a pending priority decision
// against a departed player with G.Over false and Advance unable to make any
// progress. One goroutine per match, so that is a leaked goroutine and a
// dead game.
func TestPriorityIsNeverGrantedToAPlayerAboutToBeSweptOut(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	onBoard(t, e, 0, gainerSrc)
	onBoard(t, e, 0, drainerSrc)
	e.G.Active = 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.emit(events.Event{Kind: events.Priority, Player: 2})
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -20})
	e.pending = nil

	if !e.putTriggersOnStack() {
		t.Fatal("expected an ordering decision for player 0")
	}
	submit(t, e, 0, 1)

	if e.G.Over {
		t.Fatal("a three-seat game ended on one elimination")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("the engine is stranded: nothing pending and nothing left to run it")
	}
	if e.G.Players[d.Player].Lost {
		t.Fatalf("%s decision is against player %d, who has left the game — the match can never proceed",
			d.Kind, d.Player)
	}
	before := len(e.L.Events)
	e.Advance()
	if len(e.L.Events) == before && e.Pending() == nil {
		t.Fatal("Advance made no progress and nothing is pending")
	}
	if n := passAll(t, e, 6); n == 0 {
		t.Fatal("the match could not take another action")
	}
}

// TestADecisionAgainstADepartedPlayerIsReleased covers the widened release
// hook directly, for a decision kind that is not one of Task 27's. Review
// finding F2: the old hook's justification -- that the two trigger decisions
// were the only ones asked from inside handle -- is false. handlePriority ->
// castSpell (stack.go) -> askTarget asks a KTarget decision from inside
// handle too, so a state-based action in Submit's tail can strand it exactly
// the same way.
func TestADecisionAgainstADepartedPlayerIsReleased(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	e.pending = nil
	e.ask(&decision.Decision{Player: 1, Kind: decision.KTarget, Min: 1, Max: 1,
		Prompt:  "choose a target",
		Options: []decision.Option{{Index: 0, Kind: "player", Label: "a", Player: 0}}})
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -20})

	e.checkStateBased()

	if e.G.Over {
		t.Fatal("a three-seat game ended on one elimination")
	}
	if !e.G.Players[1].Lost {
		t.Fatal("player 1 was not eliminated")
	}
	if d := e.Pending(); d != nil && e.G.Players[d.Player].Lost {
		t.Fatalf("%s decision left pending against departed player %d", d.Kind, d.Player)
	}
}

// TestAnsweredOptionalTriggerIsNeverAskedTwice is review finding F4. The
// front-of-queue guard in handleTriggerOptional used to fail into doing
// nothing at all, so the drain immediately asked the SAME trigger's optional
// question again: the player's answer was consumed (its Seq is spent) and
// silently discarded. This forces that guard's mismatch by hand -- nothing on
// a reachable path puts a different entry at the front between an ask and its
// answer -- and asserts the answer is honoured rather than re-asked.
func TestAnsweredOptionalTriggerIsNeverAskedTwice(t *testing.T) {
	t.Parallel()
	// CR 603.5 moved the ordinary OptionalDecider$ trigger's yes/no to
	// resolution, so the placement KTriggerOptional no longer exists for it.
	// The one place a placement KTriggerOptional survives is a Miracle offer
	// (Task 18) -- a keyword CAST offer, not a 603.5 optional triggered
	// ability -- so this test re-points there: a declined Miracle offer must be
	// answered once, never re-asked, never left queued, and the card stays in
	// hand.
	terminus := "Name:Terminus\nManaCost:4 W W\nTypes:Sorcery\nK:Miracle:W\n" +
		"A:SP$ ChangeZoneAll | ChangeType$ Creature | Origin$ Battlefield | Destination$ Library | LibraryPosition$ -1\n" +
		"Oracle:x\n"
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, _, term := newFixtureDeck(t, 101, terminus, bearSrc)
	moveToLibraryTop(t, e, term)
	putCreature(t, e, 0, bearSrc)
	addMana(t, e, 0, "W")
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: term,
		From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 1 || !e.pendingTriggers[0].Miracle {
		t.Fatalf("no miracle offer: %+v", e.pendingTriggers)
	}
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional || d.Player != 0 {
		t.Fatalf("offer decision %+v", d)
	}

	submitChoices(t, e, 1) // no

	if p := e.Pending(); p != nil && p.Kind == decision.KTriggerOptional && p.Source == term {
		t.Fatalf("the declined Miracle offer (%d) was asked about again", term)
	}
	for _, pt := range e.pendingTriggers {
		if pt.Source == term {
			t.Fatalf("the declined Miracle offer %d is still queued", term)
		}
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == term {
			t.Fatalf("the declined Miracle offer %d reached the stack", term)
		}
	}
	if e.G.Obj(term).Zone != state.ZHand {
		t.Fatalf("the declined Miracle card is in %s, want the hand", e.G.Obj(term).Zone)
	}
}

// TestDiffGamesSeesTheGameResult is review finding F6. diffGames is the
// harness every replay proof in this package leans on, and Task 24 inherits
// it. It compared Players, Objs, Stack, the zones and most turn scalars, but
// not Winner, Draw or NextID -- so a game that ended in a draw compared equal
// to one seat 0 won.
func TestDiffGamesSeesTheGameResult(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mut  func(g *state.Game)
	}{
		{"winner", func(g *state.Game) { g.Over, g.Winner = true, 1 }},
		{"draw", func(g *state.Game) { g.Over, g.Draw = true, true }},
		{"next id", func(g *state.Game) { g.NextID = 99 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := state.NewGame([]string{"a", "b"})
			a.Over = true
			b := a.Clone()
			tc.mut(b)
			if diff := diffGames(a, b); diff == "" {
				t.Fatalf("diffGames cannot see a difference in %s", tc.name)
			}
		})
	}
}

// TestTriggerDrainInvariantsUnderRandomizedPlay is the measurement fix round
// 1 owes the re-drain that resumeTriggerDrain now performs. The concern is
// specific: re-entering putTriggersOnStack after state-based actions have
// queued more work must not re-sort or re-offer triggers whose order a player
// has already fixed. Rather than argue that from e.orderedTriggers, this
// plays many games and asserts it, alongside the two properties fix round 1
// exists to restore.
//
// Invariants checked after every single Submit, on every game:
//
//	I1  CR 117.5 — no priority decision is ever handed out with triggers
//	    still queued.
//	I2  No decision of any kind is ever pending against a player who has
//	    left the game (the hang).
//	I3  0 <= orderedTriggers <= len(pendingTriggers), and zero when empty.
//	I4  No ordering decision is ever offered twice with the identical option
//	    set and no TriggerPush in between — a re-offer of a settled group.
func TestTriggerDrainInvariantsUnderRandomizedPlay(t *testing.T) {
	t.Parallel()
	const maxDecisions = 400
	games, decisions, orders, optionals, eliminations := 0, 0, 0, 0, 0
	for _, seats := range []int{2, 3, 4} {
		for seed := uint64(1); seed <= 40; seed++ {
			names := make([]string, seats)
			decks := make([][]*cards.Card, seats)
			for i := range names {
				names[i] = string(rune('a' + i))
				decks[i] = mountainDeck(t, 40)
			}
			e := New(Config{Seed: seed, Names: names, Decks: decks})
			// Give every seat two or three simultaneous upkeep triggers, so
			// several controllers are asked to order in the same round, and
			// include the life-draining one so seats really are eliminated
			// mid-decision.
			for p := 0; p < seats; p++ {
				onBoard(t, e, state.PlayerID(p), gainerSrc)
				onBoard(t, e, state.PlayerID(p), mayGainSrc)
				if p%2 == 0 {
					onBoard(t, e, state.PlayerID(p), drainerSrc)
				}
			}
			e.Advance()
			games++

			r := newRNG(seed * 7)
			lastOffer, pushesAtOffer := "", -1
			pushes := func() int {
				n := 0
				for _, ev := range e.L.Events {
					if ev.Kind == events.TriggerPush {
						n++
					}
				}
				return n
			}
			for n := 0; n < maxDecisions && !e.G.Over; n++ {
				d := e.Pending()
				if d == nil {
					break
				}
				// I2, checked on the decision as offered.
				if e.G.Players[d.Player].Lost {
					t.Fatalf("seats=%d seed=%d: %s decision pending against departed player %d",
						seats, seed, d.Kind, d.Player)
				}
				// I3.
				if e.orderedTriggers < 0 || e.orderedTriggers > len(e.pendingTriggers) {
					t.Fatalf("seats=%d seed=%d: orderedTriggers=%d, queue=%d",
						seats, seed, e.orderedTriggers, len(e.pendingTriggers))
				}
				var choices []int
				switch d.Kind {
				case decision.KPriority:
					// I1.
					if len(e.pendingTriggers) != 0 {
						t.Fatalf("seats=%d seed=%d: priority granted with %d trigger(s) queued — CR 117.5",
							seats, seed, len(e.pendingTriggers))
					}
					choices = []int{r.IntN(len(d.Options))}
				case decision.KTriggerOrder:
					// I4.
					offer := fmt.Sprintf("p%d:", d.Player)
					for _, o := range d.Options {
						offer += fmt.Sprintf("%d,", o.Obj)
					}
					if offer == lastOffer && pushes() == pushesAtOffer {
						t.Fatalf("seats=%d seed=%d: ordering decision %s offered twice with nothing placed in between — a settled group was re-offered",
							seats, seed, offer)
					}
					lastOffer, pushesAtOffer = offer, pushes()
					// Fisher-Yates over the option indices: a uniformly
					// random permutation, which is exactly the answer shape
					// Decision.Validate accepts for an ordering decision.
					perm := make([]int, len(d.Options))
					for i := range perm {
						perm[i] = i
					}
					for i := len(perm) - 1; i > 0; i-- {
						j := r.IntN(i + 1)
						perm[i], perm[j] = perm[j], perm[i]
					}
					choices = perm
					orders++
				case decision.KTriggerOptional:
					choices = []int{r.IntN(2)}
					optionals++
				case decision.KAttackers, decision.KBlockers:
					choices = nil
				default:
					t.Fatalf("unexpected decision kind %q", d.Kind)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
					t.Fatalf("seats=%d seed=%d: submit %s %v: %v", seats, seed, d.Kind, choices, err)
				}
				decisions++
			}
			for _, p := range e.G.Players {
				if p.Lost {
					eliminations++
				}
			}
			// A game that neither ended nor left a decision pending is a
			// stall, which is the totality constraint's own failure mode.
			if !e.G.Over && e.Pending() == nil {
				t.Fatalf("seats=%d seed=%d: the match stalled — not over and nothing pending", seats, seed)
			}
		}
	}
	t.Logf("games=%d decisions=%d orderings=%d optionals=%d eliminations=%d",
		games, decisions, orders, optionals, eliminations)
}

// --- Fix round 2 -----------------------------------------------------------

// TestASpellWhoseTargetDecisionWasReleasedDoesNotResolve is re-review finding
// N1, which fix round 1 introduced: widening the release hook turned a stall
// into a spell that RESOLVES with no targets at all. resolveTop's CR 608.2b
// fizzle branch was gated on `spec != "" && len(targets) > 0`, using "has
// targets" as a proxy for "needs targets" -- so a spell whose target decision
// was released skipped the legality check entirely and ran, untargeted riders
// included. CR 608.2b (a spell with no legal targets is countered) and CR
// 800.4a (a departed player's spell ceases to exist) both say it must not.
//
// Before: resolveEvents=1, the eliminated caster's life 0 -> 7.
func TestASpellWhoseTargetDecisionWasReleasedDoesNotResolve(t *testing.T) {
	t.Parallel()
	const riderboltSrc = `Name:Riderbolt
ManaCost:R
Types:Instant
A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBGain | SpellDescription$ x
SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 7
Oracle:x
`
	e := newSeats(t, 3)
	e.pending = nil
	id := e.G.AddObject(card(t, riderboltSrc), 0).ID
	e.G.Obj(id).Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{id})
	e.emit(events.Event{Kind: events.PutOnStack, Obj: id, Player: 0,
		From: state.ZHand, To: state.ZStack, Text: "Riderbolt"})
	e.askTarget(0, id, e.G.Obj(id).Face().SpellAbility())
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want a target decision", d)
	}

	// Eliminate the caster while their target decision is outstanding. The
	// release hook (fix round 1) clears it so the match can continue.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -20})
	e.checkStateBased()
	if !e.G.Players[0].Lost {
		t.Fatal("the caster was not eliminated")
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatal("the target decision was not released")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want departed caster's spell to have ceased", e.G.Stack)
	}

	life := e.G.Players[0].Life

	for _, ev := range e.L.Events {
		if ev.Kind == events.Resolve && ev.Obj == id {
			t.Fatal("a spell with a targeting requirement and no targets resolved — CR 608.2b")
		}
	}
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("life = %d, want %d — the spell's untargeted rider ran anyway", got, life)
	}
	if o := e.G.Obj(id); o.Zone != state.ZCeased {
		t.Fatalf("the departed caster's spell is in %s, want ceased", o.Zone)
	}
}

// TestATriggerWhoseControllerLeftIsNotPushedByItsDecider is re-review finding
// N2, and Ruling U6: an ability controlled by a player who has left the game
// ceases to exist (CR 800.4a). Under CR 603.5 the optional trigger is now
// pushed while its controller is alive, so the harder case is the ON-STACK
// one: the controller leaves after the ability is already on the stack, and
// the ability ceases to exist then (ceaseDepartedObjects, CR 800.4a) rather
// than being resolved for a departed controller.
func TestATriggerWhoseControllerLeftIsNotPushedByItsDecider(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	e.pending = nil
	onBoard(t, e, 0, wardenSrc)
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	bear.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{bear.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear.ID,
		From: state.ZHand, To: state.ZBattlefield})

	// CR 603.5: the ability goes on the stack regardless, so putTriggersOnStack
	// pushes it while the controller (player 0) is alive, asking nothing at
	// placement.
	if e.putTriggersOnStack() {
		t.Fatalf("an optional trigger asked at placement: %+v", e.Pending())
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the ability on the stack while its controller lives", e.G.Stack)
	}
	abID := e.G.Stack[0]

	// The trigger's CONTROLLER (player 0) leaves the game while the ability is
	// on the stack. The ability ceases to exist (CR 800.4a), regardless of
	// what its decider (player 1) would have answered.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -20})
	e.checkStateBased()
	if !e.G.Players[0].Lost {
		t.Fatal("the trigger's controller was not eliminated")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the departed controller's on-stack ability to have ceased", e.G.Stack)
	}
	if o := e.G.Obj(abID); o == nil || o.Zone != state.ZCeased {
		t.Fatalf("the departed controller's on-stack ability is in %v, want ceased (CR 800.4a)", e.G.Obj(abID))
	}
}

// TestOptionalTriggerWhoseDeciderLeftTheGameCeasesToExist is fix round 1's
// unpinned-branch leaf -- the reason this fix round exists. The CR 603.5 block
// in resolveTop has a branch for an OptionalDecider$ trigger whose decider has
// left the game (CR 800.4a: the ability ceases to exist rather than being
// offered to the departed seat for a yes/no nobody can answer), guarded by
// `if !askable`. Nothing observed that branch: the controller's own fix round
// swapped it for `if false && !askable`, `go test ./rules` stayed green, and
// the departed decider's question was posed to a seat that cannot answer. This
// leaf pins the branch against exactly that regression: an OptionalDecider$
// trigger goes on the stack with a LIVING controller, the decider leaves the
// game, and the ability resolves into exile rather than into the ask.
func TestOptionalTriggerWhoseDeciderLeftTheGameCeasesToExist(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	e.pending = nil // drop the turn-1 priority decision; the drain owns the queue below
	onBoard(t, e, 0, wardenSrc)
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	bear.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{bear.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear.ID,
		From: state.ZHand, To: state.ZBattlefield})

	// CR 603.5: the ability goes on the stack regardless. Its controller is
	// player 0 (the Warden); its OptionalDecider$ is the
	// TriggeredCardController -- player 1, the bear's controller.
	if e.putTriggersOnStack() {
		t.Fatalf("an optional trigger asked at placement: %+v", e.Pending())
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the ability on the stack", e.G.Stack)
	}
	abID := e.G.Stack[0]

	// The DECIDER (player 1) leaves the game while the ability is on the
	// stack, but the trigger's controller (player 0) is still alive. CR 800.4a
	// sweeps only the departed player's owned cards and on-stack abilities, so
	// the controller's ability stays. The departure is a concession at a
	// priority ask -- the lawful way to leave while a 3-seat game goes on --
	// emitted as the same PlayerLost "conceded" shape the departed-chooser
	// fixture uses.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "conceded"})
	e.checkStateBased()
	if !e.G.Players[1].Lost {
		t.Fatal("the decider was not eliminated")
	}
	if e.G.Over {
		t.Fatal("a three-seat game ended on one departure")
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the ability still on the stack (its controller lives)", e.G.Stack)
	}

	// Now let it resolve. The decider is gone: the CR 603.5 block must move the
	// ability to exile as ceased-to-exist (CR 800.4a) instead of posing the
	// yes/no to a departed seat.
	e.resolveTop()

	// (a) CR 800.4a: the ability is no longer on the stack and parks in exile
	// carrying the "ceased to exist" text.
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the departed-decider ability to have ceased off it", e.G.Stack)
	}
	if o := e.G.Obj(abID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the departed-decider ability is in %v, want exile (CR 800.4a)", e.G.Obj(abID))
	}
	ceased := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == abID &&
			ev.From == state.ZStack && ev.To == state.ZExile &&
			strings.Contains(ev.Text, "ceased to exist") {
			ceased = true
			break
		}
	}
	if !ceased {
		t.Fatal("no 'ceased to exist' MoveZone for the departed-decider ability (CR 800.4a)")
	}

	// (b) no KTriggerOptional decision was ever asked of the departed seat
	// after the departure -- the branch pins the ask only to a decider who can
	// still answer it.
	departed := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.PlayerLost && ev.Player == 1 && ev.Text == "conceded" {
			departed = i
			break
		}
	}
	if departed < 0 {
		t.Fatal("no concession PlayerLost for the decider in the log")
	}
	for i := departed + 1; i < len(e.L.Events); i++ {
		if ev := e.L.Events[i]; ev.Kind == events.DecisionAsk && ev.Player == 1 {
			t.Fatalf("after the decider left, a %q decision was asked of departed seat 1", ev.Text)
		}
	}
}

// TestResumeTriggerDrainIsInertWhileADecisionIsPending pins fix round 2's
// structural guard. resumeTriggerDrain's termination used to rest entirely on
// the argument that it is never entered with e.pending set -- true of all
// three callers, but an argument every future reader has to re-derive, and
// the re-review showed it is one statement-reorder away from unbounded
// recursion (swapping releasePendingDecisionOfDepartedPlayer's two lines
// makes it run away immediately). Task 22 is this repo's standing evidence
// for what such an argument is worth: wrong four times out of four.
//
// The guard turns it into a control-flow property, and this is the failure it
// prevents: re-entering the resume while a decision is outstanding used to
// drain the queue and replace that decision with a fresh one, placing
// triggers behind the answering player's back.
func TestResumeTriggerDrainIsInertWhileADecisionIsPending(t *testing.T) {
	t.Parallel()
	e, _ := upkeepEngine(t, gainerSrc, drainerSrc)
	if !e.putTriggersOnStack() {
		t.Fatal("expected an ordering decision")
	}
	d := e.Pending()
	queued, ordered := len(e.pendingTriggers), e.orderedTriggers

	e.resumeTriggerDrain()

	if e.Pending() != d {
		t.Fatalf("the outstanding decision was replaced: %+v", e.Pending())
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want nothing placed while a decision was outstanding", e.G.Stack)
	}
	if len(e.pendingTriggers) != queued || e.orderedTriggers != ordered {
		t.Fatalf("queue = %d/%d, want %d/%d — it was drained behind the player's back",
			len(e.pendingTriggers), e.orderedTriggers, queued, ordered)
	}
}

// --- Task 23 -----------------------------------------------------------

// TestPendingTriggersReportsQueueOrderAndOptionality is Task 23's Ruling
// F2/R3 accessor: view.Project needs the trigger queue without importing
// rules, so PendingTriggers exposes it built from the same
// pendingTriggers/triggerLabel/optionalDecider machinery the drain itself
// uses, and must not mutate any of it.
func TestPendingTriggersReportsQueueOrderAndOptionality(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, gainerSrc, drainerSrc)
	pts := e.PendingTriggers()
	if len(pts) != 2 {
		t.Fatalf("PendingTriggers = %d entries, want 2", len(pts))
	}
	if pts[0].Source != ids[0] || pts[1].Source != ids[1] {
		t.Fatalf("PendingTriggers sources = %d,%d, want %d,%d (queue order)",
			pts[0].Source, pts[1].Source, ids[0], ids[1])
	}
	for i, pt := range pts {
		if pt.Controller != 0 {
			t.Errorf("pts[%d].Controller = %d, want 0", i, pt.Controller)
		}
		if pt.Label == "" {
			t.Errorf("pts[%d].Label is empty", i)
		}
		if pt.Optional {
			t.Errorf("pts[%d].Optional = true, want false (no OptionalDecider$)", i)
		}
	}
	// Read-only: calling it must not touch the queue the drain itself uses.
	if len(e.pendingTriggers) != 2 || e.orderedTriggers != 0 {
		t.Fatalf("PendingTriggers mutated engine state: queue=%d ordered=%d",
			len(e.pendingTriggers), e.orderedTriggers)
	}
}

// TestPendingTriggersReportsOptionalDecider covers the Optional/Decider
// fields against mayGainSrc's OptionalDecider$ You.
func TestPendingTriggersReportsOptionalDecider(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, mayGainSrc)
	pts := e.PendingTriggers()
	if len(pts) != 1 {
		t.Fatalf("PendingTriggers = %d entries, want 1", len(pts))
	}
	if !pts[0].Optional {
		t.Fatal("mayGainSrc's OptionalDecider$ You did not report Optional")
	}
	if pts[0].Decider != 0 {
		t.Fatalf("Decider = %d, want the controller (0)", pts[0].Decider)
	}
	if pts[0].Source != ids[0] {
		t.Fatalf("Source = %d, want %d", pts[0].Source, ids[0])
	}
}

// TestPendingTriggersReturnsAFreshSlice guards Task 23 §10's no-aliasing
// rule: a caller mutating the returned slice must not corrupt the engine's
// own queue, nor a slice returned by an earlier call.
func TestPendingTriggersReturnsAFreshSlice(t *testing.T) {
	t.Parallel()
	e, _ := upkeepEngine(t, gainerSrc, drainerSrc)
	pts := e.PendingTriggers()
	pts[0].Label = "corrupted"
	again := e.PendingTriggers()
	if again[0].Label == "corrupted" {
		t.Fatal("PendingTriggers shared backing storage across calls")
	}
}

// TestPendingTriggersIsEmptyWithNoQueue is the boundary Project's totality
// rules lean on: an idle engine reports no pending triggers at all.
func TestPendingTriggersIsEmptyWithNoQueue(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	if pts := e.PendingTriggers(); len(pts) != 0 {
		t.Fatalf("PendingTriggers = %v, want none", pts)
	}
}

// hunterSrc is a permanent whose upkeep trigger takes a single target
// (Task 7): the shape a target-bearing trigger's own KTarget decision runs.
const hunterSrc = `Name:Hunter
ManaCost:B
Types:Artifact Creature
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigHunt | TriggerDescription$ exile a target permanent
SVar:TrigHunt:DB$ ChangeZone | ValidTgts$ Permanent | TargetMin$ 1 | TargetMax$ 1 | Origin$ Battlefield | Destination$ Exile
Oracle:x
`

// TestTriggerTargetAskResumesTheDrain is Task 7's drain-continuation test.
// A trigger that takes targets is asked right after its TriggerPush; the
// answer must resume the SAME drain (handleTarget -> resumeTriggerDrain, the
// continuation handleTriggerOrder already uses -- not a parallel path through
// turn.go) so a second, unrelated trigger behind it is still placed before
// any player receives priority. If the target answer instead granted priority
// mid-queue, the second trigger would sit unplaced behind the priority
// decision (violating CR 117.5) and this test's stack count would be 1, not 2.
func TestTriggerTargetAskResumesTheDrain(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// Two simultaneous, same-controller triggers: the first (hunter) takes a
	// target, the second (gainer) is a plain life-gain.
	a := onBoard(t, e, 0, hunterSrc)
	b := onBoard(t, e, 0, gainerSrc)
	prey := onBoard(t, e, 1, "Name:Prey\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	aSA := e.G.Obj(a).Face().Triggers[0].Effect
	bSA := e.G.Obj(b).Face().Triggers[0].Effect
	e.pendingTriggers = []pendingTrigger{
		{Source: a, Controller: 0, Idx: 0, SA: aSA, Ctx: effects.Ctx{Source: a, Controller: 0}},
		{Source: b, Controller: 0, Idx: 0, SA: bSA, Ctx: effects.Ctx{Source: b, Controller: 0}},
	}
	// Settling the two-trigger group's order by hand lets putTriggersOnStack
	// go straight to placing (and target-asking) the first entry rather than
	// asking the R1 KTriggerOrder decision this test is not about.
	e.orderedTriggers = 2

	if !e.putTriggersOnStack() {
		t.Fatal("expected a target decision, the drain did not pause")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 1 || d.Max != 1 {
		t.Fatalf("pending %+v, want a single-target decision", d)
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack %v, want only the first trigger placed before the ask", e.G.Stack)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == prey {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("prey not offered as a target: %+v", d.Options)
	}
	submit(t, e, idx)

	// The answer resumed the drain: the second life-gain trigger is ALSO now
	// on the stack -- both were placed before any priority decision.
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack %v after answer, want both triggers placed before priority", e.G.Stack)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(prey).Zone != state.ZExile {
		t.Fatalf("prey in %s, want exile (the targeting trigger resolved)", e.G.Obj(prey).Zone)
	}
	if e.G.Players[0].Life != 25 {
		t.Fatalf("life %d, want 25 (the chained life-gain trigger also resolved)", e.G.Players[0].Life)
	}
}
