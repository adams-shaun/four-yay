package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestETBNameCardWithNoVisibleMatchStillAnswerable is the cardfuzz batch1
// line 14 error ("expected 1..1 choices, got 0"): Alpine Moon's "as this
// enters, choose a nonbasic land card name" on a no-universe engine, with no
// nonbasic land anywhere in view, posed a Min 1 / Max 1 name ask with ZERO
// options -- a decision no answer satisfies. The ask must always carry a
// legal answer (the same legacy stand-in effNameCard names mid-resolution),
// and answering it must complete the entry.
func TestETBNameCardWithNoVisibleMatchStillAnswerable(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Alpine Moon"))
	moon := handIDsByFace(e)["Alpine Moon"]
	e.G.Players[0].Pool[state.MR] = 1
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, moon))
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision before Alpine Moon entered")
		}
		if d.Kind == decision.KChoose {
			if len(d.Options) < d.Min || len(d.Options) == 0 {
				t.Fatalf("name ask posed with %d options for Min %d: %+v", len(d.Options), d.Min, d)
			}
			submitChoices(t, e, 0)
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision %+v", d)
		}
		castFirst(t, e, "pass")
	}
	o := e.G.Obj(moon)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("Alpine Moon zone = %s, want Battlefield", o.Zone)
	}
	if o.ChosenName == "" {
		t.Fatal("Alpine Moon entered with no chosen name")
	}
}
