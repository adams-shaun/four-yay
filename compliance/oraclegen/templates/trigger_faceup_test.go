package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
)

// turnFaceUpCases is one real card per turned-face-up recipe shape. Each row
// names the morph family it drives (the cast_mode the cast step must ask for
// and the XMage activate selector the driver must emit): Dog Walker and Branch
// of Vitu-Ghazi are Disguise Card.Self carriers, Infernal Caretaker is a Morph
// Card.Self carrier and Ainok Survivalist a Megamorph Card.Self carrier (its
// turn-up ability rule text carries the +1/+1 counter rider). Sumala Sentry
// has a Permanent.YouCtrl trigger (the card sits on the battlefield and a
// separate Disguise probe is turned up). probe is the card the cast step
// names.
var turnFaceUpCases = []struct {
	name, key, sub, probe string
	selfInHand            bool
	castMode              string
	xrule                 string
}{
	{"Dog Walker", "trigger#0.0", levelb.TurnedFaceUpSub, "Dog Walker", true, "disguised", "{R/W}{R/W}: Turn this face-down permanent face up."},
	{"Branch of Vitu-Ghazi", "trigger#0.0", levelb.TurnedFaceUpSub, "Branch of Vitu-Ghazi", true, "disguised", "{3}: Turn this face-down permanent face up."},
	{"Infernal Caretaker", "trigger#0.0", levelb.TurnedFaceUpSub, "Infernal Caretaker", true, "morphed", "{3}{B}: Turn this face-down permanent face up."},
	{"Ainok Survivalist", "trigger#0.0", levelb.TurnedFaceUpSub, "Ainok Survivalist", true, "megamorphed", "{1}{G}: Turn this face-down permanent face up and put a +1/+1 counter on it."},
	{"Sumala Sentry", "trigger#0.0", levelb.TurnedFaceUpOtherSub, "Bolrac-Clan Basher", false, "disguised", "{3}{R}{R}: Turn this face-down permanent face up."},
	// Cryptid Inspector's filter also names another permanent but the card
	// itself is no morph family (it is a DSK payoff), so the recipe falls
	// back to turning up a probe. Its TurnFaceUp trigger is the card's second.
	{"Cryptid Inspector", "trigger#0.1", levelb.TurnedFaceUpSub, "Bolrac-Clan Basher", false, "disguised", "{3}{R}{R}: Turn this face-down permanent face up."},
}

// TestTurnFaceUpTriggerRecipeFires: each turned-face-up item generates, casts
// its probe with its morph family's face-down cast mode, turns it up with an
// activate step whose XMage selector XMage renders ("<cost>: Turn this
// face-down permanent face up[ and put a +1/+1 counter on it]."), and leaves
// the card's trigger on the stack after that activate step.
func TestTurnFaceUpTriggerRecipeFires(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range turnFaceUpCases {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)

			// Precondition: the card is where the shape puts it. A vacuous
			// setup must fail here, not pass silently.
			p0 := it.Scenario.Setup["p0"]
			onBf, inHand := inZone(p0.Battlefield, tc.name), inZone(p0.Hand, tc.name)
			if tc.selfInHand && (onBf || !inHand) {
				t.Fatalf("precondition: %s battlefield=%v hand=%v, want in hand only", tc.name, p0.Battlefield, p0.Hand)
			}
			if !tc.selfInHand && (!onBf || inHand) {
				t.Fatalf("precondition: %s battlefield=%v hand=%v, want on battlefield only", tc.name, p0.Battlefield, p0.Hand)
			}

			// The cast step asks for this family's face-down offer, and the
			// activate step names the turn-up by its label with a family-correct
			// XMage selector beside it.
			castAt, activateAt := -1, -1
			for i, st := range it.Scenario.Steps {
				if st.Op == "cast" && strings.HasSuffix(st.Card, ":"+tc.probe) {
					castAt = i
					if st.CastMode != tc.castMode {
						t.Fatalf("cast step %d cast_mode=%q, want %q", i, st.CastMode, tc.castMode)
					}
				}
				if st.Op == "activate" && strings.HasSuffix(st.Card, ":"+tc.probe) {
					activateAt = i
					if !strings.Contains(strings.ToLower(st.Ability), "turn face up") {
						t.Fatalf("activate step %d ability=%q, want a turn-face-up label", i, st.Ability)
					}
				}
			}
			if castAt < 0 || activateAt < 0 || castAt >= activateAt {
				t.Fatalf("precondition: cast@%d activate@%d for probe %s: %+v", castAt, activateAt, tc.probe, it.Scenario.Steps)
			}
			if len(it.XAbility) != len(it.Scenario.Steps) {
				t.Fatalf("xmage_ability length %d != steps %d", len(it.XAbility), len(it.Scenario.Steps))
			}
			// Precondition: the XMage selector is the family's own, so the
			// assertion below distinguishes Morph/Disguise from Megamorph.
			if x := it.XAbility[activateAt]; x != tc.xrule {
				t.Fatalf("activate step %d xmage_ability = %q, want %q", activateAt, x, tc.xrule)
			}

			// Precondition: the trigger never fired before the turn-up, so the
			// assertion below is about the activate step and not an earlier
			// event.
			res := runSteps(t, reg, it.Scenario, it.Scenario.Steps[:activateAt])
			if stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("precondition: %s's trigger is already on the stack before the turn-up", tc.name)
			}
			// The turn-up special action fires the trigger: a snapshot taken
			// through the activate step shows the card's ability on the stack.
			res = runSteps(t, reg, it.Scenario, it.Scenario.Steps[:activateAt+1])
			if len(res.Fails) != 0 {
				t.Fatalf("activate step failed: %v", res.Fails)
			}
			if !stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("after the turn-up, %s's ability is not on the stack: %+v", tc.name, res.Snapshots)
			}
		})
	}
}

// TestTurnFaceUpFamilyGate reports the morph-family skip for a card that
// carries none of the three heads, so a pass on the recipe rows above cannot
// be a whole-feature no-op (a revert of the gate's family selection).
func TestTurnFaceUpFamilyGate(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Grizzly Bears")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("Grizzly Bears not in the corpus")
	}
	if _, _, _, _, why := morphFamilyCost(c.Faces[0]); why != "turned-face-up: not a morph-family card" {
		t.Fatalf("non-morph-family skip = %q, want the named morph-family skip", why)
	}
	for _, fam := range []struct{ head, card, mode string }{
		{"Morph", "Infernal Caretaker", "morphed"},
		{"Megamorph", "Ainok Survivalist", "megamorphed"},
		{"Disguise", "Dog Walker", "disguised"},
	} {
		card, ok := reg.Lookup(fam.card)
		if !ok || len(card.Faces) == 0 {
			t.Fatalf("%s carrier not in the corpus", fam.head)
		}
		_, _, mode, _, why := morphFamilyCost(card.Faces[0])
		if why != "" || mode != fam.mode {
			t.Fatalf("%s carrier: mode=%q why=%q, want %q", fam.head, mode, why, fam.mode)
		}
	}
}
