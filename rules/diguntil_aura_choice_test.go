package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestSongbirdsBlessingDigUntilAuraChoice uses the real Songbirds' Blessing
// DigUntil carrier and adds a second eligible creature. It proves the
// answered bearer is applied in place rather than accepting the first battlefield permanent silently.
func TestSongbirdsBlessingDigUntilAuraChoice(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, auraID, _, bearID := songbirdsTestEngine(t, reg)
	second := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if second == bearID {
		t.Fatalf("setup: second Grizzly Bears lookup returned the existing bearer %d", bearID)
	}
	if e.G.Obj(bearID).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatalf("setup: bearers are not both on battlefield: %d=%s %d=%s", bearID, e.G.Obj(bearID).Zone, second, e.G.Obj(second).Zone)
	}

	d := driveSongbirdsToAttack(t, e, bearID)
	submitChoices(t, e, d.Options[0].Index) // accept the optional battlefield move
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "diguntil_aura" {
		t.Fatalf("Aura bearer decision = %+v, want KChoose/diguntil_aura", d)
	}
	if len(d.Options) != 2 || d.Options[0].Obj == d.Options[1].Obj {
		t.Fatalf("Aura bearer options = %+v, want two distinct creatures", d.Options)
	}
	chosen := -1
	for _, option := range d.Options {
		if option.Obj == second {
			chosen = option.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("Aura bearer options = %+v, do not include second bearer %d", d.Options, second)
	}
	submitChoices(t, e, chosen)
	if o := e.G.Obj(auraID); o.Zone != state.ZBattlefield || o.AttachedTo != second {
		t.Fatalf("revealed Aura zone/attachment = %s/%d, want battlefield/%d", o.Zone, o.AttachedTo, second)
	}
	replayCheck(t, e, cfg)
}
