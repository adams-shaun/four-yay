package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// D1 (verdict rows Broodheart Engine / Essence Anchor / Morcant's Eyes,
// activate#0.0): a card placed on the battlefield in setup fires its
// "At the beginning of your upkeep, surveil 1" phase trigger while the setup
// drive walks turn 1. XMage's unscripted default for the arrange ask sends
// the looked-at card to the graveyard (its doSurveil queue holds the
// to-graveyard cards and delegates to the computer player), so gorge's
// setup-drive fallback must choose the EMPTY set, not keep the card on top.
func TestSetupDriveSurveilArrangeGoesToGraveyard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Broodheart Engine")
	if !ok {
		t.Fatal("precondition: Broodheart Engine missing from corpus")
	}
	hasSurveilUpkeep := false
	for _, f := range card.Faces {
		for _, tr := range f.Triggers {
			if tr.Effect != nil && tr.Effect.API == "Surveil" {
				hasSurveilUpkeep = true
			}
		}
	}
	if !hasSurveilUpkeep {
		t.Fatal("precondition: Broodheart Engine has no LookAndArrange trigger in the corpus (re-pin this test)")
	}

	// Library pad: the Wastes filler supplies the card the upkeep surveil
	// looks at.
	res, _ := runFixtureScenario(t, `{"name":"surveilsetup","setup":{"p0":{"battlefield":["Broodheart Engine"],"graveyard":["Grizzly Bears"],"library":["Wastes","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	if snap.Checkpoint != "setup" {
		t.Fatalf("precondition: checkpoint = %s, want setup", snap.Checkpoint)
	}
	if setupPermCount(snap, "Broodheart Engine") != 1 {
		t.Fatalf("precondition: Broodheart Engine not on the battlefield at setup: %+v", snap.Permanents)
	}
	fallbackRan := false
	for _, l := range res.Transcript {
		if strings.Contains(l, "setup fallback") && strings.Contains(l, "arrange") {
			fallbackRan = true
			if !strings.Contains(l, "-> []") {
				t.Errorf("setup arrange fallback chose %s, want the empty set (every looked-at card to the graveyard)", l)
			}
		}
	}
	if !fallbackRan {
		t.Fatal("no setup arrange fallback in the transcript — the upkeep surveil never asked; the fixture is wrong")
	}
	gy := snap.Players[0].Graveyard
	if len(gy) != 2 || gy[0] != "Grizzly Bears" || gy[1] != "Wastes" {
		t.Errorf("p0 graveyard = %v, want [Grizzly Bears Wastes] (XMage's unscripted default put the looked-at Wastes there)", gy)
	}
	// Control: the SAME deck with the Engine in p0's library instead of on
	// the battlefield — no upkeep surveil ask, no Engine placement. It loses
	// exactly one library card (the setup Grizzly Bears to the graveyard)
	// and keeps the Wastes out of the graveyard. The compared values must
	// differ, or the assertions above are vacuous.
	res, _ = runFixtureScenario(t, `{"name":"surveilcontrol","setup":{"p0":{"graveyard":["Grizzly Bears"],"library":["Broodheart Engine","Wastes"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	ctl := res.Snapshots[0]
	if ctl.Checkpoint != "setup" {
		t.Fatalf("precondition: control checkpoint = %s, want setup", ctl.Checkpoint)
	}
	if ctlGy := ctl.Players[0].Graveyard; len(ctlGy) != 1 || ctlGy[0] != "Grizzly Bears" {
		t.Errorf("control p0 graveyard = %v, want [Grizzly Bears]", ctlGy)
	}
	if got, want := snap.Players[0].LibraryCount, ctl.Players[0].LibraryCount; got != want-2 {
		t.Errorf("p0 library count = %d, want %d (Engine to the battlefield plus one Wastes out through the surveil)", got, want-2)
	}
}
