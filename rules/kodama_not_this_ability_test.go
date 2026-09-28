// NotThisAbility$ True (task nta1): a ChangesZone trigger's self-exclusion
// clause -- "if it wasn't put onto the battlefield with this ability". The
// corpus's one carrier is Kodama of the East Tree, whose ETB rider drops a
// hand permanent and would re-fire on that very placement without the gate
// (a self-feeding loop the card text explicitly excludes). The tests pin the
// REAL corpus card end to end; Kodama is in no repo deck and no legacy
// golden deck, so no chain head depends on it. The helpers come from
// search_library_test.go (same package): the deck is built from compiled
// corpus cards only, so no Forge script text is committed here either.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kodamaEngine deals seat 0 a 40-card deck whose opening hand holds Kodama
// of the East Tree and three Grizzly Bears, then puts Kodama on seat 0's
// battlefield. Seat 0 starts (seatZeroStart), so the scenario runs in seat
// 0's Main1. The returned engine is at Main1 with Kodama battlefield-side
// and the three bears hand-side.
func kodamaEngine(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	kodama := searchCorpusCard(t, reg, "Kodama of the East Tree")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{kodama, bear, bear, bear}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9210, Names: []string{"kodama", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	kid := searchMoveByName(t, e, "Kodama of the East Tree", state.ZBattlefield)
	// Precondition: the trigger source sits where TriggerZones$ Battlefield
	// will look for it, and nothing has fired it yet.
	if o := e.G.Obj(kid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Kodama not on the battlefield after setup: %+v", o)
	}
	if got := kodamaTriggerPushes(t, e, kid); got != 0 {
		t.Fatalf("precondition: Kodama trigger already pushed %d times at setup", got)
	}
	return e, cfg, kid
}

// kodamaTriggerPushes counts the trigger instances pushed for Kodama's
// ChangesZone trigger across the log so far.
func kodamaTriggerPushes(t *testing.T, e *Engine, kid state.ObjID) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == kid {
			n++
		}
	}
	return n
}

// kodamaPendingTriggers counts Kodama trigger instances queued but not yet
// pushed onto the stack.
func kodamaPendingTriggers(e *Engine, kid state.ObjID) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == kid {
			n++
		}
	}
	return n
}

// bearAskOption finds the hand_move ask's option naming a bear (the eligible
// set holds the bears AND every land -- a land is a permanent card with mana
// value 0, inside cmcLE2 -- so the option must be picked by face name, not
// by position).
func bearAskOption(t *testing.T, e *Engine, d *decision.Decision) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind != "hand_move" || o.Obj == 0 {
			continue
		}
		if obj := e.G.Obj(o.Obj); obj != nil && obj.Face() != nil && obj.Face().Name == "Grizzly Bears" {
			return o.Index
		}
	}
	t.Fatalf("no hand_move option over a bear: %+v", d.Options)
	return -1
}

// drainKodama passes priority decisions until the game settles: exactly want
// Kodama trigger pushes recorded, an empty stack and no Kodama trigger left
// queued. A non-priority decision while draining is itself a re-fired
// trigger's ask -- the defect the exclusion gate exists to stop -- so it
// fails the test with the decision attached.
func drainKodama(t *testing.T, e *Engine, kid state.ObjID, want int) {
	t.Helper()
	for i := 0; i < 12; i++ {
		if kodamaTriggerPushes(t, e, kid) == want && len(e.G.Stack) == 0 && kodamaPendingTriggers(e, kid) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("settled out of decisions with pushes=%d want=%d stack=%d pending=%d",
				kodamaTriggerPushes(t, e, kid), want, len(e.G.Stack), kodamaPendingTriggers(e, kid))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected non-priority decision while draining (%d pushes, want %d): %+v -- "+
				"a re-fired trigger would surface here", kodamaTriggerPushes(t, e, kid), want, d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("did not settle within 12 passes (pushes=%d want=%d stack=%d pending=%d)",
		kodamaTriggerPushes(t, e, kid), want, len(e.G.Stack), kodamaPendingTriggers(e, kid))
}

// castBearInHand casts the named bear from seat 0's hand at the current
// priority decision, funding {1}{G} first (no tap-to-pay in this build).
func castBearInHand(t *testing.T, e *Engine, bearID state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: want a priority decision to cast from, got %+v", d)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: bear %d not in hand: %+v", bearID, o)
	}
	addMana(t, e, 0, "GG")
	d = e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bearID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for bear %d: %+v", bearID, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
}

