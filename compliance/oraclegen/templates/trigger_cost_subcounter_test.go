package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestTriggerSubCounterCostPays pins that the level-B trigger template pays a
// triggered "remove a counter from this" cost (Cost$ SubCounter /
// RemoveAnyCounter on the source) instead of declining it. The pay option is
// index 0 of the trigger-cost ask, so the generic mayYes answer picks it; the
// assertion is on the paid post-state (counters left, cards drawn), not on a
// label.
func TestTriggerSubCounterCostPays(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		kind      string // counter the cost removes
		wantLeft  int32  // counters left once the cost was paid
		wantDraw  bool   // the paid branch draws a card
	}{
		{"Guiding Hydra", "trigger#0.0", "P1P1", 1, false},
		{"Ingenious Prodigy", "trigger#0.0", "P1P1", 1, true},
		{"Slumbering Walker", "trigger#0.0", "M1M1", 1, false},
		{"Leatherhead, Swamp Stalker", "trigger#0.0", "HEXPROOF", 0, false},
		{"Leatherhead, Swamp Stalker", "combat#0.attack", "HEXPROOF", 0, false},
	} {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.name, requirementByKey(t, reg, tc.name, tc.key))
			if skip != nil {
				t.Fatalf("%s %s: %s", tc.name, tc.key, skip.Reason)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 || len(res.Snapshots) < 2 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v snaps=%d", ok, res.Fails, len(res.Snapshots))
			}
			// Precondition: the source reaches p0's battlefield holding more
			// of the counter than the paid state leaves, and its hand at that
			// point is the baseline the draw is measured against.
			var most int32
			onBF := false
			var handBefore []string
			for _, s := range res.Snapshots {
				if p := sourcePerm(s, tc.name); p != nil {
					if !onBF {
						handBefore = s.Players[0].Hand
					}
					onBF = true
					most = max(most, p.Counters[tc.kind])
				}
			}
			if !onBF || most < 1 || most <= tc.wantLeft {
				t.Fatalf("precondition: %s on p0's battlefield=%v with at most %d %s, want >= 1 and more than the %d left after paying", tc.name, onBF, most, tc.kind, tc.wantLeft)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			p := sourcePerm(last, tc.name)
			if p == nil {
				t.Fatalf("%s is not on p0's battlefield after the scenario", tc.name)
			}
			if got := p.Counters[tc.kind]; got != tc.wantLeft {
				t.Errorf("%s %s after resolve = %d, want %d (cost paid); counters=%v", tc.name, tc.kind, got, tc.wantLeft, p.Counters)
			}
			if tc.wantDraw && len(last.Players[0].Hand) <= len(handBefore) {
				t.Errorf("hand %v -> %v, want the paid branch to draw", handBefore, last.Players[0].Hand)
			}
		})
	}
}

func sourcePerm(s rules.OracleSnapshot, name string) *rules.OracleSnapPerm {
	for i := range s.Permanents {
		if s.Permanents[i].Name == name && s.Permanents[i].Controller == 0 {
			return &s.Permanents[i]
		}
	}
	return nil
}
