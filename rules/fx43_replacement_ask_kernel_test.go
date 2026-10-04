package rules

// Kernel-era restorations of the fx43_replacement_ask_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestReplacementMidResolutionAskResumes (kernel era; restored from the
// legacy fx43 pin): Mox Diamond's R: line replaces its own stack->battlefield
// move with "you may discard a land card; if you do, put Mox Diamond onto
// the battlefield, otherwise it goes to your graveyard". The ReplaceWith body
// is a Mode$ TgtChoose discard, a mid-resolution ask inside a REPLACEMENT.
// Drive the real compiled corpus card end to end: the may-discard election
// and the land pick surface while Mox is still on the stack, and answering
// them discards exactly one land and completes the replacement body so Mox
// enters the battlefield, with a log-only replay matching.
func TestReplacementMidResolutionAskResumes(t *testing.T) {
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
