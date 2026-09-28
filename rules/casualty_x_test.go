package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCasualtyXObNixilisCopyRiders: Ob Nixilis, the Adversary is the
// corpus's one variable-Casualty carrier — its printed line is
// `K:Casualty:X:NonLegendary$ True | SetLoyalty$ Casualty:The copy isn't
// legendary and has starting loyalty X.` The test casts it through the real
// election/payment/resolution path with a power-1 sacrifice, so the variable
// amount (the sacrificed creature's power, 1) differs from the printed
// starting loyalty (3) in both directions, and asserts the casualty copy
// resolves nonlegendary with loyalty 1 while the original resolves legendary
// with its printed 3 (CR 702.249a + the copy riders).
func TestCasualtyXObNixilisCopyRiders(t *testing.T) {
	t.Parallel()
	e, cfg, reg := casualtyEngine(t, "Ob Nixilis, the Adversary")
	sac := seedBattlefield(t, e, reg, "Llanowar Elves")  // power 1
	other := seedBattlefield(t, e, reg, "Grizzly Bears") // power 2, stays
	if e.G.Obj(sac).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield ||
		e.Power(sac) != 1 || e.Power(other) != 2 {
		t.Fatalf("invalid casualty setup: zones/power sac=%+v other=%+v", e.G.Obj(sac), e.G.Obj(other))
	}
	spell := searchMoveByName(t, e, "Ob Nixilis, the Adversary", state.ZHand)
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatal("Ob Nixilis must be announced from hand")
	}
	// Rider precondition: the variable amount the sacrifice names (1) is
	// actually different from the printed starting loyalty (3), so the
	// copy's loyalty and legendary status below distinguish a real
	// SetLoyalty$/NonLegendary$ read from an exact copy.
	if loy := e.G.Obj(spell).Face().Loyalty; loy != "3" || e.Power(sac) == 3 {
		t.Fatalf("rider precondition failed: printed loyalty %q, sacrifice power %d", loy, e.Power(sac))
	}
	addMana(t, e, 0, "BBR") // B pays the pip, the rest covers {1} and R
	opt := castOptMode(t, e.Pending().Options, spell, "casualty")
	submitChoices(t, e, opt.Index)
	// The variable form gates on no threshold: every creature you control
	// qualifies, because the sacrificed creature's own power names the
	// amount (CR 702.249a). The ask must offer both creatures.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "casualty" {
		t.Fatalf("expected casualty creature election, got %+v", d)
	}
	offered := make([]state.ObjID, 0, len(d.Options))
	pick := -1
	for _, o := range d.Options {
		offered = append(offered, o.Obj)
		if o.Obj == sac {
			pick = o.Index
		}
	}
	if !slices.Contains(offered, other) {
		t.Fatalf("variable casualty must offer every creature you control: %+v", d.Options)
	}
	if pick == -1 {
		t.Fatalf("power-1 sacrifice not offered: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(sac).Zone != state.ZGraveyard {
		t.Fatalf("casualty cost was not sacrificed: zone %+v", e.G.Obj(sac))
	}
	if e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("unselected creature was sacrificed")
	}
	// Exactly one copy, carrying the riders in its event payload.
	copies := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy && ev.Obj == spell {
			copies++
			if ev.Counter != "nonlegendary,loyalty=1" {
				t.Fatalf("copy payload Counter = %q, want nonlegendary,loyalty=1", ev.Counter)
			}
		}
	}
	if copies != 1 {
		t.Fatalf("copies = %d, want 1", copies)
	}
	// Original and copy on the battlefield: the original legendary with its
	// printed 3 loyalty, the copy nonlegendary with loyalty 1.
	original, copy := state.ObjID(0), state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o.Face() == nil || o.Face().Name != "Ob Nixilis, the Adversary" {
			continue
		}
		if slices.Contains(e.Derived(id).Types, "Legendary") {
			if original != 0 {
				t.Fatal("two legendary Ob Nixilis permanents: the legend rule binned one")
			}
			original = id
		} else if copy != 0 {
			t.Fatal("two nonlegendary Ob Nixilis permanents")
		} else {
			copy = id
		}
	}
	if original == 0 || copy == 0 {
		t.Fatalf("original and copy not both on the battlefield: original=%d copy=%d", original, copy)
	}
	if got := e.G.Obj(original).Counter("LOYALTY"); got != 3 {
		t.Fatalf("original loyalty = %d, want the printed 3", got)
	}
	if got := e.G.Obj(copy).Counter("LOYALTY"); got != 1 {
		t.Fatalf("copy loyalty = %d, want the sacrificed creature's power 1", got)
	}
	replayCheck(t, e, cfg)
}
