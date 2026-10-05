package effects

// api:HealDamage — Forge's "all damage already dealt to [the defined object]
// is healed" (Wolverine, Fierce Fighter; Pyramids). Before this the API was
// unregistered, so the coverage gate skipped both carriers. The mutation is a
// marked-damage reduction emitted as a negative events.Damage through the
// host, never a direct write to state.Object.Damage.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// healCarrier is a 1/3 so it can carry marked damage without being lethal.
const healCarrier = "Name:Heal Fixture\nTypes:Creature\nPT:1/3\nOracle:x\n"

// TestHealDamageClearsMarkedDamage is the behavioural leaf: a damaged creature
// named by Defined$ loses ALL its marked damage, and a control is untouched.
// The precondition (both are battlefield permanents WITH damage) is asserted
// so a vacuous setup cannot pass.
func TestHealDamageClearsMarkedDamage(t *testing.T) {
	h, c := fixtureHost(t)
	hurt := battlefield(t, h, healCarrier)
	control := battlefield(t, h, healCarrier)
	if o := h.g.Obj(hurt); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: heal target is not on the battlefield: %+v", o)
	}
	h.g.Obj(hurt).Damage = 2
	h.g.Obj(control).Damage = 1
	// The target arrives through the ordinary Defined$ read.
	c.Remembered = []state.Target{{Obj: hurt}}

	Resolve(h, c, &cards.SA{Kind: "DB", API: "HealDamage",
		Params: map[string]string{"Defined": "Remembered"}})

	if got := h.g.Obj(hurt).Damage; got != 0 {
		t.Fatalf("HealDamage left marked damage: Damage = %d, want 0", got)
	}
	if got := h.g.Obj(control).Damage; got != 1 {
		t.Fatalf("HealDamage healed a control it did not name: Damage = %d, want 1", got)
	}
	// The mutation must be an emitted negative Damage event, not a field
	// write: the log is the replay authority.
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Damage && ev.Obj == hurt && ev.Amount == -2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no negative Damage(-2) event for the healed object; log = %+v", h.log)
	}
}

// TestHealDamageNoDamageIsQuiet pins the no-op arm: healing an undamaged
// object emits nothing (no spurious Damage(0) event), while STILL proving the
// primitive ran by emitting nothing else either — paired with the registration
// assertion below so a dropped Register cannot pass this test.
func TestHealDamageNoDamageIsQuiet(t *testing.T) {
	h, c := fixtureHost(t)
	fresh := battlefield(t, h, healCarrier)
	c.Remembered = []state.Target{{Obj: fresh}}
	before := len(h.log)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "HealDamage",
		Params: map[string]string{"Defined": "Remembered"}})
	if n := len(h.log) - before; n != 0 {
		t.Fatalf("healing an undamaged object emitted %d events: %+v", n, h.log[before:])
	}
}

// TestHealDamagePrimitiveIsRegistered pins the support declaration the skip
// gate reads (so the no-op arm above cannot pass with the API unregistered).
func TestHealDamagePrimitiveIsRegistered(t *testing.T) {
	if !Supported()["api:HealDamage"] {
		t.Fatal(`Supported() is missing "api:HealDamage"`)
	}
}
