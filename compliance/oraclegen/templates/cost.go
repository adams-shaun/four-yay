package templates

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// CostStatic probes a cost static by casting either a qualifying spell or its
// own source at the reduced price. The exact price is intentional: a gorge
// refusal remains a generated item and becomes a host replay divergence, not a
// generator Skip that could hide the behavior under test.
var CostStatic = Template{ID: "static.cost", Version: 1}

type costProbe struct {
	spell, mana string
	battlefield []string
	hand        []string
	first       *oraclegen.Step
	// targeted offers the probe cast a surplus player target; gorge's own
	// target decision rewrites it to the exact pick before the item is kept.
	targeted bool
}

func costStatic(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Statics) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static index " + req.Slot}
	}
	// Each profile chooses a simple card whose printed cost makes the
	// reduction visible and supplies only the prerequisites the static needs.
	var p costProbe
	switch name {
	case "Geist of Saint Thalia":
		// Lightning Strike is {1}{R}: the generic {1} is what the reduction
		// removes, so {R} alone casts it only while Geist's static applies.
		p = costProbe{spell: "Lightning Strike", mana: "R", battlefield: []string{name}, targeted: true}
	case "Tam, the Possibility":
		p = costProbe{spell: "Jace Beleren", mana: "UU", battlefield: []string{name}}
	case "Ghalta the Immovable":
		p = costProbe{spell: name, mana: "CCCCW", hand: []string{name}, battlefield: []string{"Serra Angel"}}
	case "Ghalta the Unstoppable":
		p = costProbe{spell: name, mana: "CCCCG", hand: []string{name}, battlefield: []string{"Serra Angel"}}
	case "Traxos, Academy Guardian":
		first := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p1"}}
		p = costProbe{spell: name, mana: "CU", hand: []string{name}, first: &first}
	case "Wrath of the Bloodmane":
		p = costProbe{spell: name, mana: "CR", hand: []string{name}, battlefield: []string{"Tam, the Possibility"}}
	default:
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static probe not supported"}
	}
	slots := oraclegen.SlotSpecs(f)
	fixtures := oraclegen.Fixtures(reg, slots)
	if len(fixtures) == 0 {
		fixtures = []oraclegen.Fixture{{}}
	}
	for _, fx := range fixtures {
		p0 := *fx.P0()
		p1 := *fx.P1()
		p0.Battlefield = appendUnique(p0.Battlefield, p.battlefield...)
		p0.Hand = append(p0.Hand, p.hand...)
		p0.Hand = appendUnique(p0.Hand, p.spell)
		if p.first != nil && p.first.Card == "p0:Shock" {
			p0.Hand = appendUnique(p0.Hand, "Shock")
		}
		sc := oraclegen.Scenario{
			Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
			SetupAnswers: oraclegen.OpeningHandAnswers(f),
		}
		if p.first != nil {
			sc.Steps = append(sc.Steps, *p.first, oraclegen.Step{Op: "resolve"})
		}
		targets := fx.Targets()
		if p.targeted {
			targets = []string{"p1"}
		}
		cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + p.spell, Mana: p.mana, Targets: targets}
		sc.Steps = append(sc.Steps, cast)
		oraclegen.Baseline(sc.Setup, f)
		it := oraclegen.NewLevelBItem(name, req.Key, CostStatic.Version, []string{"601.2"}, sc)
		// Settle plays a temporary copy with resolve steps, so the target
		// rewrite is validated against a scenario whose stack empties. The
		// settled copy is what is kept: every cast carries gorge's exact
		// targets (cast steps' target decisions travel through castSpell and
		// are not scripted a second time). When gorge cannot cast at the
		// reduced price Settle fails and the unnormalized item is returned
		// as is, so the failed cast surfaces as a divergence, not a Skip.
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
		return it, nil
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static has no fixture"}
}

func appendUnique(dst []string, names ...string) []string {
	for _, n := range names {
		found := false
		for _, old := range dst {
			if old == n {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, n)
		}
	}
	return dst
}
