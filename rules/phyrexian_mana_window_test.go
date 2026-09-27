package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const phyrexianBlueWindowProbe = "Name:Gitaxian Window Probe\nManaCost:UP\nTypes:Sorcery\nA:SP$ Draw | Num$ 1\nOracle:x\n"
const phyrexianBlueWindowIsland = "Name:Window Island\nTypes:Basic Land Island\nOracle:x\n"

// TestPhyrexianBlueFaceCanUseTheCastManaWindow pins CR 601.2b's ordering:
// choosing the blue face happens before the player activates their Island in
// the CR 601.2g payment window.  The choice must consequently offer both
// {U} and the two-life Phyrexian face while the Island is still untapped.
func TestPhyrexianBlueFaceCanUseTheCastManaWindow(t *testing.T) {
	e, cfg, probe := newFixtureDeck(t, 881, phyrexianBlueWindowProbe, phyrexianBlueWindowIsland)
	island := putCreature(t, e, 0, phyrexianBlueWindowIsland)
	e.priorityRound()

	cast := castByName(t, e, 0, "Gitaxian Window Probe")
	if cast == nil {
		t.Fatal("Gitaxian Window Probe was not offered with an untapped Island")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Phyrexian face decision = %+v", d)
	}
	blue, life := -1, -1
	for _, option := range d.Options {
		switch option.Kind {
		case "pay_U":
			blue = option.Index
		case "pay_life":
			life = option.Index
		}
	}
	if blue < 0 || life < 0 {
		t.Fatalf("Phyrexian blue decision = %+v, want both pay_U and pay_life", d.Options)
	}
	submitChoices(t, e, blue)

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("mana window after choosing blue = %+v", d)
	}
	activate := -1
	for _, option := range d.Options {
		if option.Kind == "activate" && option.Obj == island {
			activate = option.Index
			break
		}
	}
	if activate < 0 {
		t.Fatalf("mana window omitted Island: %+v", d.Options)
	}
	submitChoices(t, e, activate)
	d = e.Pending()
	// The window closes itself once the newly produced blue mana pays the
	// selected face; there is no redundant Done ask after a sufficient tap.
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority after Island pays the blue face = %+v", d)
	}
	if o := e.G.Obj(probe); o == nil || o.Zone != state.ZStack {
		t.Fatalf("Probe after paying blue = %+v, want stack", o)
	}
	if o := e.G.Obj(island); o == nil || !o.Tapped {
		t.Fatalf("Island after paying blue = %+v, want tapped", o)
	}
	if got := e.G.Players[0].Pool[state.MU]; got != 0 {
		t.Fatalf("blue pool after payment = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}
