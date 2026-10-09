// Hellbent condition preludes (Level B). A trigger gated on Hellbent$ True
// (CR 702.90: its controller has no cards in hand) fires only while that hand
// is empty. Setup can leave a card there -- the source's own ETB discard
// filler, or a card an earlier prelude drew -- so the candidate casts One with
// Nothing, an instant that discards its controller's whole hand, before the
// checkpoint. The fire probe discards the candidate when the hand is already
// empty or the probe is not in the corpus.
package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// hellbentPrelude is the empty-hand setup a Hellbent trigger's gate needs.
// One with Nothing (B instant, "Discard your hand") empties any hand whatever
// earlier setup put in it; casting it leaves the card on the stack, so the
// resolve that follows discards the hand and not the probe itself.
func hellbentPrelude(reg *cards.Registry) (conditionPrelude, bool) {
	cast, ok := castProbe(reg, "One with Nothing")
	if !ok {
		return conditionPrelude{}, false
	}
	return conditionPrelude{
		hand:  []string{"One with Nothing"},
		steps: []oraclegen.Step{cast, {Op: "resolve"}},
	}, true
}
