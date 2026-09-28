package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The CR 508.1 declare-attackers batch pin: declaring attackers is ONE
// turn-based action however many defenders are attacked, so a BATCH
// "whenever you attack" trigger (Mode$ AttackersDeclared with no
// AttackedTarget$) must fire exactly ONCE for a declaration that splits its
// attackers across two defenders. Before the latch the engine emitted one
// DeclareAttackers event per defender and the batch trigger fired once per
// defender -- an over-fire. A queued trigger is pushed to the stack by the
// Submit continuation, so these tests count the TriggerPush events in the log
// (exactly one per queued trigger), not pendingTriggers.

// attackersDeclaredBatchSeat builds a three-seat table with Love on the
// Battlefield on seat 0's battlefield and two ready 2/2 attackers on seat 0,
// active and sitting in the declare-attackers step. Two attackers is the
// card's own exactly-two gate (IsPresent$ Creature.attacking+YouCtrl |
// PresentCompare$ EQ2), so the split declaration below satisfies it.
func attackersDeclaredBatchSeat(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	love := mshCorpusCardPath(t, "Love on the Battlefield", "l/love_on_the_battlefield.txt")
	e := threeSeatEngine(t)
	loveID := onBoardCard(t, e, 0, love)
	a := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b := onBoardReady(t, e, 0, "Name:Fox\nManaCost:1 W\nTypes:Creature Fox\nPT:2/2\nOracle:x\n")
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	return e, loveID, a, b
}

// batchAttackerOptionIndex returns the KAttackers option for (attacker,
// defender), failing the test if neither the decision nor the pair is there.
func batchAttackerOptionIndex(t *testing.T, e *Engine, id state.ObjID, def state.PlayerID) int {
	t.Helper()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("precondition: %d triggers already queued before the declaration", len(e.pendingTriggers))
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id && o.Player == def {
			return o.Index
		}
	}
	t.Fatalf("no attacker option for obj %d at defender %d: %+v", id, def, d.Options)
	return -1
}

// batchTriggerPushes counts the TriggerPush events the declaration queued.
func batchTriggerPushes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush {
			n++
		}
	}
	return n
}

// declareAttackersAt submits the pairs (attacker, defender) in one intent and
// returns nothing; it fails the test on a rejected intent.
func declareAttackersAt(t *testing.T, e *Engine, pairs [][2]int) {
	t.Helper()
	choices := make([]int, 0, len(pairs))
	for _, p := range pairs {
		choices = append(choices, p[1])
	}
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit declaration %v: %v", pairs, err)
	}
}

// TestAttackersDeclaredBatchFiresOnceOnSplitAttack is the fix pin. Seat 0
// declares attacker A at seat 1 and attacker B at seat 2 -- ONE declare step,
// two defenders, so two DeclareAttackers events but exactly one batch
// trigger. It asserts the precondition (two DIFFERENT defenders were really
// attacked, so the old per-defender code would have fired twice), that only
// one trigger pushed, and that the single resolution pumped BOTH attackers
// (Defined$ TriggeredAttackers must name the whole declaration, not just the
// first event's group).
func TestAttackersDeclaredBatchFiresOnceOnSplitAttack(t *testing.T) {
	t.Parallel()
	e, _, a, b := attackersDeclaredBatchSeat(t)

	e.askAttackers()
	// Precondition: the option list really does offer the split -- A at 1
	// and B at 2. If it does not, the test's premise is gone and it fails
	// here rather than passing vacuously.
	ai := batchAttackerOptionIndex(t, e, a, 1)
	bi := batchAttackerOptionIndex(t, e, b, 2)
	declareAttackersAt(t, e, [][2]int{{int(a), ai}, {int(b), bi}})

	// Precondition: the declaration really split across two defenders, i.e.
	// the engine emitted one DeclareAttackers event per defending player,
	// with a different attacker in each group.
	seen := map[state.PlayerID][]state.ObjID{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.DeclareAttackers {
			seen[ev.Player] = append(seen[ev.Player], ev.IDs...)
		}
	}
	if len(seen[1]) != 1 || len(seen[2]) != 1 {
		t.Fatalf("precondition failed: declaration did not split one attacker to each of seats 1 and 2: DeclareAttackers groups = %v", seen)
	}
	if seen[1][0] == seen[2][0] {
		t.Fatalf("precondition failed: the same attacker appears at both defenders: %v", seen)
	}

	// The batch trigger must have pushed EXACTLY once. Two pushes is the old
	// per-defender over-fire.
	if got := batchTriggerPushes(e); got != 1 {
		t.Fatalf("batch trigger pushed %d times for one split declaration, want 1", got)
	}
	// Precondition for the pump: neither attacker has first strike yet.
	if e.HasKeyword(a, "First Strike") || e.HasKeyword(b, "First Strike") {
		t.Fatal("precondition failed: an attacker already has first strike before the trigger resolves")
	}

	e.resolveTop()
	if !e.HasKeyword(a, "First Strike") || !e.HasKeyword(b, "First Strike") {
		t.Fatalf("TriggeredAttackers did not name the whole declaration: first strike A=%v B=%v, want both true",
			e.HasKeyword(a, "First Strike"), e.HasKeyword(b, "First Strike"))
	}
}

