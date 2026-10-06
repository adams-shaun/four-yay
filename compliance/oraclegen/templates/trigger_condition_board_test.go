package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
)

func TestTriggerConditionBoardFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	card, ok := reg.Lookup("Dragonmaster Outcast")
	if !ok || len(card.Faces) == 0 || len(card.Faces[0].Triggers) == 0 {
		t.Fatal("precondition: Dragonmaster Outcast trigger is absent")
	}
	tr := &card.Faces[0].Triggers[0]
	fixtures := triggerConditionFixtures(reg, card.Faces[0], tr)
	if len(fixtures) == 0 {
		t.Fatal("precondition: IsPresent count did not produce a fixture")
	}
	if got := len(fixtures[0].battlefield); got < 6 {
		t.Fatalf("Land.YouCtrl GE6 fixture has %d permanents, want at least 6", got)
	}

	paradox, ok := reg.Lookup("Paradox Shaper")
	if !ok {
		t.Fatal("precondition: Paradox Shaper absent from corpus")
	}
	found := false
	for _, req := range levelb.Requirements(paradox) {
		if req.Family != "trigger" {
			continue
		}
		_, skip := GenerateB(reg, "Paradox Shaper", req)
		if skip != nil && strings.Contains(skip.Reason, "engine predicate unread") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("precondition: Paradox Shaper has no engine-predicate skip")
	}
}
