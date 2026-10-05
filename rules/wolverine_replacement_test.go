package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestWolverineReplacementDamageStands is the real-corpus behavioural test
// for "If damage would be dealt to Wolverine, instead that damage is dealt,
// but all other damage already dealt to him is healed."
//
// The card's DamageDone replacement carries ReplacementResult$ Updated, so
// CR 616.1 makes it an event modifier: the incoming damage is still dealt.
// effects/heal.go's HealDamage body does not rewrite the held event (it emits
// its own negative Damage for the pre-existing marked damage), so before the
// fix applyNonMoveReplacements returned the event as fully handled and the
// incoming amount never folded -- a Wolverine with no marked damage took no
// damage ever.
func TestWolverineReplacementDamageStands(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	w := onBoardCard(t, e, 0, corpusCard(t, "Wolverine, Fierce Fighter"))

	// Precondition: the replacement's ActiveZones$ Battlefield scope reads a
	// battlefield permanent, and a vacuous setup (not on the battlefield, or
	// already damaged) would let a wrong assertion pass.
	if got := e.G.Obj(w).Zone; got != state.ZBattlefield {
		t.Fatalf("Wolverine zone = %v, want battlefield", got)
	}
	if got := e.G.Obj(w).Damage; got != 0 {
		t.Fatalf("Wolverine damage before = %d, want 0", got)
	}

	e.emit(events.Event{Kind: events.Damage, Obj: w, Amount: 2})

	if got := e.G.Obj(w).Damage; got != 2 {
		t.Fatalf("fresh Wolverine damage after a 2-damage hit = %d, want 2 (the hit is dealt, ReplacementResult$ Updated)", got)
	}
}

// TestWolverineHealsOldDamageKeepsNew is the second half: pre-existing marked
// damage is healed while the incoming damage stays, and the heal really fired
// (so the test cannot pass by the replacement never running).
func TestWolverineHealsOldDamageKeepsNew(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	w := onBoardCard(t, e, 0, corpusCard(t, "Wolverine, Fierce Fighter"))

	// Precondition: the replacement's ActiveZones$ Battlefield scope reads a
	// battlefield permanent; an off-battlefield Wolverine would make the
	// replacement never fire and the assertion below vacuous.
	if got := e.G.Obj(w).Zone; got != state.ZBattlefield {
		t.Fatalf("Wolverine zone = %v, want battlefield", got)
	}

	// Seed 3 marked damage as setup, then read it back so the test cannot
	// pass on a seed that did not land.
	e.G.Obj(w).Damage = 3
	if got := e.G.Obj(w).Damage; got != 3 {
		t.Fatalf("Wolverine seeded damage = %d, want 3", got)
	}

	e.emit(events.Event{Kind: events.Damage, Obj: w, Amount: 2})

	if got := e.G.Obj(w).Damage; got != 2 {
		t.Fatalf("Wolverine damage after heal-then-hit = %d, want 2 (3 healed, 2 dealt)", got)
	}
	healed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == w && ev.Amount < 0 {
			healed = true
			break
		}
	}
	if !healed {
		t.Fatalf("no negative Damage event for Wolverine in the log; the HealDamage body did not fire")
	}
}