// handBears returns the bears currently in seat 0's hand.
func handBears(t *testing.T, e *Engine) []state.ObjID {
	t.Helper()
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			out = append(out, id)
		}
	}
	return out
}

// TestKodamaNotThisAbilityPlacementDoesNotReFire is the reported defect: the
// rider's own placement must not re-fire the trigger. A bear is cast and
// enters (firing the trigger once); the trigger's Execute drops a SECOND
// bear from hand; that placement must NOT queue another trigger instance.
func TestKodamaNotThisAbilityPlacementDoesNotReFire(t *testing.T) {
	t.Parallel()
	e, cfg, kid := kodamaEngine(t)
	bears := handBears(t, e)
	if len(bears) < 3 {
		t.Fatalf("precondition: want at least three bears in the opening hand, got %v", bears)
	}
	// Cast the first bear: its entry fires Kodama's trigger exactly once,
	// and the Execute's optional hand-move ask suspends the resolution.
	castBearInHand(t, e, bears[0])
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("precondition: want the Execute's hand_move ask, got %+v", d)
	}
	if d.Source != kid {
		t.Fatalf("hand_move ask source %d, want Kodama %d", d.Source, kid)
	}
	if got := kodamaTriggerPushes(t, e, kid); got != 1 {
		t.Fatalf("precondition: the bear's entry fired the trigger %d times, want 1", got)
	}
	// THE FIX: answer the ask by placing the second bear -- and that
	// placement's own entry must not queue a second trigger.
	idx := bearAskOption(t, e, d)
	bearB := d.Options[idx].Obj
	submitChoices(t, e, idx)
	drainKodama(t, e, kid, 1)
	if got := kodamaTriggerPushes(t, e, kid); got != 1 {
		t.Fatalf("Kodama trigger pushed %d times, want exactly 1 (the placement must not re-fire)", got)
	}
	if kodamaPendingTriggers(e, kid) != 0 {
		t.Fatalf("a Kodama trigger is still queued after the placement: re-fired")
	}
	if o := e.G.Obj(bearB); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the placed bear is not on the battlefield: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// TestKodamaNotThisAbilityLaterEntryStillFires is the over-reach guard: the
// exclusion is per-cause, not a blanket silence. A LATER, unrelated entry
// (the third bear, cast after the first resolution finished) must still fire
// the trigger -- its Execute poses its own hand-move ask again, which the
// test declines (Min 0).
func TestKodamaNotThisAbilityLaterEntryStillFires(t *testing.T) {
	t.Parallel()
	e, cfg, kid := kodamaEngine(t)
	bears := handBears(t, e)
	if len(bears) < 3 {
		t.Fatalf("precondition: want at least three bears in the opening hand, got %v", bears)
	}
	castBearInHand(t, e, bears[0])
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.ResumeKind != "hand_move" {
		t.Fatalf("precondition: want the first hand_move ask, got %+v", d)
	}
	submitChoices(t, e, bearAskOption(t, e, d))
	drainKodama(t, e, kid, 1)
	if got := kodamaTriggerPushes(t, e, kid); got != 1 {
		t.Fatalf("precondition: after the settled placement the count is %d, want 1", got)
	}
	// Now the third bear enters, unrelated to the trigger's own resolution.
	castBearInHand(t, e, bears[2])
	d2 := passUntilNonPriority(t, e, 20)
	if d2 == nil || d2.ResumeKind != "hand_move" {
		t.Fatalf("the later unrelated entry did not re-fire the trigger: got %+v", d2)
	}
	if got := kodamaTriggerPushes(t, e, kid); got != 2 {
		t.Fatalf("Kodama trigger pushed %d times, want 2 (the later unrelated entry must still fire)", got)
	}
	if o := e.G.Obj(bears[2]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the third bear did not enter the battlefield: %+v", o)
	}
	// Decline the follow-up placement (Min 0 makes the empty answer legal):
	// nothing more is placed and nothing re-fires off a declined move.
	submitChoices(t, e)
	drainKodama(t, e, kid, 2)
	if got := kodamaTriggerPushes(t, e, kid); got != 2 {
		t.Fatalf("Kodama trigger pushed %d times after the decline, want 2", got)
	}
	if kodamaPendingTriggers(e, kid) != 0 {
		t.Fatalf("a Kodama trigger is still queued after the declined move: re-fired")
	}
	replayCheck(t, e, cfg)
}
