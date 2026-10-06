package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const adamantServantSrc = "Name:Adamant Servant\nManaCost:2 G\nTypes:Creature Artifact\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Adamant$ Any | Execute$ TrigDraw | TriggerDescription$ Adamant — draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You\nOracle:x\n"

func TestAdamantTriggerParamGatesOnColourThreshold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		mana     string
		green    int32
		red      int32
		wantDraw bool
	}{
		{"threeSameColour", "GGG", 3, 0, true},
		{"splitColours", "GGR", 2, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, find := etbConfig(t, 115, []string{adamantServantSrc}, nil)
			id := find("Adamant Servant", 0)
			addMana(t, e, 0, tc.mana)
			handBefore := len(e.G.Zone(state.ZHand, 0))
			castFirst(t, e, "cast")
			passUntilStackEmpty(t, e, 20)

			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: zone=%s, want battlefield", o.Zone)
			}
			if got := o.ManaColorSpent[state.MG]; got != tc.green {
				t.Fatalf("precondition: green spent = %d, want %d (mana %q)", got, tc.green, tc.mana)
			}
			if got := o.ManaColorSpent[state.MR]; got != tc.red {
				t.Fatalf("precondition: red spent = %d, want %d (mana %q)", got, tc.red, tc.mana)
			}
			wantHand := handBefore - 1
			if tc.wantDraw {
				wantHand++
			}
			if got := len(e.G.Zone(state.ZHand, 0)); got != wantHand {
				t.Fatalf("hand size = %d, want %d (spent G=%d R=%d)", got, wantHand,
					o.ManaColorSpent[state.MG], o.ManaColorSpent[state.MR])
			}
			replayCheck(t, e, cfg)
		})
	}
}
