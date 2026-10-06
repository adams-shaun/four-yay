package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestRoomUnlockTriggerRecipesFire pins the Room trigger recipes: a door's own
// "when you unlock this door" and a second-door trigger are caused by CASTING
// the door (never by placing the Room, which leaves no door unlocked), and
// Eerie's "whenever you fully unlock a Room" by casting a probe Room and
// paying to unlock its other door. Each scenario must replay in gorge with the
// requirement's trigger on the stack at some point.
func TestRoomUnlockTriggerRecipesFire(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	cases := []struct {
		card, key, sub string
		castsDoor      string // the cast step's card ref
		unlocks        bool   // a paid unlock follows the probe's cast
	}{
		{"Bottomless Pool // Locker Room", "trigger#0.0", levelb.UnlockDoorSub, "p0:Bottomless Pool // Locker Room", false},
		{"Funeral Room // Awakening Hall", "trigger#1.0", levelb.UnlockDoorSub, "p0:Awakening Hall", false},
		{"Entity Tracker", "trigger#0.1", levelb.FullyUnlockSub, "p0:Dazzling Theater // Prop Room", true},
		{"Glassworks // Shattered Yard", "trigger#1.0", "trigger.phase", "p0:Shattered Yard", false},
	}
	for _, tc := range cases {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			req := requirement(t, tc.card, tc.key)
			if req.Sub != tc.sub || req.Gap != "" {
				t.Fatalf("requirement = sub %q gap %q, want sub %q and no gap", req.Sub, req.Gap, tc.sub)
			}
			item, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("skipped: %s", skip.Reason)
			}
			p0 := item.Scenario.Setup["p0"]
			if tc.card != "Entity Tracker" {
				if containsString(p0.Battlefield, tc.card) || !containsString(p0.Hand, tc.card) {
					t.Fatalf("the Room must start in hand, not on the battlefield: hand %v battlefield %v", p0.Hand, p0.Battlefield)
				}
			}
			cast, unlock := false, false
			for _, st := range item.Steps {
				cast = cast || (st.Op == "cast" && st.Card == tc.castsDoor)
				unlock = unlock || (st.Op == "activate" && st.Ability != "")
			}
			if !cast {
				t.Fatalf("no cast step for %s in %+v", tc.castsDoor, item.Steps)
			}
			if unlock != tc.unlocks {
				t.Fatalf("paid unlock step = %t, want %t: %+v", unlock, tc.unlocks, item.Steps)
			}
			if tc.unlocks {
				at := -1
				for i, st := range item.Steps {
					if st.Op == "activate" {
						at = i
					}
				}
				if len(item.XAbility) != len(item.Steps) || at < 0 || item.XAbility[at] == "" {
					t.Fatalf("the unlock step carries no XMage rule text: %q", item.XAbility)
				}
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(res.Fails) != 0 {
				t.Fatalf("replay err=%v fails=%v", err, res.Fails)
			}
			// The item's trailing resolves empty the stack, so show the trigger
			// the way the generator's own probe does: drop them and pass once.
			probe := item.Scenario
			probe.Steps = append([]oraclegen.Step(nil), item.Steps...)
			for n := len(probe.Steps); n > 0 && probe.Steps[n-1].Op == "resolve"; n-- {
				probe.Steps = probe.Steps[:n-1]
			}
			probe.Steps = append(probe.Steps, oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1})
			_, probed, ok := oraclegen.Settle(reg, probe)
			if !ok {
				t.Fatalf("the probe scenario does not play through")
			}
			card, _ := reg.Lookup(tc.card)
			if !abilityOnStack(probed.Snapshots, tc.card, card.Faces[req.Face].Name, stackSlot(req)) {
				t.Fatalf("trigger %s never reached the stack", req.Key)
			}
		})
	}
}
