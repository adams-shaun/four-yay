package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// crewedTriggerCases is one row per newly served crew/saddle-perspective
// recipe (ticket agent-20261009T174759Z-80a5bc9a): Canyon Vaulter and
// Reckless Velocitaur's Saddled/Crewed pairs, and Balthier and Fran's
// crew-by-source attack trigger. probe is the permanent the cause activates
// (and, for the attack sub, attacks with) and must appear in the item's
// steps.
var crewedTriggerCases = []struct {
	name, key, sub, probe string
}{
	{"Canyon Vaulter", "trigger#0.0", levelb.SaddledSub, "Bulwark Ox"},
	{"Canyon Vaulter", "trigger#0.1", levelb.CrewedSub, "Smuggler's Copter"},
	{"Reckless Velocitaur", "trigger#0.0", levelb.SaddledSub, "Bulwark Ox"},
	{"Reckless Velocitaur", "trigger#0.1", levelb.CrewedSub, "Smuggler's Copter"},
	{"Balthier and Fran", "trigger#0.0", levelb.AttacksCrewedVehicleSub, "Smuggler's Copter"},
}

// TestCrewedTriggerRecipes serves one row per new cause and proves each item
// (1) is classified into the sub-family, (2) names its probe in the cause
// steps, (3) plays through gorge, (4) fires the card's trigger -- which the
// same replay with the card removed does not.
func TestCrewedTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range crewedTriggerCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			if !inZone(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: %s not on p0's battlefield: %v", tc.name, p0.Battlefield)
			}
			if !inZone(p0.Battlefield, tc.probe) {
				t.Fatalf("precondition: probe %s not on p0's battlefield: %v", tc.probe, p0.Battlefield)
			}
			found := false
			for _, st := range it.Scenario.Steps {
				found = found || strings.HasSuffix(st.Card, ":"+tc.probe)
			}
			if !found {
				t.Fatalf("no step names probe %s: %+v", tc.probe, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !firedOnStack(t, it.Scenario, tc.name) {
				t.Fatalf("%s's ability never reached the stack", tc.name)
			}
			control := it.Scenario
			control.Setup = map[string]oraclegen.Seat{}
			for k, seat := range it.Scenario.Setup {
				seat.Battlefield = without(seat.Battlefield, tc.name)
				seat.Hand = without(seat.Hand, tc.name)
				control.Setup[k] = seat
			}
			if res, ok := oraclegen.PlaysThrough(reg, control); ok && len(res.Fails) == 0 && stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("control: %s's ability is on the stack without the card in play", tc.name)
			}
		})
	}
}
