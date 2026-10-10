package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticBeholdFixtures is the fixture for a static on a card whose own cast
// carries an additional "behold and exile" cost (Champion of the Clachan),
// observed on a probe that is itself a legal behold choice. The hand holds a
// spare card of the beholden type and the cast's behold pick is scripted to
// it: left to the default the cast exiles the first option, the battlefield
// probe, before the checkpoint can read the static on it. reanswers carries
// the scripted pick to XMage's cost dialog.
func staticBeholdFixtures(f *cards.Face) []staticFixture {
	typ := requiredBeholdType(f)
	if typ == "" || beholdFixture[typ] == "" {
		return nil
	}
	spare := beholdFixture[typ]
	return []staticFixture{{
		conditionPrelude: conditionPrelude{hand: []string{spare}},
		beholdPick:       spare,
		reanswers:        true,
	}}
}

// answerBeholdCost scripts the behold pick of the card's own cast step.
func answerBeholdCost(base *oraclegen.Item, name, want string) bool {
	return scriptSingleChoose(base, func(st oraclegen.Step) bool {
		return st.Op == "cast" && st.Card == "p0:"+name
	}, want)
}
