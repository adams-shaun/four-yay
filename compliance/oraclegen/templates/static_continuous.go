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
// A static whose Affected$ filter names a subtype, a non-creature type or a
// state Grizzly Bears lacks is retried with the probes static_probes.go
// derives from the filter (a Squirrel for a Squirrel lord, an attack step for
// "attacking" permanents).
//
// A candidate is served only when gorge shows an effect: a probe's P/T or
// evergreen keywords differ from its printed ones (Grizzly Bears: 2/2, none), or the
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

// staticContinuous builds the scenario serving one static.continuous
// requirement. It tries Grizzly Bears alone first, so a row the Bear already
// observes keeps its scenario byte for byte; a row that shows nothing is
// retried once with the probes its Affected$ filter names (staticPlanFor).
func staticContinuous(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + why}
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return skip("card not in corpus")
	}
	st, affected := cards.Static{}, ""
	if i, err := strconv.Atoi(req.Slot); err == nil && i >= 0 && i < len(f.Statics) {
		st = f.Statics[i]
		affected = st.ParamStr(cards.PKAffected)
	}
	plans := []staticProbePlan{{}}
	probePlan := staticPlanFor(affected)
	if !probePlan.empty() {
		plans = append(plans, probePlan)
	}
	for i, plan := range plans {
		base, why := staticBase(reg, c, f, name, req, plan)
		if why != "" && i == 0 {
			return skip(why)
		}
		if why != "" {
			break
		}
		res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
		if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
			if i == 0 {
				return skip("scenario does not replay")
			}
			break
		}
		specs := staticProbeSpecs(reg, append([]string{staticProbe}, plan.probes...))
		if !staticObserved(res.Snapshots[len(res.Snapshots)-1], f, name, specs) {
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, base.Scenario)
		it.XAnswers = base.XAnswers
		it.Ignore = base.Ignore
		it.Compare = []string{oraclediff.CompareKeywords}
		return it, nil
	}
	if gap := staticProbeCapGap(probePlan); gap != "" {
		return skip(gap)
	}
	if gap := staticGap(st, affected); gap != "" {
		return skip(gap)
	}
	return skip("effect not observable on a probe or the card")
}

// staticBase builds the unobserved scenario for one probe plan; why is a skip
// reason when no scenario exists. The zero plan is the original template:
// Grizzly Bears on both seats.
func staticBase(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, plan staticProbePlan) (oraclegen.Item, string) {
	probes := append([]string{staticProbe}, plan.probes...)
	var base oraclegen.Item
	switch {
	case req.Face > 0 || plan.self:
		// A face-1 static is served by setup-on-back-face, not by casting:
		// the card cannot be cast on its back face (that is out of scope),
		// so it is placed there directly and observed from genesis. A self
		// static that needs the card attacking is placed the same way: a
		// cast creature is summoning sick and cannot attack.
		base = staticBackFaceScenario(f, name, req, probes)
	case requestedFace(c, name) != f:
		return base, "face is not the castable face"
	case oraclegen.HasType(f, "Land"):
		base = playLandWith(reg, name, f, func(setup map[string]oraclegen.Seat) {
			p0 := setup["p0"]
			for _, probe := range probes {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, probe)
			}
			setup["p0"] = p0
			oraclegen.Baseline(setup, f)
		})
	default:
		mana, why := oraclegen.PoolFor(f.ManaCost)
		if why != "" {
			return base, why
		}
		it, sk := castResolveWith(reg, f, name, mana, probes)
		if sk != nil {
			return base, sk.Reason
		}
		if oraclegen.HasType(f, "Equipment") {
			it.Steps = append(it.Steps, oraclegen.Step{
				Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + staticProbe,
			})
		}
		base = it
	}
	if plan.attack {
		attackers := staticAttackers(reg, name, probes, plan.self)
		base.Steps = append(base.Steps, oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers})
	}
	return base, ""
}

// staticAttackers is the p0 creatures an attack step sends: every creature
// probe, plus the card itself when the static is on it.
func staticAttackers(reg *cards.Registry, name string, probes []string, self bool) []string {
	var out []string
	if self {
		out = append(out, "p0:"+name)
	}
	for _, p := range probes {
		if c, ok := reg.Lookup(p); ok && len(c.Faces) > 0 && c.Faces[0].IsCreature() {
			out = append(out, "p0:"+p)
		}
	}
	return out
}

// staticBackFaceScenario is the setup-only scenario for a static on a face
// after 0: the card on p0's battlefield in its back face, the probe on both
// seats, and no steps. The static is live from the first checkpoint, so the
// final snapshot is where its effect shows. The probe is placed by Baseline on
// p1 and appended here on p0, matching the cast-based path's probe layout.
func staticBackFaceScenario(f *cards.Face, name string, req levelb.Requirement, probes []string) oraclegen.Item {
	p0 := oraclegen.Seat{Battlefield: []string{name}}
	setupBackFace(&p0, name, req)
	for _, probe := range probes {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, probe)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": {}},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
	}
	oraclegen.Baseline(sc.Setup, f)
	return StaticApplies.item(f, name, sc)
}

// staticObserved reports whether the final snapshot shows a continuous effect:
// a probe off its printed 2/2 with no keywords, or the card (p0's) off its
// printed P/T or evergreen keywords. The card is matched by its active face's
// printed name (f.Name), so a face-after-0 static whose permanent reports the
// back-face name is still recognised.
func staticObserved(s rules.OracleSnapshot, f *cards.Face, name string, probes map[string]staticProbeSpec) bool {
	printedKW := oraclediff.EvergreenKeywords(f.Keywords)
	cardName := f.Name
	if cardName == "" {
		cardName = name
	}
	for _, p := range s.Permanents {
		spec, isProbe := probes[p.Name]
		switch {
		case isProbe:
			if p.PT != spec.pt || oraclediff.EvergreenKeywords(p.Keywords) != spec.keywords {
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
