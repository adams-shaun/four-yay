// Ticket agent-20261009T160937Z-c135e2b0 (level-B keyword-action trigger
// recipes): one real card per sub-family the new recipes serve. Each row's
// requirement must generate, play through gorge, and show the row's own
// trigger (by slot) on the stack; a control replay with the card removed must
// not.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// keywordActionTriggerCases is one real card per newly served keyword-action
// recipe. probe is the card the cause casts or activates and must appear in
// the item's steps; it is empty when the cause acts on the source itself (a
// plot, a saddle, an attack). inHand is true for a trigger that starts in
// p0's hand (the plotted cards).
var keywordActionTriggerCases = []struct {
	name, key, sub, probe string
	inHand                bool
}{
	{"Corpseberry Cultivator", "trigger#0.1", "trigger.forage", "Feed the Cycle", false},
	{"Jolly Gerbils", "trigger#0.0", "trigger.give-gift", "Blooming Blast", false},
	{"Merfolk Cave-Diver", "trigger#0.0", "trigger.explores", "Merfolk Branchwalker", false},
	{"Nicanzil, Current Conductor", "trigger#0.0", "trigger.explores", "Merfolk Branchwalker", false},
	{"Nicanzil, Current Conductor", "trigger#0.1", "trigger.explores", "Merfolk Branchwalker", false},
	{"Evidence Examiner", "trigger#0.1", "trigger.collect-evidence", "Kylox's Voltstrider", false},
	{"Surveillance Monitor", "trigger#0.1", "trigger.collect-evidence", "Kylox's Voltstrider", false},
	{"Paranormal Analyst", "trigger#0.0", "trigger.manifest-dread", "Manifest Dread", false},
	{"Curator of Sun's Creation", "trigger#0.0", "trigger.discover", "Daring Discovery", false},
	{"Avatar Aang", "trigger#0.0", "trigger.elemental-bend", "", false},
	{"Aloe Alchemist", "trigger#0.0", "trigger.becomes-plotted", "", true},
	{"Longhorn Sharpshooter", "trigger#0.0", "trigger.becomes-plotted", "", true},
	{"Stubborn Burrowfiend", "trigger#0.0", "trigger.becomes-saddled", "", false},
}

// TestKeywordActionTriggerRecipes serves one card per new recipe and proves
// each item (1) is classified into the sub-family, (2) names its probe in the
// cause steps, (3) plays through gorge, (4) fires the row's own trigger slot
// -- which the same replay with the card removed does not.
func TestKeywordActionTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range keywordActionTriggerCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			onBf, inHand := inZone(p0.Battlefield, tc.name), inZone(p0.Hand, tc.name)
			if tc.inHand && (onBf || !inHand) || !tc.inHand && (!onBf || inHand) {
				t.Fatalf("precondition: %s battlefield=%v hand=%v, want inHand=%v", tc.name, p0.Battlefield, p0.Hand, tc.inHand)
			}
			if tc.probe != "" {
				found := false
				for _, st := range it.Scenario.Steps {
					found = found || strings.HasSuffix(st.Card, ":"+tc.probe)
				}
				if !found {
					t.Fatalf("no step names probe %s: %+v", tc.probe, it.Scenario.Steps)
				}
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			slot := tc.key[strings.LastIndexByte(tc.key, '.')+1:]
			if !keywordSlotOnStack(t, reg, it.Scenario, tc.name, slot) {
				t.Fatalf("no snapshot shows trigger %s on the stack sourced by %s", slot, tc.name)
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

// keywordSlotOnStack replays the item's cause in the detection step shapes the
// generator itself accepts (bare, one pass round, two pass rounds, and each
// with a priority checkpoint) and reports whether a snapshot shows the row's
// own trigger slot sourced by name. Two pass rounds are needed when the cause
// is a probe spell whose own enters trigger must resolve before the row
// trigger is queued (an explore probe); Settle appends the resolves that
// empty the stack after each shape.
func keywordSlotOnStack(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name, slot string) bool {
	t.Helper()
	cause := sc.Steps
	for len(cause) > 0 && cause[len(cause)-1].Op == "resolve" {
		cause = cause[:len(cause)-1]
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	withPasses := append(append([]oraclegen.Step(nil), cause...), passes...)
	deep := append(append([]oraclegen.Step(nil), withPasses...), passes...)
	deepCheckpoint := append(append([]oraclegen.Step(nil), deep...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	variants := [][]oraclegen.Step{
		cause,
		withPasses,
		deep,
		deepCheckpoint,
		append(append([]oraclegen.Step(nil), withPasses...), oraclegen.Step{Op: "pass_to", Decision: "priority"}),
		append(append([]oraclegen.Step(nil), cause...), oraclegen.Step{Op: "pass_to", Decision: "priority"}),
	}
	for _, steps := range variants {
		try := sc
		try.Steps = steps
		_, res, ok := oraclegen.Settle(reg, try)
		if !ok {
			continue
		}
		for _, snap := range res.Snapshots {
			for _, entry := range snap.Stack {
				if entry.Kind == "ability" && entry.Source == "p0:"+name && entry.Trigger == slot {
					return true
				}
			}
		}
	}
	return false
}
