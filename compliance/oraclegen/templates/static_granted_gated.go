package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules/pay"
)

// The measured reasons a gated self grant is not served; each names the
// failure the helper actually saw.
const (
	staticGatedSelfETBCounterReason = "counter-gated card with its own ETB is cast and holds no counters"
	staticGatedNotOfferedReason     = "gated self grant is not offered in the gate-on fixture"
	staticGatedControlOfferedReason = "gated self grant is also offered in the gate-off control"
)

// staticGatedGrantedAbilityItem observes a gated AddAbility grant on the
// permanent that owns both the gate and the granted ability. Its control keeps
// that permanent but removes only the gate condition. why is "" when the grant
// is not an activated ability this observation serves (the caller falls
// through to its named grant gap) and otherwise names the measured failure.
//
// A counter-gated grant is an offered "activate" (a mana ability is labelled by
// pay.ManaAbilityLabel). A max-speed grant is the engine's "granted" option
// kind, labelled "<face>: <SpellDescription>" for mana and non-mana alike; an
// "activate" assertion does not match that kind, and the generic mana offer
// leaks into the gate-off control. Both bases are the PLACED card: a setup
// permanent is not summoning sick, a cast one is.
func staticGatedGrantedAbilityItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (it oraclegen.Item, why string, ok bool) {
	var sa *cards.SA
	if ability := strings.TrimSpace(st.ParamStr(cards.PKAddAbility)); ability != "" {
		sa = cards.ResolveSVar(f.SVars, ability)
	}
	if sa == nil || sa.Kind != "AB" {
		return oraclegen.Item{}, "", false
	}
	speedGated := staticGatedOnMaxSpeed(&st)
	kind, label := "activate", ""
	desc := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
	switch {
	case speedGated:
		kind = "granted"
		label = name + ": " + desc
	case sa.API == "Mana" && grantedLoyaltyCost(sa) < 0:
		// Keep the cost prefix: a bare production such as "Add G" also
		// matches the card's printed mana ability under oracleLabelMatches' substring semantics.
		label = pay.ManaAbilityLabel(sa, "")
	default:
		label = name + ": " + desc
	}
	if desc == "" && label == name+": " {
		return oraclegen.Item{}, "", false
	}
	pool, gap := activationCost(sa.ParamStr(cards.PKCost))
	if gap != "" {
		return oraclegen.Item{}, "", false
	}
	var sc oraclegen.Scenario
	var steps []oraclegen.Step
	switch {
	case speedGated && !staticSelfETB(f):
		base := staticBackFaceScenario(f, name, req, []string{staticProbe}, nil)
		withMaxSpeed(base.Scenario.Setup)
		sc, steps = base.Scenario, base.Steps
	case !speedGated && staticSelfETB(f):
		return oraclegen.Item{}, staticGatedSelfETBCounterReason, false
	default:
		// A counter-gated or speed-gated card with its own ETB stays on the
		// cast path (staticBase); a placed one would fire the ETB in gorge only.
		base, bwhy := staticBase(reg, c, f, name, req, staticProbePlan{}, nil)
		if bwhy != "" {
			return oraclegen.Item{}, "", false
		}
		sc, steps = base.Scenario, base.Steps
	}
	p0 := sc.Setup["p0"]
	addActivationCostFixtures(&p0, sa.ParamStr(cards.PKCost))
	sc.Setup["p0"] = p0
	sc.Steps = append(append([]oraclegen.Step(nil), steps...), oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"})
	if pool != "" {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "mana", Seat: 0, Mana: pool})
	}
	sc.Steps = append(sc.Steps, gatedGrantExpectation(name, kind, label, true))
	res, rok := runStatic(reg, sc)
	if !rok || len(res.Fails) != 0 {
		return oraclegen.Item{}, staticGatedNotOfferedReason, false
	}
	control := sc
	control.Setup = cloneSeats(sc.Setup)
	cp0 := control.Setup["p0"]
	if ckind, _, gated := staticCounterGate(&st); gated {
		if cp0.Counters[name] != nil {
			delete(cp0.Counters[name], ckind)
		}
	} else if speedGated {
		cp0.Speed = 0
	}
	control.Setup["p0"] = cp0
	control.Steps = append([]oraclegen.Step(nil), sc.Steps[:len(sc.Steps)-1]...)
	control.Steps = append(control.Steps, gatedGrantExpectation(name, kind, label, false))
	cres, cok := runStatic(reg, control)
	if !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, staticGatedControlOfferedReason, false
	}
	item := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	item.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return item, "", true
}

func gatedGrantExpectation(name, kind, label string, want bool) oraclegen.Step {
	return oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{
		Offered: &oraclegen.Offered{Seat: 0, Kind: kind, Card: "p0:" + name, Label: label}, Want: boolPtr(want),
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
