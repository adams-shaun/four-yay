// Focused tests for the cast-self prelude this ticket adds: a trigger whose
// CheckSVar$ counts a self attribute (prepared, suspected) that the
// setup-placed source cannot show -- the upkeep "becomes prepared" firing
// consumed it during the setup drive, or setup placement dropped the enter
// suspect grant -- is served by casting the source on turn 1, and gorge puts
// the trigger on the stack and plays the scenario through.
package templates

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestTriggerSelfCastAttributeGate drives the FRA prepare creatures and the
// MKM suspect pair in the ticket's rows file. Each gate is asserted to be the
// self-attribute count it claims, the scenario must start the source in HAND
// (a fresh self, not the consumed setup placement), and the item must play
// through gorge with the trigger on the stack.
func TestTriggerSelfCastAttributeGate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, attr string
	}{
		{"Paradox Shaper", "trigger#0.0", "prepared"},
		{"Stingerquill Voxmancer", "trigger#0.0", "prepared"},
		{"Woodwork Prodigy", "trigger#0.0", "prepared"},
		{"Frantic Scapegoat", "trigger#0.1", "suspected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s not in the corpus", tc.name)
			}
			f := card.Faces[0]
			var req levelb.Requirement
			for _, r := range levelb.Requirements(card) {
				if r.Key == tc.key {
					req = r
				}
			}
			if (levelb.Requirement{}) == req {
				t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
			}
			// Precondition: the gate really counts a self attribute the card's
			// own trigger grants, or the cast-self premise would be vacuous.
			idx, err := strconv.Atoi(req.Slot)
			if err != nil || idx < 0 || idx >= len(f.Triggers) {
				t.Fatalf("precondition: slot %q does not name a trigger", req.Slot)
			}
			if attr := selfCastAttribute(f, &f.Triggers[idx]); attr != tc.attr {
				t.Fatalf("precondition: %s %s gate reads %q, want %q", tc.name, tc.key, attr, tc.attr)
			}
			it := triggerSelfCastItem(t, reg, tc.name, tc.key)
			sc := it.Scenario
			// The source starts in hand and is cast: the fresh-cast premise.
			if countName(sc.Setup["p0"].Battlefield, tc.name) != 0 {
				t.Fatalf("%s: source placed on the battlefield, want in hand only", tc.name)
			}
			if countName(sc.Setup["p0"].Hand, tc.name) != 1 {
				t.Fatalf("%s: hand %v, want the source once", tc.name, sc.Setup["p0"].Hand)
			}
			cast := false
			for _, st := range sc.Steps {
				if st.Op == "cast" && st.Card == "p0:"+tc.name {
					cast = true
				}
			}
			if !cast {
				t.Fatalf("%s: scenario never casts the source: %v", tc.name, sc.Steps)
			}
		})
	}
}

// triggerSelfCastItem serves one trigger requirement and proves the served
// scenario puts the trigger on the stack. The cast and resolve steps of the
// cause resolve the enter triggers too, so the visible point is the pass pair
// after them (the fire probe's own settled variant), not a step boundary of
// the item's own resolves.
func triggerSelfCastItem(t *testing.T, reg *cards.Registry, name, key string) oraclegen.Item {
	t.Helper()
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	f := card.Faces[0]
	for _, req := range levelb.Requirements(card) {
		if req.Family != "trigger" || req.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, req)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
		}
		// The item's own trailing resolves swallow the fired trigger inside a
		// resolve op (the runner resolves the whole stack); replace them with
		// a pass pair, the fire probe's settled variant, at which the trigger
		// is queued and visible on the stack.
		steps := append([]oraclegen.Step(nil), it.Scenario.Steps...)
		for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
			steps = steps[:len(steps)-1]
		}
		sc := it.Scenario
		sc.Steps = append(steps,
			oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1})
		_, res, ok := oraclegen.Settle(reg, sc)
		if !ok {
			t.Fatalf("%s %s: gorge cannot play the served scenario with a pass pair", name, key)
		}
		if !abilityOnStack(res.Snapshots, stackSourceWants(reg, name, f), req.Slot) {
			t.Fatalf("%s %s: the trigger ability is never on the stack", name, key)
		}
		return it
	}
	t.Fatalf("precondition: %s has no requirement %s", name, key)
	return oraclegen.Item{}
}
