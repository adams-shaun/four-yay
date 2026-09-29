package effects

// api:SetLife (CR 119.5) is the primitive behind The Endstone's end-step
// "your life total becomes half your starting life total, rounded up" and 34
// other corpus carriers. The rules-level test
// (rules/setaudit_eoe_test.go) drives the real card end to end; this file
// pins the primitive's own grammar at the effects level with the shapes the
// corpus writes: a Count$-backed amount set on the controller, a literal
// amount set on every player, and an unresolvable amount that must fail
// CLOSED (a Note, never a silent set-to-zero that loses the game).

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestSetLifeCountAmountLowersToHalfStartingLife is the Endstone grammar:
// `Defined$ You | LifeAmount$ X` with `X: Count$YourStartingLife/HalfUp`.
// Both the precondition (life is above the target, so the set is a real
// loss) and the target are asserted, so a silently-defaulted amount cannot
// pass.
func TestSetLifeCountAmountLowersToHalfStartingLife(t *testing.T) {
	h := newHost(t, 2)
	h.startingLife = 20
	h.g.Players[0].Life = 20
	h.g.Players[1].Life = 18
	// Precondition: the controller is strictly above the intended target.
	if h.g.Players[0].Life <= h.startingLife/2 {
		t.Fatalf("precondition: life %d is not above its half", h.g.Players[0].Life)
	}

	src := sa(t, "DB$ SetLife | Defined$ You | LifeAmount$ X")
	ctx := &Ctx{Controller: 0, SVars: map[string]string{"X": "Count$YourStartingLife/HalfUp"}}
	Resolve(h, ctx, src)

	if got := h.g.Players[0].Life; got != 10 {
		t.Fatalf("controller life = %d, want 10 (half of 20 rounded up)", got)
	}
	// Only the controller is affected: Defined$ You must not leak to seat 1.
	if got := h.g.Players[1].Life; got != 18 {
		t.Fatalf("opponent life = %d, want 18 (Defined$ You touched another seat)", got)
	}
}

// TestSetLifeLiteralAmountSetsEveryPlayer drives `Defined$ Player` with a
// literal amount across two seats at different totals, so both a loss and a
// gain are exercised in one resolution: 18 -> 1 is a loss, 0 -> 1 is a gain.
func TestSetLifeLiteralAmountSetsEveryPlayer(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 18
	h.g.Players[1].Life = 0
	// Precondition: the two seats differ from the target and from each
	// other, so a no-op and a both-same bug are distinguishable.
	if h.g.Players[0].Life == 1 || h.g.Players[1].Life == 1 || h.g.Players[0].Life == h.g.Players[1].Life {
		t.Fatal("precondition: seats must differ from the target 1 and from each other")
	}

	src := sa(t, "DB$ SetLife | Defined$ Player | LifeAmount$ 1")
	Resolve(h, &Ctx{Controller: 0}, src)

	if h.g.Players[0].Life != 1 || h.g.Players[1].Life != 1 {
		t.Fatalf("lives = (%d, %d), want (1, 1)", h.g.Players[0].Life, h.g.Players[1].Life)
	}
}

// TestSetLifeUnresolvableAmountFailsClosed is the loud-degrade contract: an
// amount this build cannot price must leave life untouched and say so. The
// Note's exact text proves the SetLife handler RAN -- reverting the
// registration would leave an "unimplemented API" note instead, so this test
// cannot pass with the primitive removed.
func TestSetLifeUnresolvableAmountFailsClosed(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 7
	// Precondition: life is not already at the reserved-for-degrade value.
	if h.g.Players[0].Life == 0 {
		t.Fatal("precondition: life must be non-zero to detect a silent set-to-zero")
	}

	src := sa(t, "DB$ SetLife | Defined$ You | LifeAmount$ NotAnSVar")
	Resolve(h, &Ctx{Controller: 0}, src)

	if got := h.g.Players[0].Life; got != 7 {
		t.Fatalf("life = %d, want 7 (an unresolvable amount must not change life)", got)
	}
	var sawNote, sawLifeChange bool
	for _, e := range h.log {
		if e.Kind == events.Note && e.Text == "SetLife: unresolvable LifeAmount" {
			sawNote = true
		}
		if e.Kind == events.LifeChange {
			sawLifeChange = true
		}
	}
	if !sawNote {
		t.Fatalf("no SetLife unresolvable Note; log = %+v (did the handler run?)", h.log)
	}
	if sawLifeChange {
		t.Fatalf("a LifeChange was emitted for an unresolvable amount: %+v", h.log)
	}
}

// TestSetLifeZeroDeltaEmitsNothing pins CR 119.5's "gains or loses the
// appropriate amount" -- when the total already equals the target that
// amount is zero, so no life event is logged (which also keeps a zero
// LifeChange from reaching the life-replacement machinery).
func TestSetLifeZeroDeltaEmitsNothing(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 5
	// Precondition: the seat is already at the target, so the delta is 0.
	if h.g.Players[0].Life != 5 {
		t.Fatal("precondition: life must equal the target for a zero delta")
	}

	src := sa(t, "DB$ SetLife | Defined$ You | LifeAmount$ 5")
	Resolve(h, &Ctx{Controller: 0}, src)

	if got := h.g.Players[0].Life; got != 5 {
		t.Fatalf("life = %d, want 5", got)
	}
	for _, e := range h.log {
		if e.Kind == events.LifeChange {
			t.Fatalf("zero-delta SetLife emitted a LifeChange: %+v", e)
		}
	}
}

// TestSetLifeDefinedOpponentSetseeksTheOpponent exercises the
// `Defined$ Opponent` selector through the shared referent grammar, so the
// primitive is reached by a selector rather than only by the default
// controller. `Show the Weakness` uses the `Defined$ Player.Opponent`
// spelling of the same set.
func TestSetLifeDefinedOpponentSetseeksTheOpponent(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 3
	h.g.Players[1].Life = 9
	// Precondition: the opponent's total differs from the target and from
	// the controller's, so a leaked target is distinguishable.
	if h.g.Players[1].Life == 4 || h.g.Players[0].Life == h.g.Players[1].Life {
		t.Fatal("precondition: opponent life must differ from 4 and from the controller's")
	}

	src := sa(t, "DB$ SetLife | Defined$ Opponent | LifeAmount$ 4")
	Resolve(h, &Ctx{Controller: 0}, src)

	if got := h.g.Players[1].Life; got != 4 {
		t.Fatalf("opponent life = %d, want 4", got)
	}
	if got := h.g.Players[0].Life; got != 3 {
		t.Fatalf("controller life = %d, want 3 (the set leaked to the wrong seat)", got)
	}
}
