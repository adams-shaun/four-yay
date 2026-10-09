package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func countName(xs []string, name string) int {
	n := 0
	for _, x := range xs {
		if x == name {
			n++
		}
	}
	return n
}

// countTyped counts the names in xs whose printed face has the type word.
func countTyped(t *testing.T, reg *cards.Registry, xs []string, word string) int {
	t.Helper()
	n := 0
	for _, x := range xs {
		c, ok := reg.Lookup(x)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", x)
		}
		for _, ty := range c.Faces[0].Types {
			if ty == word {
				n++
				break
			}
		}
	}
	return n
}

// triggerItem serves one trigger requirement and returns its scenario, and
// proves gorge shows the trigger's ability on the stack while playing it.
func triggerItem(t *testing.T, reg *cards.Registry, name, key string) oraclegen.Item {
	t.Helper()
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	for _, req := range levelb.Requirements(card) {
		if req.Family != "trigger" || req.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, req)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
		}
		// The item ends with the resolves that empty the stack: dropping them
		// one at a time must reach a state with the trigger's ability on it.
		onStack := false
		sc := it.Scenario
		steps := append([]oraclegen.Step(nil), sc.Steps...)
		for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" && !onStack {
			steps = steps[:len(steps)-1]
			sc.Steps = steps
			_, res, ok := oraclegen.Settle(reg, sc)
			if !ok {
				t.Fatalf("%s %s: gorge cannot play the served scenario minus its last resolve", name, key)
			}
			onStack = abilityOnStack(res.Snapshots, stackSourceWants(reg, name, card.Faces[0]), req.Slot)
		}
		if !onStack {
			t.Fatalf("%s %s: the trigger ability is never on the stack in %+v", name, key, it.Scenario.Steps)
		}
		return it
	}
	t.Fatalf("precondition: %s has no requirement %s", name, key)
	return oraclegen.Item{}
}

