package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// sacrificeRecipeCases is one real card per sacrifice recipe shape: a creature
// or permanent sacrificed by Village Rites, an artifact by Deadly Dispute (p0's
// or, for an OppCtrl filter, p1's), the card's own sacrifice ability, a
// Food/Clue/permanent token a maker prelude mints and Deadly Dispute then
// sacrifices, and a self filter on a card with no sacrifice ability, answered
// with Angelic Purge's sacrifice-a-permanent cost. probe is the card the cause
// step names.
var sacrificeRecipeCases = []struct {
	name, key, probe string
	seat             int
}{
	{"Pirate Peddlers", "trigger#0.0", "Village Rites", 0},
	{"Sandbender Scavengers", "trigger#0.0", "Village Rites", 0},
	{"Zhao, Ruthless Admiral", "trigger#0.0", "Village Rites", 0},
	{"Tolls of War", "trigger#0.1", "Village Rites", 0},
	{"Vito, Fanatic of Aclazotz", "trigger#0.0", "Village Rites", 0},
	{"Rakdos, the Muscle", "trigger#0.0", "Village Rites", 0},
	{"Zodiark, Umbral God", "trigger#0.1", "Village Rites", 0},
	{"Crime Novelist", "trigger#0.0", "Deadly Dispute", 0},
	{"Biotech Specialist", "trigger#0.1", "Deadly Dispute", 0},
	{"Lightless Evangel", "trigger#0.0", "Village Rites", 0},
	{"Vengeful Tracker", "trigger#0.0", "Deadly Dispute", 1},
	{"Esoteric Duplicator", "trigger#0.0", "Deadly Dispute", 0},
	{"Carrot Cake", "trigger#0.1", "Carrot Cake", 0},
	{"Heaped Harvest", "trigger#0.1", "Heaped Harvest", 0},
	{"Camellia, the Seedmiser", "trigger#0.0", "Deadly Dispute", 0},
	{"Unlucky Cabbage Merchant", "trigger#0.1", "Deadly Dispute", 0},
	{"Experimental Confectioner", "trigger#0.1", "Deadly Dispute", 0},
	{"Lazav, Wearer of Faces", "trigger#0.1", "Deadly Dispute", 0},
	{"Persuasive Interrogators", "trigger#0.1", "Deadly Dispute", 0},
	{"Curious Cadaver", "trigger#0.0", "Deadly Dispute", 0},
	{"The Sackville-Bagginses", "trigger#0.1", "Deadly Dispute", 0},
	{"Disturbing Mirth", "trigger#0.1", "Angelic Purge", 0},
	{"Ordeal of Nylea", "trigger#0.1", "Angelic Purge", 0},
}

// TestSacrificeTriggerRecipesFire serves one real card per sacrifice recipe
// shape and proves each item (1) is classified into trigger.sacrificed, (2)
// emits the cause step from the side that needs it, (3) plays through gorge,
// and (4) puts THIS card's trigger on the stack, which the same replay with the
// card removed does not.
func TestSacrificeTriggerRecipesFire(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range sacrificeRecipeCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.sacrificed")
			p0 := it.Scenario.Setup["p0"]
			if !inZone(p0.Battlefield, tc.name) && !inZone(p0.Hand, tc.name) && !inZone(p0.Graveyard, tc.name) {
				t.Fatalf("precondition: trigger source %q is in no p0 zone: %+v", tc.name, p0)
			}
			found := false
			for _, st := range it.Scenario.Steps {
				if st.Seat == tc.seat && strings.HasSuffix(st.Card, ":"+tc.probe) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no step by seat %d names probe %s: %+v", tc.seat, tc.probe, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not play through: ok=%v fails=%v", ok, res.Fails)
			}
			if !firedOnStack(t, it.Scenario, tc.name) {
				t.Fatalf("%s's sacrifice trigger never reached the stack", tc.name)
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
