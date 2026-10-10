package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestActivateLevelBRowsServed pins level-B activate rows that generate and
// replay gorge-side today (ticket agent-20261009T085207Z-4735cfec): the four
// MKM Case "Solved" rows and Kaito, Bane of Nightmares' three loyalty rows.
// Each case asserts the PRECONDITION the mechanism depends on (the ability
// line's gate or cost), then that the item generates (no skip) and plays
// through gorge with zero fails.
//
// Hauntwoods Shrieker activate#0.0 is deliberately NOT pinned: its
// `ValidTgts$ Card.faceDown` row still skips ("no fixture ... targets
// [Card.faceDown]"), the brief's claim that DSK has no level-B skips was
// measured wrong.
func TestActivateLevelBRowsServed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		lineHas   string // substring of the ability line (the served shape)
		wantOps   []string
	}{
		{"Case of the Burning Masks", "activate#0.0", "Solved", []string{"pass_to", "resolve"}},
		{"Case of the Filched Falcon", "activate#0.0", "Solved", []string{"pass_to", "resolve"}},
		{"Case of the Stashed Skeleton", "activate#0.0", "Solved", []string{"pass_to", "resolve"}},
		{"Case of the Uneaten Feast", "activate#0.0", "Solved", []string{"pass_to", "resolve"}},
		{"Kaito, Bane of Nightmares", "activate#0.0", "LOYALTY", nil},
		{"Kaito, Bane of Nightmares", "activate#0.1", "LOYALTY", nil},
		{"Kaito, Bane of Nightmares", "activate#0.2", "LOYALTY", nil},
	} {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			req := activateRowRequirement(t, reg, tc.name, tc.key)
			sa := abilityOf(t, reg, tc.name, req)
			if !strings.Contains(sa.Line, tc.lineHas) {
				t.Fatalf("precondition: %s %s ability line names no %q: %.160s", tc.name, tc.key, tc.lineHas, sa.Line)
			}
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s %s skipped: %s", tc.name, tc.key, skip.Reason)
			}
			if !seatHasName(it.Scenario.Setup["p0"].Battlefield, tc.name) {
				t.Fatalf("%s: source not on p0's battlefield: %v", tc.name, it.Scenario.Setup["p0"].Battlefield)
			}
			for _, op := range tc.wantOps {
				if !stepsHaveOp(it.Scenario.Steps, op) {
					t.Fatalf("%s %s carries no %q step: %v", tc.name, tc.key, op, it.Scenario.Steps)
				}
			}
			if !stepsHaveOp(it.Scenario.Steps, "activate") {
				t.Fatalf("%s %s names no activate step: %v", tc.name, tc.key, it.Scenario.Steps)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("%s %s does not play through gorge: ok=%v fails=%v", tc.name, tc.key, ok, res.Fails)
			}
		})
	}
}

// TestActivateGraspingShadowsDreadCost serves Shadows' Lair's
// "{B}, {T}, Remove a dread counter" draw (LCI Grasping Shadows
// activate#1.1). The cost spells the kind `Dread` while the front face places
// `DREAD`; before state.CanonicalCounterKind folded the cost's spelling the
// removal read zero counters and the ability was never offered, so the row
// skipped as "no fixture gorge can activate".
func TestActivateGraspingShadowsDreadCost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Grasping Shadows"
	req := activateRowRequirement(t, reg, name, "activate#1.1")
	if req.Face != 1 {
		t.Fatalf("precondition: activate#1.1 is on face %d, want the back face 1", req.Face)
	}
	sa := abilityOf(t, reg, name, req)
	if !strings.Contains(sa.Line, "SubCounter<1/Dread>") || !strings.Contains(sa.Line, "AB$ Draw") {
		t.Fatalf("precondition: ability line is not the Dread-cost Draw: %.160s", sa.Line)
	}
	it, skip := GenerateB(reg, name, req)
	if skip != nil {
		t.Fatalf("%s activate#1.1 skipped: %s", name, skip.Reason)
	}
	p0 := it.Scenario.Setup["p0"]
	if !seatHasName(p0.BackFace, name) {
		t.Fatalf("source is not placed on its back face: %+v", p0)
	}
	if len(p0.Hand) != 0 {
		t.Fatalf("precondition: p0 starts with hand %v; the draw assertion needs it empty", p0.Hand)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	first, last := res.Snapshots[0], res.Snapshots[len(res.Snapshots)-1]
	// The ability resolved: one card drawn and one life lost (the DBLoseLife
	// sub-ability), and the dread counter paid as the cost is gone.
	if got := len(last.Players[0].Hand) - len(first.Players[0].Hand); got != 1 {
		t.Errorf("p0 hand grew by %d, want 1 (the Draw resolved)", got)
	}
	if got := first.Players[0].Life - last.Players[0].Life; got != 1 {
		t.Errorf("p0 lost %d life, want 1 (the LoseLife sub-ability)", got)
	}
	for _, p := range last.Permanents {
		if p.Name != name && p.Name != "Shadows' Lair" {
			continue
		}
		if n := p.Counters["DREAD"]; n != 0 {
			t.Errorf("%s still carries %d dread counters after paying the cost", p.Name, n)
		}
	}
	for _, p := range first.Permanents {
		if (p.Name == name || p.Name == "Shadows' Lair") && p.Counters["DREAD"] == 1 {
			return
		}
	}
	t.Errorf("precondition: no source permanent carried one DREAD counter at setup: %+v", first.Permanents)
}
