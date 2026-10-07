package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
)

// turnFaceUpCases is one real card per turned-face-up recipe shape: Dog Walker
// and Branch of Vitu-Ghazi have a Card.Self trigger (the card is cast face
// down itself), Sumala Sentry has a Permanent.YouCtrl trigger (the card sits
// on the battlefield and a separate Disguise probe is turned up). probe is the
// card the cast step names.
var turnFaceUpCases = []struct {
	name, key, sub, probe string
	selfInHand            bool
}{
	{"Dog Walker", "trigger#0.0", levelb.TurnedFaceUpSub, "Dog Walker", true},
	{"Branch of Vitu-Ghazi", "trigger#0.0", levelb.TurnedFaceUpSub, "Branch of Vitu-Ghazi", true},
	{"Sumala Sentry", "trigger#0.0", levelb.TurnedFaceUpOtherSub, "Bolrac-Clan Basher", false},
}

// TestTurnFaceUpTriggerRecipeFires: each turned-face-up item generates, casts
// its probe with the engine's "disguised" mode, turns it up with an activate
// step whose XMage selector XMage renders ("<cost>: Turn this face-down
// permanent face up."), and leaves the card's trigger on the stack after that
// activate step.
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

			// The cast step asks for the disguised offer, and the activate step
			// names the turn-up by its label with an XMage selector beside it.
			castAt, activateAt := -1, -1
			for i, st := range it.Scenario.Steps {
				if st.Op == "cast" && strings.HasSuffix(st.Card, ":"+tc.probe) {
					castAt = i
					if st.CastMode != "disguised" {
						t.Fatalf("cast step %d cast_mode=%q, want disguised", i, st.CastMode)
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
			if x := it.XAbility[activateAt]; !strings.Contains(x, ": Turn this face-down permanent face up.") {
				t.Fatalf("activate step %d xmage_ability = %q, want the TurnFaceUp rule text", activateAt, x)
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
