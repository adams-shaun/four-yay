package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticStealCard is the Aura the steal fixture casts on p1's probe.
// Mind Control's only effect is "You control enchanted creature" (a
// GainControl$ static), so it changes none of the compared fields (P/T,
// keywords, types, colours) of the creature it takes: only the static under
// test can move the stolen probe.
const staticStealCard = "Mind Control"

// staticStealFixtures is the fixture for a static whose affected permanent is
// a creature its controller does not own (Laughing Jasper Flint's
// "Creature.YouCtrl+YouDontOwn"). Setup places no card under another
// player's control, so after the card is on the battlefield p0 casts the
// steal Aura on p1's probe (both seats carry one): the stolen Bear is
// YouCtrl+YouDontOwn while p0's own Bear is not, and the probe spec is
// keyed by name, so only the stolen one can read as changed.
func staticStealFixtures(reg *cards.Registry, st cards.Static) []staticFixture {
	if !hasWord(affectedWords(st.ParamStr(cards.PKAffected)), "YouDontOwn") {
		return nil
	}
	cast, ok := castProbe(reg, staticStealCard, "p1:"+staticProbe)
	if !ok {
		return nil
	}
	return []staticFixture{{
		conditionPrelude: conditionPrelude{hand: []string{staticStealCard}},
		afterSteps:       []oraclegen.Step{cast, {Op: "resolve", Seat: 0}},
	}}
}
