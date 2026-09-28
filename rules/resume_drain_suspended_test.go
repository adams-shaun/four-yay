package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task fx39: pin that resumeTriggerDrain is inert while a resolution is
// suspended. The suspicion being settled: turn.go's drain is guarded on
// `e.pending != nil`, and the question is whether the right guard is instead
// `e.Suspended()` (e.resume != nil). The two are not the same state in
// principle -- a resolution can be suspended with no decision pending for a
// moment, and a decision can be pending with nothing suspended -- but this
// engine wires them together: e.resume is set only inside effects' Ask
// (rules/resolution.go), which also sets e.pending, so a suspended resolution
// ALWAYS carries its own pending ask. That makes the `e.pending != nil` guard
// at least as protective as an `e.Suspended()` guard, and stronger: it also
// stops the drain from re-entering over a NON-suspended pending decision
// (the checkStateBased -> releasePending -> resumeTriggerDrain -> checkStateBased
// cycle, and the "placed triggers behind the answering player's back" bug the
// comment names).
//
// The invariant pinned here is the one that would break silently if the guard
// were weakened: calling resumeTriggerDrain while a resolution is suspended
// must do NOTHING. It must not run checkStateBased or putTriggersOnStack under
// the half-resolved object, must not grant priority to any seat, must not move
// the suspended object off the stack, and must not overwrite the outstanding
// mid-resolution ask. If a future edit removes or inverts the guard and lets
// the drain run while a resolution is sitting suspended on the stack, this test
// fails loudly instead of silently advancing the turn underneath it.
//
// Fixture choice: a REAL corpus card -- Chain Lightning -- whose
// DealDamage->CopySpellAbility chain poses a genuine mid-resolution
// UnlessCost$ ("unless_pay") ask, the repo's established suspended-resolution
// probe (see TestCR608CompletedSpellLeavesStackAfterDepartedPayer and
// TestDepartedChooserResumptionEventStreamIsDeterministic). No manufactured
// SAs; the ask is reached through the real cast/resolve path. Three seats so
// the game keeps running while the ask sits (a two-seat game would end when
// the damage completes).
func TestResumeTriggerDrainIsInertWhileAResolutionIsSuspended(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 42, Tokens: reg.Tokens}
	for _, n := range []string{"ur-delver", "death-n-taxes", "ur-delver"} {
		cfg.Names = append(cfg.Names, n)
		cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, n))
	}
	// Seat 0 is the protagonist; seatZeroStart advances the seed until the
	// CR 103.1 toss starts seat 0. It runs here, after the decks are
	// appended -- on the deck-less Config it would be a no-op (New with no
	// Names draws no toss).
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	chain := crAbortMove(t, e, 0, "Chain Lightning", state.ZHand)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -17})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 2})
	e.askPriority(0)
	crAbortAnswer(t, e, "Chain Lightning", crAbortOption(t, e, "Chain Lightning", "cast", chain))
	crResolutionPlayerTarget(t, e, 1)
	crResolutionRound(t, e)

	// We are standing exactly on the shape under test: a resolution suspended
	// on seat 1's own UnlessCost$ paid-or-copy ask, with the ask itself the
	// pending decision and the Chain Lightning still on the stack.
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" || !e.Suspended() || e.G.Obj(chain).Zone != state.ZStack {
		t.Fatalf("portend the suspended ask: want a pending unless_pay modes decision with Chain Lightning on the stack; "+
			"pending=%+v suspended=%v chainZone=%v", d, e.Suspended(), e.G.Obj(chain).Zone)
	}

	turn, step := e.G.Turn, e.G.Step
	andActive := e.G.Active
	stackLen := len(e.G.Stack)
	start := len(e.L.Events)

	// The invariant: a suspended resolution must keep the drain inert.
	e.resumeTriggerDrain()

	// The outstanding mid-resolution ask must be the SAME decision, untouched.
	if got := e.Pending(); got != d {
		t.Fatalf("the suspended ask was replaced: before=%p after=%p", d, got)
	}
	// The resolution must still be suspended, on the same object.
	if !e.Suspended() {
		t.Fatal("the drain finished the suspended resolution it must not touch")
	}
	if e.G.Obj(chain).Zone != state.ZStack {
		t.Fatalf("chain zone=%v, want still on the stack (the drain moved a half-resolved object)",
			e.G.Obj(chain).Zone)
	}
	if len(e.G.Stack) != stackLen {
		t.Fatalf("stack length = %d, want %d (the drain placed or removed a stack object)", len(e.G.Stack), stackLen)
	}
	// The turn must not have advanced underneath the suspended object.
	if e.G.Turn != turn || e.G.Step != step || e.G.Active != andActive {
		t.Fatalf("turn advanced underneath a suspended resolution: turn %d->%d step %v->%v active %d->%d",
			turn, e.G.Turn, step, e.G.Step, andActive, e.G.Active)
	}
	// Nothing was emitted: the drain ran no work at all.
	if added := len(e.L.Events) - start; added != 0 {
		t.Fatalf("resumeTriggerDrain emitted %d events while a resolution was suspended, want 0", added)
	}
}
