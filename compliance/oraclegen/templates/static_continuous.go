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
// A static gated on its own counters (`Card.Self+counters_GE<n>_<KIND>`, the
// Spacecraft station statics and the "as long as it has a counter" shapes) is
// served by placing the card on the battlefield holding exactly those counters
// (counterGatedBase): its effect is live at the first checkpoint, so no cast
// is needed and the fixture supplies the state the condition reads. A static
// gated on Condition$ MaxSpeed is placed with p0 at speed 4. A counter- or
// speed-gated static whose only effect grants an ability, trigger, static or
// replacement gets the named staticGrantWaits skip rather than the generic
// observability reason, because the probes cannot observe a granted ability
// (ticket levelb-static-granted-ability).
//
// A candidate is served only when gorge shows an effect: a probe's P/T or
// evergreen keywords differ from its printed ones (Grizzly Bears: 2/2, none), or the
// card's own P/T or evergreen keywords differ from its printed ones (a self
// static whose condition the fixture makes true), or -- for a counter-gated
// static -- the card has become a creature it is not printed as (a Spacecraft's
// station, a Vehicle's crew condition). A static that lands on nothing
// observable stays a skip, so no item asserts a static the engine does not
// implement.
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
	st, affected := staticSlotOf(f, req)
	_, _, gated := staticCounterGate(&st)
	speedGated := staticGatedOnMaxSpeed(&st)
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
		if !staticObserved(res.Snapshots[len(res.Snapshots)-1], f, name, specs, gated) {
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, base.Scenario)
		it.XAnswers = base.XAnswers
		it.Ignore = base.Ignore
		it.Compare = []string{oraclediff.CompareKeywords}
		return it, nil
	}
	// A counter- or speed-gated static that grants only an ability, trigger,
	// static or replacement is a named wait: the probes compare P/T and
	// evergreen keywords, which a granted ability never moves.
	if (gated || speedGated) && staticGrantsAbility(&st) {
		return skip(staticGrantWaits)
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
// Grizzly Bears on both seats. A static gated on its own counters is placed
// with the counters its gate names (counterGatedBase) before any cast path,
// because the condition is a state the fixture must supply; a static gated on
// Condition$ MaxSpeed gets p0 at speed 4.
func staticBase(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, plan staticProbePlan) (oraclegen.Item, string) {
	st, _ := staticSlotOf(f, req)
	probes := append([]string{staticProbe}, plan.probes...)
	var base oraclegen.Item
	switch {
	case staticCounterGated(&st):
		// The card starts on the battlefield holding the counters its gate
		// names, so the effect is already on at the first checkpoint.
		kind, need, _ := staticCounterGate(&st)
		base = counterGatedBase(f, name, kind, need)
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
	if staticGatedOnMaxSpeed(&st) {
		withMaxSpeed(base.Setup)
	}
	if plan.attack {
		attackers := staticAttackers(reg, name, probes, plan.self)
		base.Steps = append(base.Steps, oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers})
	}
	return base, ""
}

// staticSlotOf returns the static a requirement's slot indexes (the zero
// Static and "" when the slot is not a static) and its Affected$ filter.
func staticSlotOf(f *cards.Face, req levelb.Requirement) (cards.Static, string) {
	i, err := strconv.Atoi(req.Slot)
	if err != nil || i < 0 || i >= len(f.Statics) {
		return cards.Static{}, ""
	}
	st := f.Statics[i]
	return st, st.ParamStr(cards.PKAffected)
}

// staticCounterGated reports whether st applies only while its own source
// holds counters (the staticCounterGate shape).
func staticCounterGated(st *cards.Static) bool {
	_, _, ok := staticCounterGate(st)
	return ok
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
// a probe off its printed P/T or keywords, or the card (p0's) off its printed
// P/T or evergreen keywords. The card is matched by its active face's printed
// name (f.Name), so a face-after-0 static whose permanent reports the
// back-face name is still recognised. typed also counts the card turning into
// a creature it is not printed as (a Spacecraft's station, a Vehicle's crew
// condition); only the counter-gated path asks for it, so every other static
// is judged exactly as before.
func staticObserved(s rules.OracleSnapshot, f *cards.Face, name string, probes map[string]staticProbeSpec, typed bool) bool {
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
			if typed && !f.IsCreature() && hasString(p.Types, "Creature") {
				return true
			}
		}
	}
	return false
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
