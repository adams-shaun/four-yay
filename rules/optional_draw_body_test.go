package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func optionalDrawBodyFixture(t *testing.T) (*Engine, state.ObjID, int, int) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := drawReplEngine(t, 1743)
	pursuit := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Pursuit of Knowledge"))
	if o := e.G.Obj(pursuit); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Pursuit of Knowledge is not on the battlefield")
	}
	setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Mountain"))
	if got := len(e.G.Zone(state.ZLibrary, 0)); got == 0 {
		t.Fatal("precondition: seat 0 library is empty")
	}
	if got := e.G.Obj(pursuit).Counter("STUDY"); got != 0 {
		t.Fatalf("precondition: Pursuit already has %d study counters", got)
	}
	hand, draws := len(e.G.Zone(state.ZHand, 0)), countDraw(e)
	e.pending = nil
	emitDraw(t, e, 0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
		t.Fatalf("pending decision = %+v, want seat 0 KReplacement", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
		t.Fatalf("replacement options = %+v, want apply/decline", d.Options)
	}
	return e, pursuit, hand, draws
}

func TestOptionalDrawReplacementWithBodyAsksAndResumes(t *testing.T) {
	t.Parallel()
	e, pursuit, handBefore, drawsBefore := optionalDrawBodyFixture(t)
	submitChoices(t, e, 0)
	if got := e.G.Obj(pursuit).Counter("STUDY"); got != 1 {
		t.Fatalf("study counters after accepting = %d, want 1", got)
	}
	if got := countDraw(e) - drawsBefore; got != 0 {
		t.Fatalf("draws after accepting = %d, want 0", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("hand size after accepting = %d, want %d", got, handBefore)
	}
}

func TestOptionalDrawReplacementWithBodyDeclineDraws(t *testing.T) {
	t.Parallel()
	e, pursuit, handBefore, drawsBefore := optionalDrawBodyFixture(t)
	submitChoices(t, e, 1)
	if got := e.G.Obj(pursuit).Counter("STUDY"); got != 0 {
		t.Fatalf("study counters after declining = %d, want 0", got)
	}
	if got := countDraw(e) - drawsBefore; got != 1 {
		t.Fatalf("draws after declining = %d, want 1", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand size after declining = %d, want %d", got, handBefore+1)
	}
}
