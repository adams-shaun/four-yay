package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func targetEffectAmount(t *testing.T, d *decision.Decision) int {
	t.Helper()
	if d == nil || d.TargetEffect == nil || d.TargetEffect.Damage == nil || d.TargetEffect.Damage.Amount == nil {
		t.Fatalf("target decision has no known damage amount: %+v", d)
	}
	return *d.TargetEffect.Damage.Amount
}

func stackTargetEffect(t *testing.T, src string, x int32) (*Engine, *cards.SA) {
	t.Helper()
	e := newSeats(t, 2)
	c := card(t, src)
	o := e.G.AddObject(c, 0)
	if o.Zone != state.ZLibrary {
		t.Fatalf("fixture precondition: new card zone = %s, want library", o.Zone)
	}
	e.emit(events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 0,
		From: state.ZLibrary, To: state.ZStack, Text: "target fidelity"})
	e.emit(events.Event{Kind: events.CastInfo, Obj: o.ID, Amount: x})
	o = e.G.Obj(o.ID)
	if o == nil || o.Zone != state.ZStack {
		t.Fatalf("fixture precondition: spell is not on stack: %+v", o)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.API != "DealDamage" {
		t.Fatalf("fixture precondition: spell ability = %+v", sa)
	}
	e.askTarget(0, o.ID, sa)
	if e.Pending() == nil || e.Pending().Kind != decision.KTarget {
		t.Fatalf("fixture precondition: target decision = %+v", e.Pending())
	}
	return e, sa
}

func TestTargetEffectReportsAnnouncedXAndSVarAmounts(t *testing.T) {
	t.Parallel()
	e, _ := stackTargetEffect(t,
		"Name:X Bolt\nManaCost:X R\nTypes:Instant\n"+
			"A:SP$ DealDamage | ValidTgts$ Player | NumDmg$ X\n"+
			"SVar:X:Count$xPaid\nOracle:x\n", 4)
	if got := targetEffectAmount(t, e.Pending()); got != 4 {
		t.Fatalf("announced X damage = %d, want 4", got)
	}

	e2, _ := stackTargetEffect(t,
		"Name:SVar Bolt\nManaCost:R\nTypes:Instant\n"+
			"A:SP$ DealDamage | ValidTgts$ Player | NumDmg$ X\n"+
			"SVar:X:Count$YourLifeTotal\nOracle:x\n", 0)
	if got := targetEffectAmount(t, e2.Pending()); got != 20 {
		t.Fatalf("SVar damage = %d, want the controller's 20 life, not the zero fallback", got)
	}
	if targetEffectAmount(t, e.Pending()) == targetEffectAmount(t, e2.Pending()) {
		t.Fatal("precondition: announced-X and SVar amounts must differ")
	}
}

func TestTargetEffectClassifiesKnownRemovalShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, api, destination, kind string
	}{
		{name: "destroy", api: "Destroy", kind: "destroy"},
		{name: "sacrifice", api: "Sacrifice", kind: "sacrifice"},
		{name: "exile", api: "ChangeZone", destination: "Exile", kind: "exile"},
		{name: "bounce", api: "ChangeZone", destination: "Hand", kind: "bounce"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSeats(t, 2)
			sa := &cards.SA{API: tc.api, Params: map[string]string{
				"ValidTgts": "Player", "Destination": tc.destination,
			}}
			e.askTarget(0, 0, sa)
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("fixture precondition: target decision = %+v", d)
			}
			if d.TargetEffect == nil || d.TargetEffect.Removal == nil {
				t.Fatalf("missing removal classification: %+v", d.TargetEffect)
			}
			if got := d.TargetEffect.Removal.Kind; got != tc.kind {
				t.Fatalf("removal kind = %q, want %q", got, tc.kind)
			}
			if tc.destination != "" && d.TargetEffect.Removal.Destination != strings.ToLower(tc.destination) {
				t.Fatalf("destination = %q, want %q", d.TargetEffect.Removal.Destination, strings.ToLower(tc.destination))
			}
		})
	}
}

func TestTargetEffectLeavesUnknownAPIsUninterpreted(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.askTarget(0, 0, &cards.SA{API: "FutureRemoval", Params: map[string]string{"ValidTgts": "Player"}})
	d := e.Pending()
	if d == nil || d.TargetEffect == nil {
		t.Fatalf("fixture precondition: target decision = %+v", d)
	}
	if d.TargetEffect.Removal != nil || d.TargetEffect.Damage != nil {
		t.Fatalf("unknown API was classified: %+v", d.TargetEffect)
	}
}