// TestTriggerConditionBoardFixtures: a conditional trigger is served with the
// board its own filter and count ask for, the fixture survives scenario
// construction with its multiplicity, and gorge puts the trigger on the stack.
func TestTriggerConditionBoardFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		check     func(t *testing.T, p0, p1 oraclegen.Seat, steps []oraclegen.Step)
	}{
		{"Dragonmaster Outcast", "trigger#0.0", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// "Land.YouCtrl GE6": six lands, a basic repeated, the source once.
			if n := countTyped(t, reg, p0.Battlefield, "Land"); n != 6 {
				t.Fatalf("battlefield %v has %d lands, want 6", p0.Battlefield, n)
			}
			if countName(p0.Battlefield, "Forest") != 2 || countName(p0.Battlefield, "Dragonmaster Outcast") != 1 {
				t.Fatalf("battlefield %v: want two Forests and the source once", p0.Battlefield)
			}
		}},
		{"Golbez, Crystal Collector", "trigger#0.1", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// CheckSVar X = Count$Valid Artifact.YouCtrl, GE4.
			if n := countTyped(t, reg, p0.Battlefield, "Artifact"); n < 4 {
				t.Fatalf("battlefield %v has %d artifacts, want 4", p0.Battlefield, n)
			}
		}},
		{"Katara, Bending Prodigy", "trigger#0.0", func(t *testing.T, p0, p1 oraclegen.Seat, steps []oraclegen.Step) {
			// "PresentDefined$ Self | IsPresent$ Card.tapped": the source
			// attacks and stays tapped to its end step.
			atk := attackStep(t, steps).Attackers
			if len(atk) != 1 || atk[0] != "p0:Katara, Bending Prodigy" {
				t.Fatalf("attackers = %v, want the source", atk)
			}
		}},
		{"Airbender Ascension", "trigger#0.2", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// "Card.Self+counters_GE4_QUEST" on a phase trigger.
			if got := p0.Counters["Airbender Ascension"]["QUEST"]; got != 4 {
				t.Fatalf("counters %v, want 4 QUEST on the source", p0.Counters)
			}
		}},
		{"Joined Researchers", "trigger#0.0", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// "an opponent has more cards in hand than you".
			if len(p1.Hand) <= len(p0.Hand) || len(p1.Hand) < 2 {
				t.Fatalf("hands p0=%v p1=%v, want p1 holding more", p0.Hand, p1.Hand)
			}
		}},
		{"Serah Farron", "trigger#0.0", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// "Creature.Other+Legendary+YouCtrl GE2".
			others := removeString(append([]string(nil), p0.Battlefield...), "Serah Farron")
			if n := countTyped(t, reg, others, "Legendary"); n < 2 || countTyped(t, reg, others, "Creature") < 2 {
				t.Fatalf("battlefield %v has %d other legendary creatures, want 2", p0.Battlefield, n)
			}
		}},
		{"Discerning Financier", "trigger#0.0", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// "an opponent controls more lands than you".
			if countTyped(t, reg, p1.Battlefield, "Land") <= countTyped(t, reg, p0.Battlefield, "Land") {
				t.Fatalf("p0 %v p1 %v, want p1 with more lands", p0.Battlefield, p1.Battlefield)
			}
		}},
		{"Desert Were-Worm", "trigger#0.0", func(t *testing.T, p0, p1 oraclegen.Seat, steps []oraclegen.Step) {
			// "attacking creatures with total power 12 or greater".
			atk := attackStep(t, steps).Attackers
			if len(atk) < 2 || !containsString(p0.Battlefield, strings.TrimPrefix(atk[1], "p0:")) {
				t.Fatalf("attackers %v on %v: want the source with fillers placed", atk, p0.Battlefield)
			}
		}},
		{"Emet-Selch, Unsundered", "trigger#0.2", func(t *testing.T, p0, p1 oraclegen.Seat, _ []oraclegen.Step) {
			// Fourteen graveyard cards, and the source cast on turn 1: a
			// setup-placed source would transform in setup's turn-1 upkeep.
			if len(p0.Graveyard) < 14 || !containsString(p0.Hand, "Emet-Selch, Unsundered") || containsString(p0.Battlefield, "Emet-Selch, Unsundered") {
				t.Fatalf("p0 %+v: want 14 graveyard cards and the source in hand", p0)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerItem(t, reg, tc.name, tc.key)
			tc.check(t, it.Scenario.Setup["p0"], it.Scenario.Setup["p1"], it.Scenario.Steps)
		})
	}
}

// TestTriggerConditionBoardSkips: a gate the engine cannot read or no setup
// reaches is skipped with its own named reason, not a fixture.
func TestTriggerConditionBoardSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, want string }{
		// Paradox Shaper moved to the served side: its consumed self-attribute
		// gate is a cast-self cause now (trigger_condition_selfcast_test.go).
		{"Case of the Market Melee", "trigger#0.2", "needs a solved Case"},
	} {
		card, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", tc.name)
		}
		found := false
		for _, req := range levelb.Requirements(card) {
			if req.Family != "trigger" || req.Key != tc.key {
				continue
			}
			found = true
			it, skip := GenerateB(reg, tc.name, req)
			if skip == nil {
				t.Fatalf("%s %s was served (%s), want a skip", tc.name, tc.key, it.ID)
			}
			if !strings.Contains(skip.Reason, tc.want) {
				t.Fatalf("%s %s skip = %q, want %q", tc.name, tc.key, skip.Reason, tc.want)
			}
		}
		if !found {
			t.Fatalf("precondition: %s has no requirement %s", tc.name, tc.key)
		}
	}
}

// TestAppendFixtureCounts: repeats a count-aware fixture asks for are kept,
// a zone's existing copy (the source) counts toward them, and names a cause
// lists twice by accident are not added beyond what was asked.
func TestAppendFixtureCounts(t *testing.T) {
	got := appendFixtureCounts([]string{"Source", "Forest"}, []string{"Forest", "Forest", "Island", "Source"})
	want := []string{"Source", "Forest", "Forest", "Island"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
