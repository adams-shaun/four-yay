package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// Level B D8: a triggered "remove a counter from this creature" cost
// (Cost$ SubCounter<1/P1P1>, Cost$ RemoveAnyCounter<1/Any/CARDNAME>) is offered
// as a pay option at index 0 of the trigger-cost ask, so the generic mayYes
// answer in the trigger template pays it. The assertions read the counters and
// the hand, not the answer label, so a regression to the decline is caught by
// the board.

func levelBItem(t *testing.T, name, key string) oraclegen.Item {
	t.Helper()
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s: %s", name, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return oraclegen.Item{}
}

func snapPerm(snap rules.OracleSnapshot, name string) (rules.OracleSnapPerm, bool) {
	for _, p := range snap.Permanents {
		if p.Name == name && p.Controller == 0 {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

func TestTriggerSubCounterCostPays(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, counter string
		before, after      int32
		draws              bool
	}{
		{"Guiding Hydra", "trigger#0.0", "P1P1", 2, 1, false},
		{"Ingenious Prodigy", "trigger#0.0", "P1P1", 2, 1, true},
		{"Slumbering Walker", "trigger#0.0", "M1M1", 2, 1, false},
		{"Leatherhead, Swamp Stalker", "trigger#0.0", "HEXPROOF", 1, 0, false},
		{"Leatherhead, Swamp Stalker", "combat#0.attack", "HEXPROOF", 1, 0, false},
	} {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := levelBItem(t, tc.name, tc.key)
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}

			// The last step carrying a scripted answer is where the trigger
			// cost is decided -- "Remove 1 …" when paid, "Do not pay" when
			// declined; it is the trigger's resolve, or the pass_to that
			// carries a combat trigger. Everything before it only builds the
			// board. Located by the presence of an answer, not by its label,
			// so a decline still reaches the counter assertions below.
			ask := -1
			for i, st := range it.Scenario.Steps {
				if len(st.Answers) > 0 {
					ask = i
				}
			}
			if ask < 0 {
				t.Fatalf("precondition: the scenario answers no ask: %+v", it.Scenario.Steps)
			}

			pre := runSteps(t, reg, it.Scenario, it.Scenario.Steps[:ask])
			preSnap := pre.Snapshots[len(pre.Snapshots)-1]
			preSrc, ok := snapPerm(preSnap, tc.name)
			if !ok {
				t.Fatalf("precondition: %s is not on p0's battlefield before the ask", tc.name)
			}
			if got := preSrc.Counters[tc.counter]; got != tc.before || got < 1 {
				t.Fatalf("precondition: %s holds %s=%d before the ask, want %d (>=1)", tc.name, tc.counter, got, tc.before)
			}
			preHand := len(preSnap.Players[0].Hand)

			res := runSteps(t, reg, it.Scenario, it.Scenario.Steps)
			post := res.Snapshots[len(res.Snapshots)-1]
			src, ok := snapPerm(post, tc.name)
			if !ok {
				t.Fatalf("%s left the battlefield", tc.name)
			}
			if got := src.Counters[tc.counter]; got != tc.after {
				t.Fatalf("%s %s=%d after resolve, want %d (cost not paid)", tc.name, tc.counter, got, tc.after)
			}
			if tc.draws {
				if got := len(post.Players[0].Hand); got != preHand+1 {
					t.Fatalf("%s hand %d -> %d, want one card drawn", tc.name, preHand, got)
				}
			}
		})
	}
}
