package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticCounterPumpCard is the instant the counters-added fixture casts. Its
// whole effect is "put a +1/+1 counter on target creature" for {G}, so the
// only things it changes on the card are that counter and the +1/+1 it gives.
const staticCounterPumpCard = "Battlegrowth"

// staticCountersAddedFixtures is the fixture for a static gated on counters
// the controller put on the card itself this turn (Beast, Erudite Aerialist's
// "CheckSVar$ X, X = Count$CountersAddedThisTurn P1P1 You Card.Self"). Setup
// counters are placed before turn 1 and do not count, so after the card is
// on the battlefield p0 casts a +1/+1-counter instant on it. The counter's
// own +1/+1 is the fixture's baseline shift of the card's P/T, so only the
// static's grant reads as a change.
func staticCountersAddedFixtures(reg *cards.Registry, f *cards.Face, st cards.Static) []staticFixture {
	gated := false
	for _, body := range staticSVarBodies(f, st) {
		if strings.EqualFold(strings.TrimSpace(body), "Count$CountersAddedThisTurn P1P1 You Card.Self") {
			gated = true
		}
	}
	if !gated {
		return nil
	}
	cast, ok := castProbe(reg, staticCounterPumpCard, "p0:"+f.Name)
	if !ok {
		return nil
	}
	return []staticFixture{{
		conditionPrelude: conditionPrelude{hand: []string{staticCounterPumpCard}},
		afterSteps:       []oraclegen.Step{cast, {Op: "resolve", Seat: 0}},
		selfPT:           [2]int32{1, 1},
	}}
}
