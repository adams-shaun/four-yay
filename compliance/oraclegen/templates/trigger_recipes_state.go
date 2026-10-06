package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// stateSelfCountersSub is levelb's spelling of the self-counter state
// trigger sub-family (Mazemind Tome's "When there are four or more page
// counters on it").
const stateSelfCountersSub = levelb.StateSelfCountersSub

// stateTriggerRecipe builds the causes of a state trigger on the source's own
// counter count (CR 603.8): the source starts one counter short of the gate
// and the card's own battlefield activation adds the last one, either as a
// cost (Mazemind Tome's "{T}, Put a page counter on it") or as its effect
// (a self PutCounter of one counter). The state becomes true while the
// scenario plays, which both engines observe the same way, rather than at
// setup, where the trigger would have to be checked before the first
// checkpoint. ok is false for every other sub-family.
func stateTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	if sub != stateSelfCountersSub {
		return nil, "", false
	}
	kind, n, gated := levelb.SelfCounterGate(t.ParamStr(cards.PKIsPresent))
	if !gated {
		return nil, "state trigger: not a self counter gate", true
	}
	prefixes, xwhy := oraclegen.XMageAbility(f)
	for idx, sa := range f.Abilities {
		if !sa.IsActivated() || !selfCounterActivation(sa, kind) {
			continue
		}
		if zone := sa.ParamStr(cards.PKActivationZone); zone != "" && !strings.EqualFold(zone, "Battlefield") {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield")
		if gap != "" {
			continue
		}
		if xwhy != "" {
			continue
		}
		prefix, okp := prefixes[idx]
		if !okp {
			continue
		}
		i := idx
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, cost)
		c := triggerCause{
			battlefield:  append([]string(nil), setup.Battlefield...),
			hand:         append([]string(nil), setup.Hand...),
			graveyard:    append([]string(nil), setup.Graveyard...),
			steps:        []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: mana, AbilityIndex: &i, Answers: activationXAnswers(cost)}},
			xability:     []string{prefix},
			activateCost: cost,
		}
		if n > 1 {
			c.counters = map[string]map[string]int{"__SOURCE__": {kind: n - 1}}
		}
		causes = append(causes, c)
	}
	if len(causes) == 0 {
		return nil, "state trigger: no own activation adds a " + kind + " counter", true
	}
	return causes, "", true
}

// selfCounterActivation reports whether an activated ability puts exactly one
// counter of kind on its own source: as a cost token AddCounter<1/kind>, or
// as an untargeted self PutCounter effect of one counter.
func selfCounterActivation(sa *cards.SA, kind string) bool {
	for _, tok := range costTokens(sa.ParamStr(cards.PKCost)) {
		payload, ok := strings.CutPrefix(tok, "AddCounter<")
		if !ok {
			continue
		}
		fields := strings.Split(strings.TrimSuffix(payload, ">"), "/")
		if len(fields) >= 2 && fields[0] == "1" && strings.EqualFold(fields[1], kind) {
			return true
		}
	}
	if !strings.EqualFold(sa.API, "PutCounter") || sa.Params["ValidTgts"] != "" {
		return false
	}
	if d := strings.TrimSpace(sa.Params["Defined"]); d != "" && !strings.EqualFold(d, "Self") {
		return false
	}
	if num := strings.TrimSpace(sa.Params["CounterNum"]); num != "" && num != "1" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(sa.Params["CounterType"]), kind)
}
