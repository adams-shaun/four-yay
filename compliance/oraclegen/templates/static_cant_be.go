package templates

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The CantBeCast / CantBeActivated shapes beyond the combat window (spec
// section 7). Each observes "this option is not offered" with an Offered
// expectation (want=false) at the checkpoint where the named seat holds
// priority, and is served only when the identical checkpoint with the static
// absent offers it.

const (
	opponentCastProbe = "Gut Shot"
	limitFirstProbe   = "Ornithopter"
	limitSecondProbe  = "Memnite"
)

// offeredNot is a want=false Offered expectation.
func offeredNot(seat int, kind, card string) []oraclegen.Expect {
	return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: seat, Kind: kind, Card: card}, Want: boolPtr(false)}}
}

// serveOffered replays sc, requires it to satisfy its want=false assertion,
// requires control (the source-removed replay) to offer the option, and
// returns the item.
func serveOffered(reg *cards.Registry, f *cards.Face, name, mode string, req levelb.Requirement, cr string, sc oraclegen.Scenario, control func() string) (oraclegen.Item, *oraclegen.Skip) {
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, fmt.Sprintf("offered assertion does not hold (ok=%v fails=%v)", ok, res.Fails))
	}
	if why := control(); why != "" {
		return staticSkip(name, mode, "control does not offer the option: "+why)
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{cr}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// cantBeCastOpponentTurnItem: p0 passes priority in main1 and p1 holds Gut
// Shot ({R/P}, payable with life, so no mana pool is needed).
func cantBeCastOpponentTurnItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	sc := staticScenario(f, name, []string{name}, nil, []oraclegen.Step{{Op: "pass", Seat: 0, Expect: offeredNot(1, "cast", "p1:"+opponentCastProbe)}})
	p1 := sc.Setup["p1"]
	p1.Hand = []string{opponentCastProbe}
	sc.Setup["p1"] = p1
	return serveOffered(reg, f, name, "CantBeCast", req, "601.2", sc, func() string { return sourceRemovedControlFails(reg, sc, name) })
}

// cantBeActivatedOpponentTurnItem: the same pass, p1 holding Mogg Fanatic.
func cantBeActivatedOpponentTurnItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	sc := staticScenario(f, name, []string{name}, nil, []oraclegen.Step{{Op: "pass", Seat: 0, Expect: offeredNot(1, "activate", "p1:"+activatedProbe)}})
	p1 := sc.Setup["p1"]
	p1.Battlefield = append(p1.Battlefield, activatedProbe)
	sc.Setup["p1"] = p1
	return serveOffered(reg, f, name, "CantBeActivated", req, "602.2", sc, func() string { return sourceRemovedControlFails(reg, sc, name) })
}

// cantBeActivatedAllItem: Mogg Fanatic's ability at p0's main1 priority, with
// no phase window.
func cantBeActivatedAllItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	step := oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offeredNot(0, "activate", "p0:"+activatedProbe)}
	sc := staticScenario(f, name, []string{name, activatedProbe}, nil, []oraclegen.Step{step})
	return serveOffered(reg, f, name, "CantBeActivated", req, "602.2", sc, func() string { return sourceRemovedControlFails(reg, sc, name) })
}

// cantBeActivatedEnchantedItem: the Aura is cast onto Mogg Fanatic (a setup
// Aura would sit unattached, which state-based actions remove). The control
// cannot reuse sourceRemovedControlFails, whose replay keeps the cast steps
// naming the removed Aura, so it leaves the Aura in the library-side hand-less
// setup and drops the cast and resolve steps with it.
func cantBeActivatedEnchantedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return staticSkip(name, "CantBeActivated", "aura cost unpayable: "+why)
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: []string{"p0:" + activatedProbe}},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offeredNot(0, "activate", "p0:"+activatedProbe)},
	}
	sc := staticScenario(f, name, []string{activatedProbe}, []string{name}, steps)
	return serveOffered(reg, f, name, "CantBeActivated", req, "602.2", sc, func() string {
		control := staticScenario(f, name, []string{activatedProbe}, nil, steps[2:])
		control.Steps[0].Expect = []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + activatedProbe}, Want: boolPtr(true)}}
		res, ok := runStatic(reg, control)
		switch {
		case !ok:
			return "control did not run"
		case len(res.Fails) != 0:
			return fmt.Sprintf("%v", res.Fails)
		}
		return ""
	})
}

// cantBeCastFirstTurnsItem: the spell is not offered in p0's first turn. The
// control is the same offer in p0's fourth turn (turn 7), reached by three hops
// to p0's next main1.
func cantBeCastFirstTurnsItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return staticSkip(name, "CantBeCast", "spell cost unpayable: "+why)
	}
	offer := func(want bool) []oraclegen.Step {
		return []oraclegen.Step{
			{Op: "mana", Seat: 0, Mana: pool},
			{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:" + name}, Want: boolPtr(want)}}},
		}
	}
	sc := staticScenario(f, name, nil, []string{name}, offer(false))
	// A pass_to main1 stops at once when the game is already in a main1, so
	// each hop leaves through main2 first.
	var controlSteps []oraclegen.Step
	for i := 0; i < 3; i++ {
		controlSteps = append(controlSteps, oraclegen.Step{Op: "pass_to", Step: "main2"}, oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p0"})
	}
	return serveOffered(reg, f, name, "CantBeCast", req, "601.2", sc, func() string {
		control := staticScenario(f, name, nil, []string{name}, append(controlSteps, offer(true)...))
		res, ok := runStatic(reg, control)
		switch {
		case !ok:
			return "control did not run"
		case len(res.Fails) != 0:
			return fmt.Sprintf("%v", res.Fails)
		}
		return ""
	})
}
