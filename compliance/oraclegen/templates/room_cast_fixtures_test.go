package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestOracleRoomCastFixturesGenerate pins the two DSK cast-resolve fixtures
// that became generatable with the cast-face provenance fix in 4176f596b.
func TestOracleRoomCastFixturesGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	cases := []struct {
		catalogue string
		front     string
		back      string
	}{
		{"Central Elevator // Promising Stairs", "Central Elevator", "Promising Stairs"},
		{"Smoky Lounge // Misty Salon", "Smoky Lounge", "Misty Salon"},
	}
	for _, tc := range cases {
		t.Run(tc.catalogue, func(t *testing.T) {
			card, ok := reg.Lookup(tc.catalogue)
			if !ok || len(card.Faces) != 2 {
				t.Fatalf("precondition: %q must exist with two faces; found=%t faces=%d", tc.catalogue, ok, len(card.Faces))
			}
			if card.Faces[0].Name != tc.front || card.Faces[1].Name != tc.back {
				t.Fatalf("precondition: faces = %q // %q, want %q // %q", card.Faces[0].Name, card.Faces[1].Name, tc.front, tc.back)
			}

			it, skip := Generate(reg, tc.catalogue)
			if skip != nil {
				t.Fatalf("unexpected skip: %s", skip.Reason)
			}
			castAt := -1
			for i, step := range it.Steps {
				if step.Op == "cast" && step.Card == "p0:"+tc.catalogue {
					castAt = i
					break
				}
			}
			if castAt < 0 {
				t.Fatalf("scenario does not cast catalogue card with front face %q: %+v", tc.front, it.Steps)
			}

			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 {
				t.Fatalf("scenario replay err=%v fails=%v", err, res.Fails)
			}
			if castAt+1 >= len(res.Snapshots) {
				t.Fatalf("precondition: missing post-cast snapshot for %q", tc.front)
			}
			before, after := 0, 0
			for _, name := range res.Snapshots[castAt].Players[0].Hand {
				if name == tc.front {
					before++
				}
			}
			for _, name := range res.Snapshots[castAt+1].Players[0].Hand {
				if name == tc.front {
					after++
				}
			}
			if before == 0 || after >= before {
				t.Fatalf("precondition: cast did not move %q out of hand: before=%d after=%d", tc.front, before, after)
			}
		})
	}
}
