package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task fx43: does a mid-resolution ask inside a REPLACEMENT resume at all?
//
// Mox Diamond is the sharpest of the four corpus cards whose ReplaceWith$
// body is itself a discard asker (the others are Chains of Mephistopheles,
// Magus of the Chains and Indominus Rex Alpha). Its R: line intercepts its
// own stack->battlefield Move and replaces it with "you may discard a land
// card; if you do, put Mox Diamond onto the battlefield, otherwise it goes
// to your graveyard". The ReplaceWith body (SVar:PayBeforeETB) is a
// Mode$ TgtChoose discard -- a mid-resolution KModes ask -- and that choice
// is exactly what decides whether Mox lands on the battlefield or in the
// graveyard.
//
// This drives a REAL compiled corpus card end to end through the cast and
// resolve path with no synthetic SA. The correct outcome is: the discard ask
// surfaces while Mox is still on the stack; answering it discards ONE land
// and completes the replacement's own body so Mox enters the battlefield.
//
// Current behaviour is BROKEN (the reason this test exists and the diagnosis
// this task was asked to establish):
//
//  1. The ask IS posed (a suspended-resolution KModes "discard" decision
//     surfaces) -- but by the time it is pending, Mox has ALREADY been moved
//     off the stack. The ensureLeftTheStack guard (rules/stack.go) treats a
//     SUSPENDED replacement -- its event already marked handled, its body
//     still awaiting the discard answer -- as a replacement that "fully
//     replaced the move without relocating the object", so it parks Mox in
//     its resting zone (the graveyard) before the answer can resume the body.
//  2. The pending answer then no-ops: resumeResolution (rules/resolution.go)
//     finds the object already off the stack and running the suspended
//     sub-ability would have nothing left to act on, so the replacement's
//     remaining body (discard the land, then MoveToBattlefield) never runs and
//     no land is ever discarded.
//  3. Separately, even if the parking were held off, applyingReplacement does
//     not survive the suspension: applyReplacements resets it to false when
//     the suspended body returns, so when the resumed body emits its own
//     stack->battlefield completion move it is re-intercepted by the SAME
//     replacement and re-poses the discard (measured: on Mox the replacement
//     body re-enters and the discard is posed repeatedly).
//
// The regression pin below is on the UNFIXED behaviour: it
// asserts the correct outcome (Mox on the battlefield, one land discarded,
// Mox still on the stack while its discard is pending). Any one of these
// assertions failing is the task's answer -- the fix has to make
// ensureLeftTheStack defer the park while suspended and thread the
// replacement context across the resume (rules/resolution.go).
//
// fx44 (the fix it pins) now makes all of this pass: ensureLeftTheStack
// defers the park while a replacement is suspended (the object stays on the
// stack for its answer), and the resume threads the replacement context --
// applyingReplacement (so the completed move is not re-intercepted by the
// same replacement) plus Replaced/Remembered (so the body's Defined$
// ReplacedCard and SVar:X Remembered$Amount gating find their subject) --
// back across the suspension. The guard below is therefore removed and the
// test joins the ordinary suite.
func TestReplacementMidResolutionAskResumes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 42, Tokens: reg.Tokens}
	cfg.Names = []string{"caster", "opponent"}
	cfg.Decks = [][]*cards.Card{
		append(append([]*cards.Card{}, testutil.RepoDeck(t, reg, "ur-delver")...), lookupCard(t, reg, "Mox Diamond")),
		testutil.RepoDeck(t, reg, "death-n-taxes"),
	}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	mox := crAbortMove(t, e, 0, "Mox Diamond", state.ZHand)
	// Give seat 0 a couple of lands in hand so the discard is a real choice:
	// Mode$ TgtChoose only poses its ask when there are STRICTLY more
	// DiscardValid$-eligible cards (here Land) than NumCards$ (default 1), so
	// a single land would be discarded with no question and never exercise
	// the suspended-resolution path.
	seedLands(t, e, 0, 3)

	e.askPriority(0)
	crAbortAnswer(t, e, "Mox Diamond", crAbortOption(t, e, "Mox Diamond", "cast", mox))

	// Drain priority until the replacement's mid-resolution discard surfaces.
	d := drainToSuspendedAsk(t, e, 40)
	if d == nil {
		t.Fatalf("Mox Diamond's ETB replacement never posed its discard ask " +
			"(no suspended-resolution KModes decision reached)")
	}
	// Optional$ True: the may-discard election comes first; answer yes and
	// the land pick is the next suspended ask.
	if d.ResumeKind != "discard_may" {
		t.Fatalf("suspended ask ResumeKind = %q, want \"discard_may\"", d.ResumeKind)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("answer the may-discard election: %v", err)
	}
	d = e.Pending()
	if d == nil || d.ResumeKind != "discard" {
		t.Fatalf("after electing to discard got %+v, want the land pick", d)
	}
	if !e.Suspended() {
		t.Fatalf("replacement posed an ask but e.Suspended() is false -- the resume " +
			"machinery was not engaged")
	}

	// The ask must be posed while the object it is replacing is still on the
	// stack -- the whole point of a mid-resolution replacement resume is that
	// the choice decides where the object goes. BUG: Mox is already parked off
	// the stack here (the ensureLeftTheStack guard fired during the suspension).
	if len(e.G.Stack) == 0 || e.G.Stack[len(e.G.Stack)-1] != mox {
		t.Fatalf("Mox not on top of the stack while its discard is pending: stack=%v -- "+
			"the replacement's ask is orphaned (the object was moved off the stack "+
			"before the answer could resume its body)", e.G.Stack)
	}
	if e.G.Obj(mox).Zone != state.ZStack {
		t.Fatalf("Mox zone = %s, want stack while its discard is pending", e.G.Obj(mox).Zone)
	}

	// Find the land the ask is offering and answer it.
	var landID state.ObjID
	for _, opt := range d.Options {
		if opt.Kind == "discard" && opt.Obj != 0 {
			landID = opt.Obj
			break
		}
	}
	if landID == 0 {
		t.Fatalf("discard ask offered no land options: %+v", d.Options)
	}
	landName := e.G.Obj(landID).Face().Name
	crAbortAnswer(t, e, "Mox Diamond", crAbortOption(t, e, "Mox Diamond", "discard", landID))

	// The discard branch of Mox's replacement should put Mox on the
	// battlefield and take exactly one land to the graveyard.
	if e.G.Obj(mox).Zone != state.ZBattlefield {
		t.Fatalf("answered a discard but Mox zone = %s, want battlefield -- the replacement "+
			"body did not complete after its mid-resolution ask", e.G.Obj(mox).Zone)
	}
	if e.G.Obj(landID).Zone != state.ZGraveyard {
		t.Fatalf("discarded land %s zone = %s, want graveyard -- the answer did not actually "+
			"discard a card", landName, e.G.Obj(landID).Zone)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after Mox resolved, want empty", e.G.Stack)
	}
	landCount := 0
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil &&
			strings.Contains(strings.Join(o.Face().Types, " "), "Land") {
			landCount++
		}
	}
	if landCount != 1 {
		t.Fatalf("seat 0 ended with %d lands in the graveyard, want exactly 1 -- "+
			"Mox Diamond's replacement must discard exactly one land", landCount)
	}

	// T21-e: the same event log must replay to the identical Game. If the
	// discard answer resumed (or the parked move emitted) through a path that
	// mutated state without emitting an event, a log-only fold would diverge.
	fresh := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, fresh); diff != "" {
		t.Fatalf("log-only replay diverges for Mox Diamond's mid-resolution replacement:\n%s", diff)
	}
}

// lookupCard fetches one compiled corpus card as a deck supplement.
func lookupCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("missing corpus card %s", name)
	}
	return c
}

// drainToSuspendedAsk passes every priority decision until a non-priority,
// suspended-resolution decision (the replacement's mid-resolution ask)
// surfaces, or the bound is exhausted. It returns nil if none did.
func drainToSuspendedAsk(t *testing.T, e *Engine, bound int) *decision.Decision {
	t.Helper()
	for i := 0; i < bound; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind != decision.KPriority {
			return d
		}
		crAbortAnswer(t, e, "drain", crAbortOption(t, e, "drain", "pass", 0))
	}
	return nil
}

// seedLands moves up to n land cards from seat p's library to its hand.
func seedLands(t *testing.T, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	moved := 0
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZLibrary, p)...) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || !strings.Contains(strings.Join(o.Face().Types, " "), "Land") {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		moved++
		if moved >= n {
			return
		}
	}
	if moved < n {
		t.Fatalf("seedLands: only %d lands available in seat %d's library, want %d", moved, p, n)
	}
}
