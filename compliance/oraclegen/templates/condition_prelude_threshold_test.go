package templates

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// thresholdTrigger returns the card's trigger slot carrying a Threshold$
// param, with its requirement.
func thresholdTrigger(t *testing.T, reg *cards.Registry, name string) (levelb.Requirement, *cards.Trigger, bool) {
	t.Helper()
	card, ok := reg.Lookup(name)
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("precondition: %s missing from corpus", name)
	}
	for _, req := range levelb.Requirements(card) {
		if req.Family != "trigger" {
			continue
		}
		slot, err := strconv.Atoi(req.Slot)
		if err != nil || slot < 0 || slot >= len(card.Faces[0].Triggers) {
			continue
		}
		if card.Faces[0].Triggers[slot].ParamStr(cards.PKThreshold) != "" {
			return req, &card.Faces[0].Triggers[slot], true
		}
	}
	return levelb.Requirement{}, nil, false
}

// TestThresholdConditionPreludeIsSameName pins the Tersa Lightshatter
// trigger#0.1 flake fix: a threshold (no delirium) gate's graveyard prelude is
// nine same-name cards, so a trigger that removes one AT RANDOM leaves the
// same snapshot whichever card each engine's random pick takes. Delirium
// still gets bigGraveyard, whose four card types it needs.
func TestThresholdConditionPreludeIsSameName(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	req, trigger, ok := thresholdTrigger(t, reg, "Tersa Lightshatter")
	if !ok {
		t.Fatal("precondition: Tersa Lightshatter has no threshold trigger requirement")
	}
	card, _ := reg.Lookup("Tersa Lightshatter")
	preludes := conditionPreludes(reg, trigger.Params, card.Faces[0].SVars)
	var found bool
	for _, c := range preludes {
		if len(c.graveyard) >= 7 && allSameName(c.graveyard) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no threshold prelude offers a same-name graveyard: %+v", preludes)
	}
	// The generated scenario itself carries it: the fire probe accepts the
	// same-name graveyard because threshold counts cards, not card types.
	item, skip := GenerateB(reg, "Tersa Lightshatter", req)
	if skip != nil {
		t.Fatalf("Tersa Lightshatter %s: %s", req.Key, skip.Reason)
	}
	gy := item.Scenario.Setup["p0"].Graveyard
	if len(gy) < 7 || !allSameName(gy) {
		t.Fatalf("generated setup graveyard = %v, want >=7 same-name cards", gy)
	}
}

// TestDeliriumConditionPreludeKeepsCardTypes pins the sibling: a delirium
// gate still gets bigGraveyard (its four card types), never the same-name
// graveyard.
func TestDeliriumConditionPreludeKeepsCardTypes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	name := "Angel of Deliverance"
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s missing from corpus", name)
	}
	var found bool
	for _, req := range levelb.Requirements(card) {
		if req.Family != "trigger" {
			continue
		}
		slot, err := strconv.Atoi(req.Slot)
		if err != nil || slot < 0 || slot >= len(card.Faces[0].Triggers) {
			continue
		}
		trigger := &card.Faces[0].Triggers[slot]
		if trigger.ParamStr(cards.PKDelirium) == "" {
			continue
		}
		for _, c := range conditionPreludes(reg, trigger.Params, card.Faces[0].SVars) {
			if len(c.graveyard) >= 7 && !allSameName(c.graveyard) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("%s: no delirium prelude offers a card-type graveyard", name)
	}
}

// allSameName reports every entry equal (and nonempty).
func allSameName(cards []string) bool {
	if len(cards) == 0 || cards[0] == "" {
		return false
	}
	for _, c := range cards {
		if c != cards[0] {
			return false
		}
	}
	return true
}
