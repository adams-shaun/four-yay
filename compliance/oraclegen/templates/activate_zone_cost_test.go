package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestActivateZoneCostPayments(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, zone string
	}{
		{"Dire Flail", "activate#0.1", "graveyard"},
		{"Rubble Rouser", "activate#0.0", "graveyard"},
		{"Forensic Researcher", "activate#0.1", "graveyard"},
		{"Reverberating Summons", "activate#0.0", "hand"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s is absent from corpus", tc.name)
			}
			it, req := activateRequirement(t, reg, tc.name, tc.key)
			idx := 0
			if _, err := strconv.Atoi(req.Slot); err != nil {
				t.Fatalf("precondition: bad ability slot %q: %v", req.Slot, err)
			} else {
				idx, _ = strconv.Atoi(req.Slot)
			}
			if req.Face < 0 || req.Face >= len(card.Faces) || idx >= len(card.Faces[req.Face].Abilities) || !card.Faces[req.Face].Abilities[idx].IsActivated() {
				t.Fatalf("precondition: %s %s is not an activated ability", tc.name, tc.key)
			}
			if it.Card != tc.name || it.Template != tc.key || len(it.Scenario.Steps) < 1 || it.Scenario.Steps[0].Op != "activate" {
				t.Fatalf("precondition: malformed generated activation scenario: %+v", it)
			}
			cost := card.Faces[req.Face].Abilities[idx].ParamStr(cards.PKCost)
			fixtures, answerKind := activationCostFixturesForTest(cost)
			if len(fixtures) == 0 {
				t.Fatalf("precondition: no fixture selected for supported cost %q", cost)
			}
			seat := it.Scenario.Setup["p0"]
			if !containsFold(seat.Battlefield, tc.name) {
				t.Fatalf("precondition: activation source %q is not on p0's battlefield: %v", tc.name, seat.Battlefield)
			}
			zone := seat.Graveyard
			if tc.zone == "hand" {
				zone = seat.Hand
			}
			for _, name := range fixtures {
				if !containsFold(zone, name) {
					t.Errorf("selected cost fixture %q absent from p0 %s: %v", name, tc.zone, zone)
				}
				answers := 0
				for _, stepAnswers := range it.XAnswers {
					for _, answer := range stepAnswers {
						if answer.Kind == answerKind && strings.EqualFold(answer.Value, name) {
							answers++
						}
					}
				}
				if answers == 0 {
					t.Errorf("XMage %s answer for cost fixture %q is absent: %+v", answerKind, name, it.XAnswers)
				}
			}
		})
	}
}

func activationCostFixturesForTest(cost string) ([]string, string) {
	for _, tok := range costTokens(cost) {
		if names := activationCostFixtures(tok); len(names) != 0 {
			head, _, _ := strings.Cut(tok, "<")
			return names, costAnswerKind(cost, head)
		}
	}
	return nil, ""
}

func containsFold(names []string, want string) bool {
	for _, name := range names {
		if strings.EqualFold(name, want) {
			return true
		}
	}
	return false
}
