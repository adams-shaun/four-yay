package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestWashOutChosenColor is the requested corpus-backed card-behaviour leaf.
func TestWashOutChosenColor(t *testing.T) {
	t.Parallel()
	runWashOutChosenColor(t)
}

// TestCR608WashOutChosenColor places the same real-card case in the CR lane:
// CR 608.2c resolves instructions in order, choosing the colour before the
// chained ChangeZoneAll effect applies that answer.
func TestCR608WashOutChosenColor(t *testing.T) {
	t.Parallel()
	runWashOutChosenColor(t)
}

func runWashOutChosenColor(t *testing.T) {
	blue := "Name:Blue Test Creature\nManaCost:U\nTypes:Creature\nPT:2/2\nOracle:x\n"
	red := "Name:Red Test Creature\nManaCost:R\nTypes:Creature\nPT:2/2\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 42, corpusCardText(t, "w/wash_out.txt"), blue, red)
	washOut := moveSeeded(t, e, 0, corpusCardText(t, "w/wash_out.txt"), state.ZHand)
	blueID := moveSeeded(t, e, 0, blue, state.ZBattlefield)
	redID := moveSeeded(t, e, 0, red, state.ZBattlefield)
	if len(e.G.Zone(state.ZBattlefield, 0)) < 2 || e.G.Obj(blueID).Zone != state.ZBattlefield || e.G.Obj(redID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: both target permanents must be on the battlefield: blue=%s red=%s", e.G.Obj(blueID).Zone, e.G.Obj(redID).Zone)
	}
	blueColors, redColors := effects.ColorsOf(e.G.Obj(blueID)), effects.ColorsOf(e.G.Obj(redID))
	if blueColors == redColors || !strings.Contains(blueColors, "U") || strings.Contains(redColors, "U") {
		t.Fatalf("precondition: fixtures must be differently colored (blue=%q red=%q)", blueColors, redColors)
	}
	addMana(t, e, 0, "UUUU")
	option := castOptionFor(t, e, washOut)
	submitChoices(t, e, option.Index)
	d := passUntilAsk(t, e)
	if d.Kind != decision.KChoose || d.ResumeKind != "choosecolor" {
		t.Fatalf("expected ChooseColor ask, got %+v", d)
	}
	choice := optionByLabel(d.Options, "Blue")
	if choice < 0 {
		t.Fatalf("Blue missing from Wash Out's color options: %+v", d.Options)
	}
	submitChoices(t, e, choice)
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(blueID).Zone; got != state.ZHand {
		t.Errorf("blue permanent ended in %s, want hand", got)
	}
	if got := e.G.Obj(redID).Zone; got != state.ZBattlefield {
		t.Errorf("red permanent ended in %s, want battlefield", got)
	}
	if got := e.G.Obj(washOut).Zone; got != state.ZGraveyard {
		t.Errorf("Wash Out ended in %s, want graveyard", got)
	}
}
