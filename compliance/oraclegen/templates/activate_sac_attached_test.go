package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

// TestActivateSacAttachedCost generates the level-B activate items for the two
// attached-Sac cost cards (Ronin, Shadow Stalker's Equipment and Faunsbane
// Troll's Aura). Both were a `Sac<attached>` cost gap before this ticket: the
// template had no fixture that puts an Aura/Equipment on the source and
// attaches it.
func TestActivateSacAttachedCost(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, fixture string
	}{
		{"Ronin, Shadow Stalker", "activate#0.1", "Bonesplitter"},
		{"Faunsbane Troll", "activate#0.0", "Unholy Strength"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, req := activateRequirement(t, reg, tc.name, tc.key)
			// Precondition: the named card actually carries an attached-Sac
			// cost, or this test would pass on a template that changed under
			// it.
			c, ok := reg.Lookup(tc.name)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s is missing from the corpus", tc.name)
			}
			idx := 0
			if n, err := strconv.Atoi(req.Slot); err == nil {
				idx = n
			}
			if idx >= len(c.Faces[0].Abilities) {
				t.Fatalf("precondition: %s ability index %d out of range", tc.name, idx)
			}
			cost := c.Faces[0].Abilities[idx].ParamStr(cards.PKCost)
			if !strings.Contains(cost, ".Attached") {
				t.Fatalf("precondition: %s ability %d cost %q has no attached-Sac", tc.name, idx, cost)
			}
			// The fixture card is on p0's battlefield and the prelude attaches
			// it to the source.
			seat := it.Scenario.Setup["p0"]
			if !containsString(seat.Battlefield, tc.fixture) {
				t.Fatalf("attached fixture %q absent from p0 battlefield: %v", tc.fixture, seat.Battlefield)
			}
			sawAttach := false
			for _, st := range it.Scenario.Steps {
				if st.Op == "attach" && st.Card == "p0:"+tc.fixture && st.AttachedTo == "p0:"+tc.name {
					sawAttach = true
				}
			}
			if !sawAttach {
				t.Fatalf("no attach step for %q to %s in scenario steps %+v", tc.fixture, tc.name, it.Scenario.Steps)
			}
			// The XMage sacrifice answer names the attached fixture exactly
			// once.
			matches := 0
			for _, stepAnswers := range it.XAnswers {
				for _, answer := range stepAnswers {
					if strings.EqualFold(answer.Value, tc.fixture) {
						matches++
					}
				}
			}
			if matches != 1 {
				t.Fatalf("XMage answers name attached fixture %q %d times, want once: %+v", tc.fixture, matches, it.XAnswers)
			}
			// The scenario replays cleanly: the attached fixture is sacrificed
			// and the ability resolves.
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("scenario does not replay: err=%v fails=%v", err, res.Fails)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			for _, p := range last.Permanents {
				if p.Name == tc.fixture {
					t.Fatalf("attached fixture %q still on the battlefield after the sacrifice", tc.fixture)
				}
			}
		})
	}
}
