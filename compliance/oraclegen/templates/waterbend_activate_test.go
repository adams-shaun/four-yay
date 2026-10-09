package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/rules"
)

// The waterbend-cost activate items (ticket h7-driver-answers): the declined
// tap-helpers ask is no longer scripted (XMage pays the waterbend generic
// from the prefilled pool), and what XMage DOES ask is re-scripted by
// activate_waterbend.go. Each case pins the item's xmage answers and proves
// the gorge-side scenario still replays and shows the effect the scenario
// compares.
func TestWaterbendActivateXMageAnswers(t *testing.T) {
	cases := []struct {
		card, key     string
		wantStep0     string // JSON of the activate step's xmage answers
		checkSnapshot func(t *testing.T, res rules.OracleResult)
	}{
		{
			// Aang, Swift Savior: Waterbend {8}: Transform Aang. No ask on
			// either engine beyond the helpers, so the step scripts nothing
			// (the old "no"+skip pair broke XMage's next dialog).
			card: "Aang, Swift Savior", key: "activate#0.0",
			wantStep0: "[]",
			checkSnapshot: func(t *testing.T, res rules.OracleResult) {
				last := res.Snapshots[len(res.Snapshots)-1]
				b, _ := json.Marshal(last)
				if !strings.Contains(string(b), "Aang and La, Ocean's Fury") {
					t.Fatalf("precondition: the activation did not transform Aang (last snapshot %s)", b)
				}
			},
		},
		{
			// Katara, Water Tribe's Hope: Waterbend<X> announces X through
			// XMage's getAmount, which reads an "X=1" choice.
			card: "Katara, Water Tribe's Hope", key: "activate#0.0",
			wantStep0: `[{"seat":0,"kind":"choice","value":"X=1"}]`,
			checkSnapshot: func(t *testing.T, res rules.OracleResult) {
				last := res.Snapshots[len(res.Snapshots)-1]
				b, _ := json.Marshal(last)
				if !strings.Contains(string(b), "1/1") {
					t.Fatalf("precondition: the X=1 base P/T is not on the board (%s)", b)
				}
			},
		},
		{
			// Water Tribe Rallier: the dig's "you may reveal" pick is an
			// optional target on XMage, closed with a queued skip; gorge
			// declined it, so the looked-at cards never reach the hand.
			card: "Water Tribe Rallier", key: "activate#0.0",
			wantStep0: `[{"seat":0,"kind":"target","value":"[target_skip]"}]`,
			checkSnapshot: func(t *testing.T, res rules.OracleResult) {
				last := res.Snapshots[len(res.Snapshots)-1]
				b, _ := json.Marshal(last)
				if strings.Contains(string(b), "hand") && strings.Contains(string(b), "Grizzly Bears") {
					// The top library card must not have been revealed into
					// the hand; the dig's rest went to the bottom instead.
					var snap map[string]any
					if err := json.Unmarshal(b, &snap); err != nil {
						t.Fatal(err)
					}
					if hand, ok := snap["p0_hand"].(string); ok && strings.Contains(hand, "Grizzly Bears") {
						t.Fatalf("the declined reveal moved a card into the hand (%s)", hand)
					}
				}
			},
		},
		{
			// Giant Koi: "This creature can't be blocked this turn" asks
			// nothing on XMage, and neither engine's snapshot projects the
			// one-turn restriction, so the compared board is the creature
			// staying in play with its pool spent; the step scripts nothing.
			card: "Giant Koi", key: "activate#0.0",
			wantStep0: "[]",
			checkSnapshot: func(t *testing.T, res rules.OracleResult) {
				last := res.Snapshots[len(res.Snapshots)-1]
				b, _ := json.Marshal(last)
				if !strings.Contains(string(b), "p0:Giant Koi") || strings.Contains(string(b), "\"pool\":\"CCC\"") {
					t.Fatalf("precondition: Giant Koi not on the battlefield or the pool did not empty (%s)", b)
				}
			},
		},
		{
			// North Pole Patrol's waterbend tap: the mandatory target is
			// carried by the step's own targets, so nothing extra.
			card: "North Pole Patrol", key: "activate#0.1",
			wantStep0: "[]",
		},
	}
	reg := loadGenRegistry(t)
	for _, tc := range cases {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("%s not in corpus", tc.card)
			}
			var req levelb.Requirement
			for _, r := range levelb.Requirements(c) {
				if r.Key == tc.key {
					req = r
				}
			}
			if req.Key == "" {
				t.Fatalf("no requirement %s", tc.key)
			}
			it, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("skip: %s", skip.Reason)
			}
			got, err := json.Marshal(it.XAnswers[0])
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantStep0 == "[]" {
				if len(it.XAnswers[0]) != 0 {
					t.Fatalf("step 0 xmage answers = %s, want none", got)
				}
			} else if string(got) != tc.wantStep0 {
				t.Fatalf("step 0 xmage answers = %s, want %s", got, tc.wantStep0)
			}
			// Precondition: the raw scenario is unchanged by the answer
			// edits and still replays, ending with the compared effect.
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil {
				t.Fatalf("gorge replay: %v", err)
			}
			if len(res.Fails) != 0 {
				t.Fatalf("gorge replay fails: %v", res.Fails)
			}
			if tc.checkSnapshot != nil {
				tc.checkSnapshot(t, res)
			}
		})
	}
}
