package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticGatedGrantedAbilityItem observes a gated AddAbility grant on the
// permanent that owns both the gate and the granted ability. Its control keeps
// that permanent but removes only the gate condition.
func staticGatedGrantedAbilityItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	var sa *cards.SA
	if ability := strings.TrimSpace(st.ParamStr(cards.PKAddAbility)); ability != "" {
		sa = cards.ResolveSVar(f.SVars, ability)
	}
	if sa == nil || sa.Kind != "AB" {
		return oraclegen.Item{}, false
	}
	var label string
	if sa.API == "Mana" && grantedLoyaltyCost(sa) < 0 {
		label = grantedManaLabel(sa)
	} else {
		desc := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
		if desc == "" {
			return oraclegen.Item{}, false
		}
		label = name + ": " + desc
	}
	pool, gap := activationCost(sa.ParamStr(cards.PKCost))
	if gap != "" {
		return oraclegen.Item{}, false
	}
	base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil)
	if why != "" {
		return oraclegen.Item{}, false
	}
	sc := base.Scenario
	p0 := sc.Setup["p0"]
	addActivationCostFixtures(&p0, sa.ParamStr(cards.PKCost))
	sc.Setup["p0"] = p0
	sc.Steps = append(append([]oraclegen.Step(nil), base.Steps...), oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"})
	if pool != "" {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "mana", Seat: 0, Mana: pool})
	}
	sc.Steps = append(sc.Steps, gatedGrantExpectation(name, label, true))
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	control := sc
	control.Setup = cloneSeats(sc.Setup)
	cp0 := control.Setup["p0"]
	if kind, _, gated := staticCounterGate(&st); gated {
		if cp0.Counters[name] != nil {
			delete(cp0.Counters[name], kind)
		}
	} else if staticGatedOnMaxSpeed(&st) {
		cp0.Speed = 0
	}
	control.Setup["p0"] = cp0
	control.Steps = append([]oraclegen.Step(nil), sc.Steps[:len(sc.Steps)-1]...)
	control.Steps = append(control.Steps, gatedGrantExpectation(name, label, false))
	cres, cok := runStatic(reg, control)
	if !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	it.Compare = []string{oraclediff.CompareKeywords}
	return it, true
}

func gatedGrantExpectation(name, label string, want bool) oraclegen.Step {
	return oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{
		Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + name, Label: label}, Want: boolPtr(want),
	}}}
}

func cloneSeats(in map[string]oraclegen.Seat) map[string]oraclegen.Seat {
	out := make(map[string]oraclegen.Seat, len(in))
	for k, v := range in {
		v.Counters = cloneCounterMap(v.Counters)
		out[k] = v
	}
	return out
}

func cloneCounterMap(in map[string]map[string]int32) map[string]map[string]int32 {
	out := make(map[string]map[string]int32, len(in))
	for card, counters := range in {
		out[card] = make(map[string]int32, len(counters))
		for kind, n := range counters {
			out[card][kind] = n
		}
	}
	return out
}
