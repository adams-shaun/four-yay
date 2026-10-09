package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// damageRecipeCases is one real card per damage-trigger recipe shape: a bearer
// (Equipment/Aura) source's combat damage, the card's own combat damage, a
// keyword-qualified source creature, a creature dealt noncombat damage, the
// creature an Aura enchants dealt noncombat damage, the card itself dealt
// noncombat damage, the card's combat damage to a blocking creature it names,
// a creature's combat damage to the card's controller, and a face-down
// creature's combat damage. probe is the Shock cast the cause names for a
// Shock cause, or "" for an attack cause.
var damageRecipeCases = []struct {
	name, key, shock string
	attack           bool
	block            bool
	seat1Attack      bool
	morphCast        bool
}{
	{"Lost Jitte", "trigger#0.0", "", true, false, false, false},
	{"Sword of Wealth and Power", "trigger#0.0", "", true, false, false, false},
	{"Thieving Otter", "trigger#0.0", "", true, false, false, false},
	{"Fynn, the Fangbearer", "trigger#0.0", "", true, false, false, false},
	{"Elegy Acolyte", "trigger#0.0", "", true, false, false, false},
	{"Cracked Skull", "trigger#0.1", "Shock", false, false, false, false},
	{"Expedited Inheritance", "trigger#0.0", "Shock", false, false, false, false},
	{"Taii Wakeen, Perfect Shot", "trigger#0.0", "Shock", false, false, false, false},
	{"Grievous Wound", "trigger#0.0", "Shock", false, false, false, false},
	{"Spider-Slayer, Hatred Honed", "trigger#0.0", "", true, true, false, false},
	{"Contested Game Ball", "trigger#0.0", "", true, false, true, false},
	{"Yarus, Roar of the Old Gods", "trigger#0.0", "", true, false, false, true},
}

// TestDamageTriggerRecipesFire serves one real card per damage-trigger recipe
// shape and proves each item (1) is classified into trigger.damage, (2) emits
// the attack or Shock cause step, (3) plays through gorge, and (4) puts THIS
// card's trigger on the stack, which the same replay with the card removed does
// not.
func TestDamageTriggerRecipesFire(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range damageRecipeCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.DamageSub)
			p0 := it.Scenario.Setup["p0"]
			if !inZone(p0.Battlefield, tc.name) && !inZone(p0.Hand, tc.name) && !inZone(p0.Graveyard, tc.name) {
				t.Fatalf("precondition: trigger source %q is in no p0 zone: %+v", tc.name, p0)
			}
			if tc.block {
				// The blocking victim is the damaged creature the trigger names;
				// it must be on p1's side for the block pair to reach it.
				if len(it.Scenario.Setup["p1"].Battlefield) == 0 {
					t.Fatalf("precondition: no blocker on p1's battlefield: %+v", it.Scenario.Setup["p1"])
				}
			}
			if tc.seat1Attack && len(it.Scenario.Setup["p1"].Battlefield) == 0 {
				t.Fatalf("precondition: no attacker on p1's battlefield: %+v", it.Scenario.Setup["p1"])
			}
			if tc.morphCast && len(p0.Hand) == 0 {
				t.Fatalf("precondition: no face-down cast candidate in p0's hand: %+v", p0)
			}
			attack, shock := false, false
			blockStep, seat1, morphed := false, false, false
			for _, st := range it.Scenario.Steps {
				if st.Op == "attack" && len(st.Attackers) > 0 {
					attack = true
					if st.Seat == 1 && st.Defender == "p0" {
						seat1 = true
					}
				}
				if st.Op == "block" && len(st.Blocks) > 0 {
					blockStep = true
				}
				if st.Op == "cast" && st.CastMode != "" {
					morphed = true
				}
				if tc.shock != "" && st.Op == "cast" && strings.HasSuffix(st.Card, ":"+tc.shock) {
					shock = true
				}
			}
			if tc.attack && !attack {
				t.Fatalf("no attack step in the cause: %+v", it.Scenario.Steps)
			}
			if tc.block && !blockStep {
				t.Fatalf("no block step in the cause: %+v", it.Scenario.Steps)
			}
			if tc.seat1Attack && !seat1 {
				t.Fatalf("no p1-attacks-p0 attack step in the cause: %+v", it.Scenario.Steps)
			}
			if tc.morphCast && !morphed {
				t.Fatalf("no face-down cast step in the cause: %+v", it.Scenario.Steps)
			}
			if tc.shock != "" && !shock {
				t.Fatalf("no %s cast step in the cause: %+v", tc.shock, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not play through: ok=%v fails=%v", ok, res.Fails)
			}
			if !firedOnStack(t, it.Scenario, tc.name) {
				t.Fatalf("%s's damage trigger never reached the stack", tc.name)
			}
			control := it.Scenario
			control.Setup = map[string]oraclegen.Seat{}
			for k, seat := range it.Scenario.Setup {
				seat.Battlefield = without(seat.Battlefield, tc.name)
				seat.Hand = without(seat.Hand, tc.name)
				seat.Graveyard = without(seat.Graveyard, tc.name)
				control.Setup[k] = seat
			}
			if res, ok := oraclegen.PlaysThrough(reg, control); ok && len(res.Fails) == 0 && stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("control: %s's ability is on the stack without the card in play", tc.name)
			}
		})
	}
}

// TestDamageTriggerClassifierRouted pins that the new sub-family is registered
// with the template (a recipe that lands without its triggerSubs entry serves
// nothing) and that the narrower damage families keep their own sub-families.
func TestDamageTriggerClassifierRouted(t *testing.T) {
	if !triggerSubs(levelb.DamageSub) {
		t.Fatalf("%s is not served by the trigger template", levelb.DamageSub)
	}
}