// TestAttackersDeclaredBatchFiresOnceWhenBothAttackSameDefender is the
// single-defender control: the ordinary (non-split) declaration of two
// attackers at ONE defender pushes the batch trigger exactly once too, so the
// fix is not over-latching declarations that were already correct.
func TestAttackersDeclaredBatchFiresOnceWhenBothAttackSameDefender(t *testing.T) {
	t.Parallel()
	e, _, a, b := attackersDeclaredBatchSeat(t)

	e.askAttackers()
	ai := batchAttackerOptionIndex(t, e, a, 1)
	bi := batchAttackerOptionIndex(t, e, b, 1)
	declareAttackersAt(t, e, [][2]int{{int(a), ai}, {int(b), bi}})
	// Precondition: both attackers really landed at ONE defender.
	nAt1 := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DeclareAttackers && ev.Player == 1 {
			nAt1 += len(ev.IDs)
		}
	}
	if nAt1 != 2 {
		t.Fatalf("precondition failed: seat 1 was attacked by %d attackers, want 2", nAt1)
	}
	if got := batchTriggerPushes(e); got != 1 {
		t.Fatalf("batch trigger pushed %d times for a same-defender declaration, want 1", got)
	}
}

// TestAttackersDeclaredBatchFiresOnceOnSplitAttackBard is the over-fire pin
// proper, on Bard, Heir of Girion ("Whenever you attack, draw a card."), a
// batch trigger with NO cumulative-count gate -- so unlike Love on the
// Battlefield the old per-defender code queued it once for EACH defender's
// event. A split attack must draw exactly one card, not two.
func TestAttackersDeclaredBatchFiresOnceOnSplitAttackBard(t *testing.T) {
	t.Parallel()
	bard := mshCorpusCardPath(t, "Bard, Heir of Girion", "b/bard_heir_of_girion.txt")
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, bard)
	a := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b := onBoardReady(t, e, 0, "Name:Fox\nManaCost:1 W\nTypes:Creature Fox\nPT:2/2\nOracle:x\n")
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	handBefore := len(e.G.Zone(state.ZHand, 0))

	e.askAttackers()
	ai := batchAttackerOptionIndex(t, e, a, 1)
	bi := batchAttackerOptionIndex(t, e, b, 2)
	declareAttackersAt(t, e, [][2]int{{int(a), ai}, {int(b), bi}})

	// Precondition: the declaration split one attacker to each defender.
	nAt1, nAt2 := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DeclareAttackers {
			switch ev.Player {
			case 1:
				nAt1 += len(ev.IDs)
			case 2:
				nAt2 += len(ev.IDs)
			}
		}
	}
	if nAt1 != 1 || nAt2 != 1 {
		t.Fatalf("precondition failed: split groups = seat1:%d seat2:%d, want 1 and 1", nAt1, nAt2)
	}
	// The over-fire pin itself: ONE declare step queues the batch trigger
	// exactly once. On the reverted code the second defender's event queues
	// it again, so this fails before the resolution that would otherwise
	// double-draw.
	if got := batchTriggerPushes(e); got != 1 {
		t.Fatalf("Bard trigger pushed %d times for one split declaration, want 1", got)
	}
	e.resolveTop()

	// One draw for ONE declare step. Two is the old per-defender over-fire.
	if got := len(e.G.Zone(state.ZHand, 0)) - handBefore; got != 1 {
		t.Fatalf("Bard drew %d cards for one split declaration, want 1", got)
	}
}

// TestAttackersDeclaredOneTargetStillFiresPerDefender is the control: a
// PER-DEFENDER attack trigger (Mode$ AttackersDeclaredOneTarget) is genuinely
// one triggering condition per defending player, so a split attack fires it
// once for EACH defender. It pins that the batch latch did not swallow the
// per-defender shape. Karazikar, the Eye Tyrant is the corpus carrier
// ("Whenever you attack a player, tap target creature that player controls").
func TestAttackersDeclaredOneTargetStillFiresPerDefender(t *testing.T) {
	t.Parallel()
	karazikar := mshCorpusCardPath(t, "Karazikar, the Eye Tyrant", "k/karazikar_the_eye_tyrant.txt")
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, karazikar)
	a := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b := onBoardReady(t, e, 0, "Name:Fox\nManaCost:1 W\nTypes:Creature Fox\nPT:2/2\nOracle:x\n")
	// One creature per attacked defender for the trigger's own target ask.
	onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	onBoard(t, e, 2, "Name:Ox\nManaCost:2\nTypes:Creature Ox\nPT:1/1\nOracle:x\n")
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	e.askAttackers()
	ai := batchAttackerOptionIndex(t, e, a, 1)
	bi := batchAttackerOptionIndex(t, e, b, 2)
	declareAttackersAt(t, e, [][2]int{{int(a), ai}, {int(b), bi}})

	// Precondition: the declaration split, so a per-defender shape has two
	// triggering conditions.
	nAt1, nAt2 := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DeclareAttackers {
			switch ev.Player {
			case 1:
				nAt1 += len(ev.IDs)
			case 2:
				nAt2 += len(ev.IDs)
			}
		}
	}
	if nAt1 != 1 || nAt2 != 1 {
		t.Fatalf("precondition failed: split declaration groups = seat1:%d seat2:%d, want 1 and 1", nAt1, nAt2)
	}

	// One per-defender trigger for each attacked player: exactly two QUEUED.
	// A per-defender trigger's target is chosen when the trigger is put on
	// the stack, so the two sit in the queue behind a trigger-order decision
	// rather than being auto-pushed (a single trigger needs no ordering, the
	// batch case above). The batch latch must not apply to this mode: two
	// entries is the per-defender behaviour, zero or one would be the latch
	// wrongly swallowing it.
	if got := len(e.pendingTriggers); got != 2 {
		t.Fatalf("OneTarget queued %d triggers for a two-defender split, want 2", got)
	}
	tod := e.Pending()
	if tod == nil || tod.Kind != decision.KTriggerOrder || len(tod.Options) != 2 {
		t.Fatalf("OneTarget trigger-order decision = %+v, want two orderable triggers", tod)
	}
}
