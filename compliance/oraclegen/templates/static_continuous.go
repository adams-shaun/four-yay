// Level-B continuous-static template (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.3; ticket
// L11). The scenario is the card's level-A one (cast and resolve, or play the
// land) with a probe permanent, Grizzly Bears, on both seats: p1 already holds
// one through Baseline, p0 gets one here. The static arrives after setup, so a
// continuous effect that lands on a probe -- or on the card itself -- is a
// field that changes after the first checkpoint and is therefore frozen.
// Keywords are opt-in (Item.Compare "keywords"), so a granted keyword is part
// of the comparison.
//
// One scenario serves every static.continuous requirement of the face: each
// requirement key gets its own item built from the same scenario, so the
// duplicated rows are identical but each is its own verdict row.
//
// A candidate is served only when gorge shows an effect: a probe's P/T or
// evergreen keywords differ from Grizzly Bears' printed 2/2 with none, or the
// card's own P/T or evergreen keywords differ from its printed ones (a self
// static whose condition the fixture makes true). A static that lands on
// nothing observable stays a skip, so no item asserts a static the engine does
// not implement.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// StaticApplies serves static.continuous requirements. Its own version:
// bumping it stales only this family's level-B rows.
var StaticApplies = Template{ID: "static", Version: 1}

// staticProbe is the permanent on both seats a continuous static lands on.
const staticProbe = "Grizzly Bears"

// staticProbePT is the probe's printed P/T; the probe has no keywords.
const staticProbePT = "2/2"

// staticContinuous builds the scenario serving one static.continuous
// requirement.
func staticContinuous(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + why}
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return skip("card not in corpus")
	}
	if requestedFace(c, name) != f {
		return skip("face is not the castable face")
	}
	st := staticAt(f, req)
	if st == nil {
		return skip("slot " + req.Slot + " is not a static")
	}
	kind, need, gated := staticCounterGate(st)
	var base oraclegen.Item
	if gated {
		// The card starts on the battlefield holding the counters its gate
		// names, so the effect is already on at the first checkpoint.
		base = counterGatedBase(f, name, kind, need)
	} else if oraclegen.HasType(f, "Land") {
		base = playLandWith(reg, name, f, func(setup map[string]oraclegen.Seat) {
			p0 := setup["p0"]
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, staticProbe)
			setup["p0"] = p0
			oraclegen.Baseline(setup, f)
		})
	} else {
		mana, why := oraclegen.PoolFor(f.ManaCost)
		if why != "" {
			return skip(why)
		}
		it, sk := castResolveWith(reg, f, name, mana, []string{staticProbe})
		if sk != nil {
			return skip(sk.Reason)
		}
		base = it
	}
	if staticGatedOnMaxSpeed(st) {
		withMaxSpeed(base.Setup)
	}
	res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return skip("scenario does not replay")
	}
	if !staticObserved(res.Snapshots[len(res.Snapshots)-1], f, name, gated) {
		if (gated || staticGatedOnMaxSpeed(st)) && staticGrantsAbility(st) {
			return skip(staticGrantWaits)
		}
		return skip("effect not observable on a probe or the card")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, base.Scenario)
	it.XAnswers = base.XAnswers
	it.Ignore = base.Ignore
	it.Compare = []string{oraclediff.CompareKeywords}
	return it, nil
}

// staticObserved reports whether the final snapshot shows a continuous effect:
// a probe off its printed 2/2 with no keywords, or the card (p0's) off its
// printed P/T or evergreen keywords. typed also counts the card turning into a
// creature it is not printed as (a Spacecraft's station, a Vehicle's crew
// condition); only the counter-gated path asks for it, so every other static
// is judged exactly as before.
func staticObserved(s rules.OracleSnapshot, f *cards.Face, name string, typed bool) bool {
	printedKW := oraclediff.EvergreenKeywords(f.Keywords)
	for _, p := range s.Permanents {
		switch {
		case p.Name == staticProbe:
			if p.PT != staticProbePT || oraclediff.EvergreenKeywords(p.Keywords) != "" {
				return true
			}
		case p.Controller == 0 && p.Name == name:
			if p.PT != "" && f.PT != "" && !strings.Contains(f.PT, "*") && p.PT != f.PT {
				return true
			}
			if oraclediff.EvergreenKeywords(p.Keywords) != printedKW {
				return true
			}
			if typed && !f.IsCreature() && hasString(p.Types, "Creature") {
				return true
			}
		}
	}
	return false
}

// staticAt returns the static a requirement's slot indexes, or nil.
func staticAt(f *cards.Face, req levelb.Requirement) *cards.Static {
	i, err := strconv.Atoi(req.Slot)
	if err != nil || i < 0 || i >= len(f.Statics) {
		return nil
	}
	return &f.Statics[i]
}

// counterGatedBase is the scenario for a static gated on its own counters:
// the card and the probe on p0's battlefield, the card holding need counters
// of kind, and no steps -- the first checkpoint already shows the effect.
func counterGatedBase(f *cards.Face, name, kind string, need int32) oraclegen.Item {
	p0 := oraclegen.Seat{Battlefield: []string{name, staticProbe}}
	p0 = withSetupCounters(p0, name, kind, need)
	sc := oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": p0, "p1": {}},
		Steps: []oraclegen.Step{},
	}
	oraclegen.Baseline(sc.Setup, f)
	return oraclegen.Item{Scenario: sc}
}

func hasString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
