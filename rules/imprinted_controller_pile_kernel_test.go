package rules

// Restores the sacrifice leg of effects/imprinted_controller_pile_test.go's
// TestEnchantersBaneImprintedControllerDamage on the kernel (its decline leg
// is TestEnchantersBaneDamageLegReachesImprintController): when the
// imprinted enchantment's controller SACRIFICES it, the damage leg's
// ConditionCheckSVar gate (Remembered$Amount EQ0) suppresses the damage.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestEnchantersBaneSacrificedTargetDealsNoDamage(t *testing.T) {
	t.Parallel()
	e, cfg, baneID, auraID := enchantersBaneFixture(t)
	e.setStep(state.StepEnd)
	e.priorityRound()
	from := len(e.L.Events)
	sacAsked := false
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil || (d.Kind == decision.KPriority && len(e.G.Stack) == 0) {
			break
		}
		switch {
		case d.Kind == decision.KPriority:
			passFirst(t, e)
		case d.Kind == decision.KTarget:
			targetObject(t, e, auraID)
		case d.Kind == decision.KChoose && d.ResumeKind == "sacrifice":
			if d.Player != 1 {
				t.Fatalf("sacrifice ask posed to seat %d, want the aura's controller (seat 1)", d.Player)
			}
			sacAsked = true
			submitChoices(t, e, kr2ObjIdx(t, d, auraID))
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
	}
	if !sacAsked {
		t.Fatal("the optional sacrifice was never posed")
	}
	if z := e.G.Obj(auraID).Zone; z != state.ZGraveyard {
		t.Fatalf("sacrificed aura is on %s, want graveyard", z)
	}
	if dmg := kr2Events(e, from, events.Damage); len(dmg) != 0 {
		t.Fatalf("damage dealt despite the sacrifice: %+v", dmg)
	}
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("life = %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if o := e.G.Obj(baneID); len(o.Imprinted) != 0 {
		t.Fatalf("Bane's imprint = %v, want cleared", o.Imprinted)
	}
	replayCheck(t, e, cfg)
}
