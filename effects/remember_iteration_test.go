package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// rememberIteration is a pure fold, so these are direct unit tests of the
// fold rule: an entry the iteration's body REMOVED must leave the outer
// remembered set, an entry the body ADDED must join it, and the iteration
// subject must not leak into it. The regression this pins is the Thieves'
// Auction livelock: its outer Repeat's RepeatDefined$ Remembered | RepeatPresent$
// Card gate never shrank because the body's ForgetChosen$ removal was dropped
// by the fold, so the Repeat re-ran forever.

func TestRememberIterationDropsEntriesTheBodyForgot(t *testing.T) {
	cardX := state.Target{Obj: 10}
	cardY := state.Target{Obj: 11}
	player := state.Target{Player: 0, IsPlayer: true}
	outer := []state.Target{cardX, cardY}
	base := []state.Target{cardX, cardY}
	// The body started with base+subject and forgot cardX (ForgetChosen$).
	body := []state.Target{cardY, player}

	got := rememberIteration(outer, body, base, player)

	// Precondition: the values under comparison really differ, so a fold that
	// ignored the removal cannot accidentally pass.
	if indexTarget(outer, cardX) < 0 || indexTarget(body, cardX) >= 0 {
		t.Fatalf("precondition: cardX=%+v must be in outer (%v) and absent from body (%v)", cardX, outer, body)
	}
	if indexTarget(got, cardX) >= 0 {
		t.Fatalf("body forgot cardX, but the fold kept it: got %v", got)
	}
	if indexTarget(got, cardY) < 0 {
		t.Fatalf("cardY was untouched by the body, so it must remain: got %v", got)
	}
	if indexTarget(got, player) >= 0 {
		t.Fatalf("the iteration subject is not an addition to the outer set: got %v", got)
	}
}

func TestRememberIterationKeepsBodyAdditions(t *testing.T) {
	cardX := state.Target{Obj: 10}
	added := state.Target{Obj: 12}
	player := state.Target{Player: 0, IsPlayer: true}
	outer := []state.Target{cardX}
	base := []state.Target{cardX}
	body := []state.Target{cardX, player, added}

	got := rememberIteration(outer, body, base, player)

	if indexTarget(got, cardX) < 0 {
		t.Fatalf("cardX untouched by the body must remain: got %v", got)
	}
	if indexTarget(got, added) < 0 {
		t.Fatalf("an entry the body remembered must join the outer set: got %v", got)
	}
}

func TestRememberIterationPlayerLoopBase(t *testing.T) {
	// A player loop's base is the outer set with player entries removed
	// (iterationBase): the body sees the cards and its own subject. Forgetting
	// a card must still remove it from the outer set.
	cardX := state.Target{Obj: 10}
	cardY := state.Target{Obj: 11}
	prevPlayer := state.Target{Player: 1, IsPlayer: true}
	subject := state.Target{Player: 0, IsPlayer: true}
	outer := []state.Target{cardX, cardY, prevPlayer}
	base := []state.Target{cardX, cardY} // iterationBase drops players
	body := []state.Target{cardY, subject}

	got := rememberIteration(outer, body, base, subject)

	if indexTarget(got, cardX) >= 0 {
		t.Fatalf("body forgot cardX: got %v", got)
	}
	if indexTarget(got, cardY) < 0 {
		t.Fatalf("cardY must remain: got %v", got)
	}
	if indexTarget(got, prevPlayer) < 0 {
		t.Fatalf("an outer player entry does not belong to the iteration's base and must remain: got %v", got)
	}
}
