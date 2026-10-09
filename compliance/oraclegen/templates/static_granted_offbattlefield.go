package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// offBattlefieldGatedGrantItem observes a speed-gated AddAbility$ self grant
// whose static acts OFF the battlefield (the Surveyor cycle's "Max speed —
// {3}, Exile this card from your graveyard: Draw a card."). The engine offers
// the granted activation on the card in its own zone (rules'
// offBattlefieldGrantedWalk, cli-3b80d13b1), so the observation places the
// card in that zone at max speed and asserts the "granted" offer; the gate-off
// control keeps the card in the zone and drops only the speed (a self grant
// has no source to remove).
//
// ok is false with why "" when the shape is not this arm's (the caller keeps
// its battlefield attempt and the named grant gaps), and with a measured
// reason when the shape is served but the scenario failed. The shape guard is
// exact: one non-battlefield EffectZone$ this arm knows, a self grant, the
// MaxSpeed gate (the only gate with a one-knob off-switch), an AB SVar with a
// SpellDescription to label the offer, and a cost payable from that zone.
func offBattlefieldGatedGrantItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, sa *cards.SA) (it oraclegen.Item, why string, ok bool) {
	zone := offBattlefieldGrantZone(&st)
	if zone == "" || sa == nil || sa.Kind != "AB" {
		return oraclegen.Item{}, "", false
	}
	desc := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
	if desc == "" {
		return oraclegen.Item{}, "", false
	}
	pool, gap := activationCostIn(sa.ParamStr(cards.PKCost), zone)
	if gap != "" {
		return oraclegen.Item{}, "", false
	}
	label := name + ": " + desc
	sc := staticScenario(f, name, nil, nil, nil)
	p0 := sc.Setup["p0"]
	p0.Graveyard = appendFixtureUnique(p0.Graveyard, name)
	sc.Setup["p0"] = p0
	withMaxSpeed(sc.Setup)
	sc.Steps = append(append([]oraclegen.Step(nil), sc.Steps...),
		oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"})
	if pool != "" {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "mana", Seat: 0, Mana: pool})
	}
	sc.Steps = append(sc.Steps, gatedGrantExpectation(name, "granted", label, true))
	res, rok := runStatic(reg, sc)
	if !rok || len(res.Fails) != 0 {
		return oraclegen.Item{}, staticGatedNotOfferedReason, false
	}
	control := sc
	control.Setup = cloneSeats(sc.Setup)
	cp0 := control.Setup["p0"]
	cp0.Speed = 0
	control.Setup["p0"] = cp0
	control.Steps = append([]oraclegen.Step(nil), sc.Steps[:len(sc.Steps)-1]...)
	control.Steps = append(control.Steps, gatedGrantExpectation(name, "granted", label, false))
	if cres, cok := runStatic(reg, control); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, staticGatedControlOfferedReason, false
	}
	item := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	item.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return item, "", true
}

// offBattlefieldGrantZone is the single non-battlefield zone a static's
// EffectZone$ names, lower-cased for activationCostIn, or "" when the static
// does not name exactly one zone this arm serves.
func offBattlefieldGrantZone(st *cards.Static) string {
	z := strings.TrimSpace(st.ParamStr(cards.PKEffectZone))
	if z == "" || strings.ContainsAny(z, ", ") {
		return ""
	}
	for _, known := range []string{"Graveyard", "Exile", "Hand", "Library"} {
		if strings.EqualFold(z, known) {
			return strings.ToLower(known)
		}
	}
	return ""
}
