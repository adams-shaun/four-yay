package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// costStaticGap is the skip reason of a cost static no probe serves. A named
// suffix says which shape needs a fixture the generator cannot establish.
func costStaticGap(st cards.Static, gap string) string {
	reason := "cost static probe not supported"
	switch {
	case gap != "":
		reason += ": " + gap
	case strings.EqualFold(st.Params["Type"], "Ability") || st.Params["ValidSpell"] != "":
		reason += ": activated-ability probe unsupported"
	case strings.EqualFold(st.Params["ValidCard"], "Card.Self"):
		reason += ": unsupported self-cost shape"
	}
	return reason
}

// costProbeItem builds the level-B item casting (or activating) one probe at
// the reduced price with the static's permanent on the battlefield.
func costProbeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, idx int, p costProbe, fx oraclegen.Fixture) oraclegen.Item {
	p0 := *fx.P0()
	p1 := *fx.P1()
	p0.Battlefield = appendUnique(p0.Battlefield, p.battlefield...)
	p0.Graveyard = appendUnique(p0.Graveyard, p.graveyard...)
	p0.Exile = appendUnique(p0.Exile, p.exile...)
	p0.Hand = append(p0.Hand, p.hand...)
	if p.activate == nil {
		if p.opponent {
			p1.Hand = appendUnique(p1.Hand, p.spell)
		} else {
			p0.Hand = appendUnique(p0.Hand, p.spell)
		}
	}
	if p.first != nil && p.first.Card == "p0:Shock" {
		p0.Hand = appendUnique(p0.Hand, "Shock")
	}
	if p.seat != nil {
		p.seat(&p0)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
	}
	sc.Steps = append(sc.Steps, fx.CombatSteps()...)
	sc.Steps = append(sc.Steps, fx.Prelude()...)
	sc.Steps = append(sc.Steps, p.pre...)
	if p.first != nil {
		sc.Steps = append(sc.Steps, *p.first, oraclegen.Step{Op: "resolve"})
	}
	targets := fx.Targets()
	if p.targeted {
		targets = []string{"p1"}
		if p.opponent {
			targets = []string{"p0:" + name}
		}
	}
	if strings.Contains(f.Statics[idx].Params["ValidTarget"], "tapped") {
		for _, target := range targets {
			tapFixtureTarget(target, &p0, &p1)
		}
		sc.Setup["p0"], sc.Setup["p1"] = p0, p1
	}
	castSeat, castRef := 0, "p0:"+p.spell
	if p.opponent {
		castSeat, castRef = 1, "p1:"+p.spell
	}
	step := oraclegen.Step{Op: "cast", Seat: castSeat, Card: castRef, Mana: p.mana, Targets: targets}
	if a := p.activate; a != nil {
		index := a.index
		step = oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + p.spell, Mana: p.mana, Targets: a.targets, AbilityIndex: &index}
	}
	if p.opponent {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "pass", Seat: 0})
	}
	sc.Steps = append(sc.Steps, step)
	oraclegen.Baseline(sc.Setup, f)
	it := oraclegen.NewLevelBItem(name, req.Key, CostStatic.Version, []string{"601.2"}, sc)
	// Settle plays a temporary copy with resolve steps, so the target
	// rewrite is validated against a scenario whose stack empties. The
	// settled copy is what is kept: every probe step carries gorge's exact
	// targets (target decisions travel through the step and are not scripted
	// a second time). When gorge cannot pay the reduced price Settle fails
	// and the unnormalized item is returned as is, so the failed step
	// surfaces as a divergence, not a Skip.
	if n, res, ok := oraclegen.Settle(reg, sc); ok {
		settled := sc
		settled.Steps = append([]oraclegen.Step(nil), sc.Steps...)
		for i := 0; i < n; i++ {
			settled.Steps = append(settled.Steps, oraclegen.Step{Op: "resolve"})
		}
		settled, castSteps := oraclegen.ChooseTargets(settled, res.Decisions)
		if res2, ok2 := oraclegen.PlaysThrough(reg, settled); ok2 {
			settled.Name, settled.CR, settled.Why = it.Name, it.CR, it.Why
			it.Scenario = settled
			it.XAnswers = oraclegen.XAnswersForScenario(res2, settled, nil, castSteps)
		}
	}
	if p.activate != nil {
		it.CR = []string{"602.2"}
		if len(it.XAnswers) == 0 {
			it.XAnswers = make([][]oraclegen.XAnswer, len(it.Steps))
		}
		it.XAbility = make([]string, len(it.Steps))
		for i, s := range it.Steps {
			if s.Op == "activate" {
				it.XAbility[i] = p.activate.prefix
			}
		}
	}
	return it
}
