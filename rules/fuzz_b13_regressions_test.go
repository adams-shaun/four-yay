package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the cardfuzz batch13 rejected-intent failure (all real
// corpus cards).

// TestTanukiPersistentArtifactManaSpendMovesTheTally (cardfuzz batch13 line
// 1): Tanuki Transplanter is an artifact, so its attack trigger's
// "PersistentMana$ True" {G} lands in the pool as TYPED artifact mana
// ("ArtifactG") that is also persistent. The payment path attributed only
// the PLAIN share of a spend ordinary-first and emitted the typed share
// without the " pm" marker, so spending the Tanuki mana emptied the pool but
// left Player.PersistentMana at its old count. The next payment read the
// visible persistent share as larger than the pool -- a NEGATIVE ordinary
// share -- and charged an ordinary {G} as two marked persistent units,
// driving the pool to -1. In the fuzz game the green seat's attackBudget
// then summed that pool to {-1} and rejected a free ({0}) attack
// declaration ("declaration's attack cost {0} exceeds the affordable
// {-1}").
func TestTanukiPersistentArtifactManaSpendMovesTheTally(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Tanuki Transplanter", "Giant Growth", "Giant Growth", "Giant Growth")
	tanuki := searchMoveByName(t, e, "Tanuki Transplanter", state.ZBattlefield)
	// A logged TurnChange clears summoning sickness (CR 302.6) the
	// replayable way, then the clock is parked in main phase 1.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	e.priorityRound()
	driveToAttackers(t, e)
	d := e.Pending()
	attack := -1
	for _, o := range d.Options {
		if o.Obj == tanuki && o.Player == 1 {
			attack = o.Index
		}
	}
	if attack < 0 {
		t.Fatalf("Tanuki Transplanter cannot attack seat 1: %+v", d.Options)
	}
	submitChoices(t, e, attack)
	driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })

	pl := &e.G.Players[0]
	// Precondition the defect rides on: the trigger's two {G} are both
	// persistent AND artifact-typed.
	if pl.Pool[state.MG] != 2 || pl.PersistentMana[state.MG] != 2 ||
		pl.TypedMana[state.TypedArtifact][state.MG] != 2 {
		t.Fatalf("after Tanuki's trigger pool=%v persistent=%v artifact-typed=%v, want 2 persistent artifact {G}",
			pl.Pool, pl.PersistentMana, pl.TypedMana[state.TypedArtifact])
	}

	castGrowth := func() {
		t.Helper()
		gg := searchMoveByName(t, e, "Giant Growth", state.ZHand)
		submitChoices(t, e, castOptionFor(t, e, gg).Index)
		driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })
		if z := e.G.Obj(gg).Zone; z != state.ZGraveyard {
			t.Fatalf("Giant Growth rests in %v, want graveyard (cast and resolved)", z)
		}
	}
	// Each spend of a persistent typed unit moves the tally with it.
	castGrowth()
	if pl.Pool[state.MG] != 1 || pl.PersistentMana[state.MG] != 1 {
		t.Fatalf("after one {G} spend pool=%d persistent=%d, want 1/1", pl.Pool[state.MG], pl.PersistentMana[state.MG])
	}
	castGrowth()
	if pl.Pool[state.MG] != 0 || pl.PersistentMana[state.MG] != 0 {
		t.Fatalf("after two {G} spends pool=%d persistent=%d, want 0/0", pl.Pool[state.MG], pl.PersistentMana[state.MG])
	}
	// The fuzz symptom: an ordinary {G} spent afterwards must leave an empty
	// pool, never a negative one.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.pending = nil
	e.priorityRound()
	castGrowth()
	for i, n := range pl.Pool {
		if n < 0 {
			t.Fatalf("pool slot %d is %d after an ordinary spend (pool %v, persistent %v)", i, n, pl.Pool, pl.PersistentMana)
		}
	}
	if pl.Pool[state.MG] != 0 || pl.PersistentMana[state.MG] != 0 {
		t.Fatalf("after the ordinary {G} spend pool=%d persistent=%d, want 0/0", pl.Pool[state.MG], pl.PersistentMana[state.MG])
	}
	if b := e.attackBudget(0); b < 0 {
		t.Fatalf("attackBudget = %d, want non-negative", b)
	}
	replayCheck(t, e, cfg)
}
