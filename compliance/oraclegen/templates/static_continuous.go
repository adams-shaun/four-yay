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
	var base oraclegen.Item
	switch {
	case req.Face > 0:
		// A face-1 static is served by setup-on-back-face, not by casting:
		// the card cannot be cast on its back face (that is out of scope),
		// so it is placed there directly and observed from genesis.
		base = staticBackFaceScenario(f, name, req)
	case requestedFace(c, name) != f:
		return skip("face is not the castable face")
	case oraclegen.HasType(f, "Land"):
		base = playLandWith(reg, name, f, func(setup map[string]oraclegen.Seat) {
			p0 := setup["p0"]
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, staticProbe)
			setup["p0"] = p0
			oraclegen.Baseline(setup, f)
		})
	default:
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
	res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return skip("scenario does not replay")
	}
	if !staticObserved(res.Snapshots[len(res.Snapshots)-1], f, name) {
		return skip("effect not observable on a probe or the card")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, base.Scenario)
	it.XAnswers = base.XAnswers
	it.Ignore = base.Ignore
	it.Compare = []string{oraclediff.CompareKeywords}
	return it, nil
}

// staticBackFaceScenario is the setup-only scenario for a static on a face
// after 0: the card on p0's battlefield in its back face, the probe on both
// seats, and no steps. The static is live from the first checkpoint, so the
// final snapshot is where its effect shows. The probe is placed by Baseline on
// p1 and appended here on p0, matching the cast-based path's probe layout.
func staticBackFaceScenario(f *cards.Face, name string, req levelb.Requirement) oraclegen.Item {
	p0 := oraclegen.Seat{Battlefield: []string{name}}
	setupBackFace(&p0, name, req)
	p0.Battlefield = appendFixtureUnique(p0.Battlefield, staticProbe)
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": {}},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
	}
	oraclegen.Baseline(sc.Setup, f)
	return StaticApplies.item(name, sc)
}

// staticObserved reports whether the final snapshot shows a continuous effect:
// a probe off its printed 2/2 with no keywords, or the card (p0's) off its
// printed P/T or evergreen keywords. The card is matched by its active face's
// printed name (f.Name), so a face-after-0 static whose permanent reports the
// back-face name is still recognised.
func staticObserved(s rules.OracleSnapshot, f *cards.Face, name string) bool {
	printedKW := oraclediff.EvergreenKeywords(f.Keywords)
	cardName := f.Name
	if cardName == "" {
		cardName = name
	}
	for _, p := range s.Permanents {
		switch {
		case p.Name == staticProbe:
			if p.PT != staticProbePT || oraclediff.EvergreenKeywords(p.Keywords) != "" {
				return true
			}
		case p.Controller == 0 && p.Name == cardName:
			if p.PT != "" && f.PT != "" && !strings.Contains(f.PT, "*") && p.PT != f.PT {
				return true
			}
			if oraclediff.EvergreenKeywords(p.Keywords) != printedKW {
				return true
			}
		}
	}
	return false
}
