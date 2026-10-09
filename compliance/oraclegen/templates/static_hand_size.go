// Observation for a Continuous SetMaxHandSize$ static (ticket
// cli-20261009T031407Z-dd0d6fbb): a player-rule number no snapshot field
// carries, so the row was skipped as "hand size is not observable in the
// permanent snapshot". The item asserts gorge's own effective CR 514.1
// maximum through a runner expectation, exactly as the MayLookAt$
// observation asserts MayLookAtLibraryTop, beside a control without the card.
package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
)

// staticDefaultMaxHandSize is CR 514.1's default maximum, the control value
// the SetMaxHandSize$ observation must differ from. It mirrors rules'
// unexported maxHandSize (rules/combat.go).
const staticDefaultMaxHandSize = 7

// staticMaxHandSizeItem serves a Continuous SetMaxHandSize$ static (Reliquary
// Tower-style "you have no maximum hand size", The Ten Rings' "ten", Doctor
// Octopus's "eight"): the card is on p0's battlefield and the item asserts
// p0's effective CR 514.1 maximum at p0's begin-combat checkpoint (the
// runner's max_hand_size expectation, frozen into `fails` like can_block). A
// control without the card proves the default, and a value equal to the
// default is refused so the assertion cannot pass vacuously. ok is false when
// the value is not one the engine prices (a dynamic SVar name like Winter's
// Y), so the row keeps its named gap.
func staticMaxHandSizeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	want, ok := effects.HandSizeValueOK(st.ParamStr(cards.PKSetMaxHandSize))
	if !ok || want == staticDefaultMaxHandSize {
		return oraclegen.Item{}, false
	}
	step := func(want int) []oraclegen.Step {
		return []oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "begin-combat",
			Expect: []oraclegen.Expect{{MaxHandSize: map[string]int{"p0": want}}}}}
	}
	control := staticScenario(f, name, nil, nil, step(staticDefaultMaxHandSize))
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := withBackFace(staticScenario(f, name, []string{name}, nil, step(want)), name, req)
	it, skip := finishLegalityItem(reg, f, name, "MaxHandSize", req, sc, []string{"514.1"})
	if skip != nil {
		return oraclegen.Item{}, false
	}
	return it, true
}
