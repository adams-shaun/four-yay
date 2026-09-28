package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A PutCounter emitted by an Updated Moved replacement body is a fresh
// placement, even while the outer Moved replacement is resolving.
func TestReplacementBodyCounterReceivesAddCounterReplacement(t *testing.T) {
	t.Parallel()
	cre := entryCounterEtbCreature(t)
	e, cfg := tokenReplGame(t, 170, cre)
	id := moveSeededCard(t, e, 0, cre, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
		t.Fatalf("precondition: body must place 2 counters on battlefield: %+v", o)
	}
	replayCheck(t, e, cfg)

	vori := tokenReplCorpusCard(t, "Vorinclex, Monstrous Raider")
	e, cfg = tokenReplGame(t, 171, vori, cre)
	v := moveSeededCard(t, e, 0, vori, state.ZBattlefield)
	if o := e.G.Obj(v); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Vorinclex is not on the battlefield")
	}
	e.SetCounterAdder(0)
	id = moveSeededCard(t, e, 0, cre, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: recipient not on battlefield: %+v", o)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != 4 {
		t.Fatalf("replacement-body counter = %d, want 4 (2 doubled by Vorinclex)", got)
	}
	replayCheck(t, e, cfg)
}
