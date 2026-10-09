// Observation for a Continuous SetMaxHandSize$ static (ticket
// cli-20261009T031407Z-dd0d6fbb, extended by
// agent-20261009T055739Z-b43f3480): a player-rule number no snapshot field
// carries, so the row was skipped as "hand size is not observable in the
// permanent snapshot". The item asserts gorge's own effective CR 514.1
// maximum through a runner expectation, exactly as the MayLookAt$
// observation asserts MayLookAtLibraryTop, beside a control without the card.
//
// A literal or Unlimited value is read straight off the static. A value that
// names an SVar -- Winter, Misanthropic Guide's Delirium "each opponent's
// maximum hand size is equal to seven minus the number of those card types"
// -- gets the fixture that makes its count nonzero and a computed
// expectation, so the assertion is still the engine's own priced maximum.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
)

// staticDefaultMaxHandSize is CR 514.1's default maximum, the control value
// the SetMaxHandSize$ observation must differ from. It mirrors rules'
// unexported maxHandSize (rules/combat.go).
const staticDefaultMaxHandSize = 7

// staticMaxHandSizeItem serves a Continuous SetMaxHandSize$ static: a
// literal/Unlimited value directly, an SVar value through the fixture its
// shape asks for. ok is false when no value the engine prices can be
// asserted, so the row keeps its named gap.
func staticMaxHandSizeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if it, ok := staticMaxHandSizeLiteralItem(reg, f, name, req, st); ok {
		return it, true
	}
	return staticMaxHandSizeSVarItem(reg, f, name, req, st)
}

// staticMaxHandSizeLiteralItem serves a literal/Unlimited SetMaxHandSize$
// static (Reliquary Tower-style "you have no maximum hand size", The Ten
// Rings' "ten", Doctor Octopus's "eight"): the card is on p0's battlefield
// and the item asserts p0's effective CR 514.1 maximum at p0's begin-combat
// checkpoint (the runner's max_hand_size expectation, frozen into `fails`
// like can_block). A control without the card proves the default, and a value
// equal to the default is refused so the assertion cannot pass vacuously.
// ok is false when the value is not one the engine prices (a dynamic SVar
// name like Winter's Y), so the row keeps its named gap.
func staticMaxHandSizeLiteralItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
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

// staticHandSizeGraveFixture is the graveyard a "seven minus the number of
// card types" maximum is priced against: four cards of four distinct card
// types (Land, Instant, Sorcery, Creature), which both makes the count
// nonzero and satisfies Delirium's 4+ types.
var staticHandSizeGraveFixture = []string{"Wastes", "Shock", "Divination", "Llanowar Elves"}

// staticMaxHandSizeSVarItem serves an SVar-valued SetMaxHandSize$ static whose
// body is "Number$<n>/Minus.<svar>" over a graveyard card-type count -- the
// Delirium shape Winter, Misanthropic Guide prints. The fixture puts
// len(staticHandSizeGraveFixture) distinct card types in the CONTROLLER's
// graveyard, so the count is that length and the expected maximum is
// n - len(fixture). The expectation lands on the seat Affected$ names (an
// "Opponent" maximum is not p0's), and a value that would equal the default
// is refused so the assertion cannot pass vacuously. ok is false for every
// other dynamic shape, which keeps its named gap.
func staticMaxHandSizeSVarItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	raw := strings.TrimSpace(st.ParamStr(cards.PKSetMaxHandSize))
	if _, literal := effects.HandSizeValueOK(raw); literal || raw == "" {
		return oraclegen.Item{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKCondition)), "Delirium") {
		return oraclegen.Item{}, false
	}
	n, ok := staticHandSizeNumber(raw, f.SVars)
	if !ok {
		return oraclegen.Item{}, false
	}
	seat, ok := staticHandSizeAffectedSeat(st.ParamStr(cards.PKAffected))
	if !ok {
		return oraclegen.Item{}, false
	}
	want := n - len(staticHandSizeGraveFixture)
	if want < 0 || want == staticDefaultMaxHandSize {
		return oraclegen.Item{}, false
	}
	steps := func(want int) []oraclegen.Step {
		return []oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "begin-combat",
			Expect: []oraclegen.Expect{{MaxHandSize: map[string]int{seat: want}}}}}
	}
	control := staticHandSizeGraveScenario(f, name, nil, steps(staticDefaultMaxHandSize))
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := withBackFace(staticHandSizeGraveScenario(f, name, []string{name}, steps(want)), name, req)
	it, skip := finishLegalityItem(reg, f, name, "MaxHandSize", req, sc, []string{"514.1"})
	if skip != nil {
		return oraclegen.Item{}, false
	}
	return it, true
}

// staticHandSizeNumber reads the literal in an SVar's "Number$<n>/Minus.<svar>"
// body, requiring the operand it subtracts to be a graveyard card-type count
// ("Count$ValidGraveyard ...$CardTypes") -- the shape the fixture prices.
func staticHandSizeNumber(raw string, svars map[string]string) (int, bool) {
	body := strings.TrimSpace(svars[raw])
	lit, op, ok := strings.Cut(strings.TrimPrefix(body, "Number$"), "/")
	if !ok || !strings.HasPrefix(body, "Number$") || !strings.HasPrefix(op, "Minus.") {
		return 0, false
	}
	operand := strings.TrimSpace(svars[strings.TrimPrefix(op, "Minus.")])
	if !strings.HasPrefix(operand, "Count$ValidGraveyard ") || !strings.HasSuffix(operand, "$CardTypes") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(lit))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// staticHandSizeAffectedSeat is the seat Affected$ names for the maximum: the
// static's controller ("You") or that player's opponent ("Opponent").
func staticHandSizeAffectedSeat(affected string) (string, bool) {
	for _, w := range affectedWords(affected) {
		switch w {
		case "You":
			return "p0", true
		case "Opponent", "Opponents":
			return "p1", true
		}
	}
	return "", false
}

// staticHandSizeGraveScenario is staticScenario with the card-type fixture in
// p0's graveyard (the static's controller, whose card types the count reads),
// on both the card-on-battlefield and the control builds.
func staticHandSizeGraveScenario(f *cards.Face, name string, battlefield []string, steps []oraclegen.Step) oraclegen.Scenario {
	sc := staticScenario(f, name, battlefield, nil, steps)
	p0 := sc.Setup["p0"]
	p0.Graveyard = append(append([]string(nil), p0.Graveyard...), staticHandSizeGraveFixture...)
	sc.Setup["p0"] = p0
	return sc
}
