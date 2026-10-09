package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// selfCauseTriggerCases is one real card per newly served self-cause recipe
// (ticket agent-20261009T160853Z-c3e35453): a self-transform trigger (the
// other face's Phase transform enabler pays and flips the card into the
// requirement's face), a self cycling trigger (the card's own hand cycling
// activation discards it as its cost) and an origin-free land-play trigger
// (p0's own land drop). wantZone is where the trigger's source sits at setup;
// stepOp is the cause op the item must carry.
var selfCauseTriggerCases = []struct {
	name, key, sub, wantZone, stepOp string
}{
	{"Ashling, Rekindled", "trigger#0.1", "trigger.transformed", "battlefield", "pass"},
	{"Ashling, Rekindled", "trigger#1.0", "trigger.transformed", "battlefield", "pass"},
	{"Brigid, Clachan's Heart", "trigger#0.1", "trigger.transformed", "battlefield", "pass"},
	{"Sygg, Wanderwine Wisdom", "trigger#1.0", "trigger.transformed", "battlefield", "pass"},
	{"Trystan, Callous Cultivator", "trigger#1.0", "trigger.transformed", "battlefield", "pass"},
	{"Sidequest: Raise a Chocobo", "trigger#1.0", "trigger.transformed", "battlefield", "pass"},
	{"Ultimecia, Time Sorceress", "trigger#1.0", "trigger.transformed", "battlefield", "pass"},
	{"Agonasaur Rex", "trigger#0.0", "trigger.cycled", "hand", "activate"},
	{"Webstrike Elite", "trigger#0.0", "trigger.cycled", "hand", "activate"},
	{"Basri, Tomorrow's Champion", "trigger#0.0", "trigger.cycled", "hand", "activate"},
	{"The Endstone", "trigger#0.0", "trigger.land-played", "battlefield", "play"},
	{"Recycle", "trigger#0.1", "trigger.land-played", "battlefield", "play"},
	{"Infernal Sovereign", "trigger#0.1", "trigger.land-played", "battlefield", "play"},
}

// TestSelfCauseTriggerRecipes serves one card per new recipe and proves each
// item (1) carries the trigger's source in the zone the rule reads, (2) names
// the cause op in its steps, (3) plays through gorge, (4) fires the card's
// trigger -- which the same replay with the card removed does not.
func TestSelfCauseTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range selfCauseTriggerCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			var it oraclegen.Item
			var slot string
			var face *cards.Face
			found := false
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				found = true
				if r.Sub != tc.sub {
					t.Fatalf("precondition: %s %s classified %s, want %s", tc.name, tc.key, r.Sub, tc.sub)
				}
				var skip *oraclegen.Skip
				it, skip = GenerateB(reg, tc.name, r)
				if skip != nil {
					t.Fatalf("%s %s: %s", tc.name, tc.key, skip.Reason)
				}
				slot, face = r.Slot, c.Faces[r.Face]
			}
			if !found {
				t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
			}
			p0 := it.Scenario.Setup["p0"]
			// The trigger's source is where its rule reads it: a transform or
			// land-play source on the battlefield, a cycling card in the hand.
			switch tc.wantZone {
			case "battlefield":
				if !inZone(p0.Battlefield, tc.name) {
					t.Fatalf("precondition: %s not on p0's battlefield: %v", tc.name, p0.Battlefield)
				}
			case "hand":
				if !inZone(p0.Hand, tc.name) {
					t.Fatalf("precondition: %s not in p0's hand: %v", tc.name, p0.Hand)
				}
			}
			foundOp := false
			for _, st := range it.Scenario.Steps {
				if tc.stepOp == "activate" && st.Op == "activate" && strings.HasSuffix(st.Card, ":"+tc.name) {
					foundOp = true
				}
				if tc.stepOp == "play" && st.Op == "play" {
					foundOp = true
				}
				if tc.stepOp == "pass" && st.Op == "pass" {
					foundOp = true
				}
			}
			if !foundOp {
				t.Fatalf("no %s cause step: %+v", tc.stepOp, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			// The trigger reaches the stack: the generation's own slot check
			// over the cause replay, stopped at the next priority decision --
			// for every cause here the first one after the trigger was queued.
			cause := it.Scenario.Steps
			for len(cause) > 0 && cause[len(cause)-1].Op == "resolve" {
				cause = cause[:len(cause)-1]
			}
			fire := runSteps(t, reg, it.Scenario, append(append([]oraclegen.Step(nil), cause...),
				oraclegen.Step{Op: "pass_to", Decision: "priority"}))
			if len(fire.Fails) != 0 || !abilityOnStack(fire.Snapshots, stackSourceWants(reg, tc.name, face), slot) {
				t.Fatalf("%s's trigger %s never reached the stack: fails=%v", tc.name, slot, fire.Fails)
			}
			control := it.Scenario
			control.Setup = map[string]oraclegen.Seat{}
			for k, seat := range it.Scenario.Setup {
				seat.Battlefield = without(seat.Battlefield, tc.name)
				seat.Hand = without(seat.Hand, tc.name)
				seat.BackFace = without(seat.BackFace, tc.name)
				control.Setup[k] = seat
			}
			if res, ok := oraclegen.PlaysThrough(reg, control); ok && len(res.Fails) == 0 && stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("control: %s's ability is on the stack without the card in play", tc.name)
			}
		})
	}
}
