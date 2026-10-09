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
	case strings.EqualFold(st.Params["Type"], "Ability") || (st.Params["ValidSpell"] != "" && !strings.EqualFold(st.Params["ValidSpell"], "Spell.Bargain")):
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
	p0.Battlefield = appendFixtureCounts(p0.Battlefield, p.battlefield)
	p1.Battlefield = appendFixtureCounts(p1.Battlefield, p.opponentBattlefield)
	p0.Graveyard = appendFixtureCounts(p0.Graveyard, p.graveyard)
	p0.Exile = appendUnique(p0.Exile, p.exile...)
	p0.Hand = append(p0.Hand, p.hand...)
	if p.activate == nil {
		if p.castFrom != "" {
			// The provenance probe casts from the zone its spell sits in, so
			// the spell is seeded there and nowhere else: a hand copy would
			// leave the cast step's ref ambiguous between two objects.
			p0.Graveyard = appendUnique(p0.Graveyard, p.spell)
		} else if p.opponent {
			p1.Hand = appendUnique(p1.Hand, p.spell)
		} else {
			p0.Hand = appendUnique(p0.Hand, p.spell)
		}
	}
	if p.first != nil {
		if p.first.Seat == 0 {
			p0.Hand = appendUnique(p0.Hand, strings.TrimPrefix(p.first.Card, "p0:"))
		} else {
			p1.Hand = appendUnique(p1.Hand, strings.TrimPrefix(p.first.Card, "p1:"))
		}
	}
	if p.precast != nil {
		p0.Hand = appendUnique(p0.Hand, p.precast.card)
		for _, target := range p.precast.targets {
			if strings.HasPrefix(target, "p0:") {
				p0.Battlefield = appendUnique(p0.Battlefield, strings.TrimPrefix(target, "p0:"))
			}
		}
	}
	if p.seat != nil {
		p.seat(&p0)
	}
	// A requirement on an alternate face (Norman Osborn's Green Goblin
	// ReduceCost) needs the source permanent showing that face: setup places
	// it on its back face (Seat.BackFace, an emitted FlipFace).
	setupBackFace(&p0, name, req)
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
	}
	sc.Steps = append(sc.Steps, fx.CombatSteps()...)
	sc.Steps = append(sc.Steps, fx.Prelude()...)
	sc.Steps = append(sc.Steps, p.pre...)
	if p.first != nil {
		sc.Steps = append(sc.Steps, *p.first)
		if !p.firstNoResolve {
			sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
		} else {
			sc.Steps = append(sc.Steps, oraclegen.Step{Op: "pass", Seat: p.first.Seat})
		}
	}
	targets := fx.Targets()
	if len(p.targets) > 0 {
		targets = append([]string(nil), p.targets...)
	}
	if p.precast != nil {
		// The probe targets the precast spell on the stack; interleave it at
		// the stack slot positions with the fixture's plain-slot targets.
		slots := oraclegen.SlotSpecs(f)
		stackIdx := stackSlotIndexes(slots)
		targets = insertStackTargets(slots, stackIdx, fx.Targets(), *p.precast)
	}
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
	step := oraclegen.Step{Op: "cast", Seat: castSeat, Card: castRef, Mana: p.mana, Targets: targets, CastMode: p.castMode, Answers: p.answers}
	if a := p.activate; a != nil {
		index := a.index
		step = oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + p.spell, Mana: p.mana, Targets: a.targets, AbilityIndex: &index}
	}
	if p.opponent {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "pass", Seat: 0})
	}
	if p.precast != nil {
		// p0 casts the precast and holds priority (CR 117.3c), so no pass
		// separates it from the discounted probe.
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + p.precast.card, Mana: p.precast.mana, Targets: p.precast.targets})
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
	}
	if p.activate != nil || len(p.preXAbility) > 0 {
		it.XAbility = growXAbility(it.XAbility, len(it.Steps))
		offset := len(fx.CombatSteps()) + len(fx.Prelude())
		copy(it.XAbility[offset:], p.preXAbility)
		if p.activate != nil {
			for i, s := range it.Steps {
				if s.Op == "activate" && i >= offset+len(p.preXAbility) {
					it.XAbility[i] = p.activate.prefix
				}
			}
		}
	}
	if p.plotText != "" {
		if i := plotStepIndex(it.Steps); i >= 0 {
			it.XAbility = growXAbility(it.XAbility, len(it.Steps))
			it.XAbility[i] = p.plotText
		}
	}
	return it
}
