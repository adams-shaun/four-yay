package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// flashSpellUnlessFixture casts the real corpus Flash ("put a creature card from your
// hand onto the battlefield; sacrifice it unless you pay its mana cost reduced
// by {2}") with Hill Giant ({3}{R}: the unless cost is {1}{R}) in hand, picks
// the giant at the ChangeZone ask, and returns the engine at the unless-pay
// ask together with the giant's id.
func flashSpellUnlessFixture(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Flash"), mustCorpusCard(t, reg, "Hill Giant"))
	ids := handIDsByFace(e)
	flash, giant := ids["Flash"], ids["Hill Giant"]
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Players[0].Pool[state.MR] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, flash))
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision before the unless-pay ask")
		}
		switch {
		case d.Kind == decision.KModes && d.ResumeKind == "unless_pay":
			return e, giant
		case d.Kind == decision.KPriority:
			castFirst(t, e, "pass")
		case d.Kind == decision.KChoose || d.Kind == decision.KTarget:
			picked := -1
			for _, o := range d.Options {
				if o.Obj == giant {
					picked = o.Index
				}
			}
			if picked < 0 {
				t.Fatalf("Hill Giant not offered: %+v", d.Options)
			}
			submitChoices(t, e, picked)
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
	}
	t.Fatal("Flash never posed its unless-pay ask")
	return nil, 0
}

// assertNoReask fails when the answered unless-pay ask is pending again.
func assertNoReask(t *testing.T, e *Engine) {
	t.Helper()
	if d := e.Pending(); d != nil && d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
		t.Fatalf("the answered unless-pay ask was re-posed: %+v", d)
	}
}

// TestFlashUnlessDeclineSacrificesOnce is the cardfuzz batch1 livelock
// (lines 2/21/4): SacrificeAll's body posed its OWN unless ask after the
// shared gate (effects.unlessProceed) had already asked, consumed and
// cleared the answer, so every answered re-entry asked again forever. A
// decline must sacrifice the creature and end the resolution.
func TestFlashUnlessDeclineSacrificesOnce(t *testing.T) {
	t.Parallel()
	e, giant := flashSpellUnlessFixture(t)
	answerUnlessPay(t, e, false)
	assertNoReask(t, e)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(giant).Zone; z != state.ZGraveyard {
		t.Fatalf("declined Flash: Hill Giant zone = %s, want Graveyard", z)
	}
}

// TestFlashUnlessPayKeepsCreature: paying {1}{R} keeps the creature, charges
// the pool, and poses no second ask.
func TestFlashUnlessPayKeepsCreature(t *testing.T) {
	t.Parallel()
	e, giant := flashSpellUnlessFixture(t)
	answerUnlessPay(t, e, true)
	assertNoReask(t, e)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(giant).Zone; z != state.ZBattlefield {
		t.Fatalf("paid Flash: Hill Giant zone = %s, want Battlefield", z)
	}
	if got := e.G.Players[0].Pool; got != (state.Mana{}) {
		t.Fatalf("pool left = %v, want empty ({1}{U} for Flash, {1}{R} for the giant)", got)
	}
}
