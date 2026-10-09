package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// otherModeTriggerCases is one real card per newly served "other mode" recipe
// (ticket cli-20261009T031408Z-a01003ef): a life-loss trigger (Shock at the
// losing player), a discard trigger (Mind Rot making the controller discard)
// and an opponent-draw trigger (Sign in Blood). probe is the card the cause
// casts and must appear in the item's steps.
var otherModeTriggerCases = []struct {
	name, key, sub, probe string
}{
	{"Moonstone Harbinger", "trigger#0.1", "trigger.life-lost", "Shock"},
	{"Marina Vendrell's Grimoire", "trigger#0.2", "trigger.life-lost", "Shock"},
	{"The Master of Lake-town", "trigger#0.0", "trigger.life-lost", "Shock"},
	{"Captain Howler, Sea Scourge", "trigger#0.0", "trigger.discarded", "Mind Rot"},
	{"Inti, Seneschal of the Sun", "trigger#0.1", "trigger.discarded", "Mind Rot"},
	{"Moonstone, Harsh Mistress", "trigger#0.0", "trigger.discarded", "Mind Rot"},
	{"King T'Challa", "trigger#0.0", "trigger.drawn-other", "Sign in Blood"},
	{"Gleaming Splendor", "trigger#0.0", "trigger.drawn-other", "Sign in Blood"},
}

// TestOtherModeTriggerRecipes serves one card per new recipe and proves each
// item (1) is classified into the sub-family, (2) names its probe in the cause
// steps, (3) plays through gorge, (4) fires the card's trigger -- which the
// same replay with the card removed does not.
func TestOtherModeTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range otherModeTriggerCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			if !inZone(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: %s not on p0's battlefield: %v", tc.name, p0.Battlefield)
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
