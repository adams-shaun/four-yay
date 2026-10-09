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
// gated on Condition$ MaxSpeed is placed with p0 at speed 4. Counter- or
// speed-gated AddAbility statics use the granted-ability offered observation
// on the card itself; unserved grants fall through to their named gap.
//
// A candidate is served only when gorge shows an effect: a probe's P/T,
// evergreen keywords, types or colours differ from its printed ones (Grizzly
// Bears: 2/2, none), or the card's own differ from its printed ones (a self
// static whose condition the fixture makes true), or the card shows the P/T of
// its own characteristic-defining static, or a GainControl$ static leaves a
// permanent under a controller who is not its owner (static_observe.go). A
// counter-gated static whose card has become a creature it is not printed as
// (a Spacecraft's station, a Vehicle's crew condition) is caught the same way,
// by the card's types moving. A static that lands on nothing observable stays
// a skip, so no item asserts a static the engine does not implement.
package templates

import (
	"fmt"
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
	// A gated self grant is observed as an offered ability on the card
	// itself. Try that first, but on failure fall through to the generic
	// probe and fixture paths: a gated grant that also carries an
	// observable effect (a level creature's SetPower/SetToughness, Echo
	// Mage, Joraga Treespeaker) is served by the generic path and must not
	// be converted into a skip. The helper's specific reason is kept for
	// the final gap. A grant whose Affected$ names other cards is not a self
	// grant and is not this helper's to claim.
	var gatedGrantWhy string
	if (gated || speedGated) && st.HasParam(cards.PKAddAbility) && staticSelfGrantedAbility(&st) {
		it, why, ok := staticGatedGrantedAbilityItem(reg, c, f, name, req, st)
		if ok {
			return it, nil
		}
		gatedGrantWhy = why
	}
	plans := []staticProbePlan{{}}
	probePlan := staticPlanFor(affected)
	if !probePlan.empty() {
		plans = append(plans, probePlan)
	}
	// served is the item for a scenario that replays and shows an effect.
	// withHost widens the compared probes with the permanent the card is
	// attached to (an Aura or Equipment's effect lands on its host, which a
	// fixture such as p1's Ornithopter can be). It is tried only in the final
	// fallback pass, never on a candidate an earlier path already served, so a
	// row that served without it keeps its scenario bytes (Zoetic Glyph).
	// counterShift is the +n/+n the probe-counters fixture put on the base
	// probe: the compared probe spec's P/T is raised by it, so the counters
	// alone are the baseline and only a change the static itself makes is
	// observable.
	served := func(base oraclegen.Item, res rules.OracleResult, plan staticProbePlan, withHost bool, counterShift int32) (oraclegen.Item, bool) {
		specs := staticProbeSpecs(reg, append([]string{staticProbe}, plan.probes...))
		if counterShift != 0 {
			if spec, ok := specs[staticProbe]; ok {
				specs[staticProbe] = shiftProbePT(spec, counterShift)
			}
		}
		snap := res.Snapshots[len(res.Snapshots)-1]
		if withHost {
			specs = staticWithAttachHost(reg, snap, f.Name, specs)
		}
		observed, namedOnly := staticObservedNamed(snap, f, name, st, specs)
		if !observed {
			return oraclegen.Item{}, false
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, base.Scenario)
		it.XAnswers = base.XAnswers
		it.Ignore = base.Ignore
		it.XAbility = base.XAbility
		// An evergreen grant keeps the evergreen opt-in; a grant of a named
		// ability (Ward, Prowess, Wither, Persist, Firebending) is observable
		// only under the wider vocabulary, so the item names it. The
		// evergreen check is tried first, so an item the evergreen set
		// already serves is untouched.
		if namedOnly {
			it.Compare = []string{oraclediff.CompareKeywordsNamed}
		} else {
			it.Compare = []string{oraclediff.CompareKeywords}
		}
		return it, true
	}
	// Candidates that replayed and showed nothing on the plain probes are
	// kept so the attach-host fallback can retry observation on their own
	// snapshot without rebuilding or replaying the scenario.
	type staticCandidate struct {
		base oraclegen.Item
		res  rules.OracleResult
		plan staticProbePlan
	}
	var candidates []staticCandidate
	for i, plan := range plans {
		base, why := staticBase(reg, c, f, name, req, plan, nil)
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
		if it, ok := served(base, res, plan, false, 0); ok {
			return it, nil
		}
		candidates = append(candidates, staticCandidate{base, res, plan})
	}
	// A condition or count the bare scenario leaves false or zero: retry each
	// candidate fixture with each probe plan. This runs only after every bare
	// scenario failed, so a row the bare scenarios serve keeps its bytes.
	for _, cond := range staticFixtures(reg, f, st) {
		for _, plan := range plans {
			base, why := staticBase(reg, c, f, name, req, plan, &cond)
			if why != "" {
				continue
			}
			res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				continue
			}
			if it, ok := served(base, res, plan, false, 0); ok {
				return it, nil
			}
			candidates = append(candidates, staticCandidate{base, res, plan})
		}
	}
	// A static whose affected permanent carries a counter gate the bare
	// scenario and every condition fixture leaves at zero
	// (Creature.YouCtrl+counters_GE1_P1P1, Permanent.YouCtrl+HasCounters):
	// retry with the probe holding the counters the gate names. It runs only
	// after every existing candidate failed, so an already-served row keeps
	// its bytes, and the compared probe spec is shifted by the counters, so a
	// static the engine does not implement still shows nothing and falls
	// through to its named gap.
	if fx, ok := staticProbeCounterFixture(&st, affected); ok {
		for _, plan := range plans {
			base, why := staticBase(reg, c, f, name, req, plan, &fx)
			if why != "" {
				continue
			}
			res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				continue
			}
			// Only P1P1 counters move the probe's P/T; a gate on another kind
			// leaves the compared spec alone.
			shift := int32(fx.probeCounters["P1P1"])
			if it, ok := served(base, res, plan, false, shift); ok {
				return it, nil
			}
		}
	}
	// Retry Aura rows whose cast target is killed by their own -X/-X static
	// with a setup-attached, larger probe. This is after all existing candidates
	// so already-served scenarios retain their bytes.
	if it, ok := staticSurvivingAuraItem(reg, f, name, req, st); ok {
		return it, nil
	}
	// Retry Equipment rows whose ETB target would consume the probe: answer the
	// ETB with a different legal permanent (or decline an optional target).
	for _, cand := range candidates {
		if it, ok := staticPreserveETBProbe(reg, f, name, req, st, cand.base, cand.res); ok {
			return it, nil
		}
	}
	// Fallback: a static that lands on the permanent the card is attached to
	// rather than on a probe (Puppet Crafting, Shimmerwilds Growth). This
	// runs only after every existing candidate failed, so it can never win an
	// earlier candidate and change an already-served row's scenario bytes.
	for _, cand := range candidates {
		if it, ok := served(cand.base, cand.res, cand.plan, true, 0); ok {
			return it, nil
		}
	}
	// A removal of all abilities defined by the permanent the card enchants
	// (Flood the Engine, Frozen in Ice, ...) is unobservable on the vanilla
	// fixture; retry it on a host that prints an ability. After every existing
	// path, so a served row keeps its bytes.
	if it, ok := staticAbilityRemovalItem(reg, f, name, req, st); ok {
		return it, nil
	}
	// A static that acts on cards outside the battlefield changes nothing a
	// snapshot shows; it is observed as an offered option instead
	// (static_offer.go). A look-at permission shows in neither engine's
	// snapshot, so it carries its own named skip.
	if it, ok := staticOfferItem(reg, c, f, name, req, st); ok {
		return it, nil
	}
	if st.HasParam(cards.PKMayLookAt) {
		if it, ok := lookAtLibraryTopItem(reg, f, name, req); ok {
			return it, nil
		}
		// The library-top read fails on a MayLookAt$ static the plain shape
		// does not cover: a grant gated on the source's own Case being solved
		// (the Case solves itself at the end step), or a grant over a
		// face-down battlefield permanent rather than the library top. Both
		// carry the named look-at skip when their scenario cannot be built.
		if it, ok := lookAtSolvedItem(reg, f, name, req, st); ok {
			return it, nil
		}
		if it, ok := lookAtFaceDownItem(reg, f, name, req, st); ok {
			return it, nil
		}
		return skip(staticLookAtReason)
	}
	// A player-rule static (SetMaxHandSize$) changes no snapshot field; it is
	// observed as a runner expectation of the effective maximum
	// (static_hand_size.go). A value the engine does not price keeps its
	// named gap below.
	if st.HasParam(cards.PKSetMaxHandSize) {
		if it, ok := staticMaxHandSizeItem(reg, f, name, req, st); ok {
			return it, nil
		}
	}
	if it, ok := staticSpellLifelinkItem(reg, c, f, name, req, st); ok {
		return it, nil
	}
	if gap := staticProbeCapGap(probePlan); gap != "" {
		return skip(gap)
	}
	if gap := staticOffBattlefieldGrantGap(st); gap != "" {
		return skip(gap)
	}
	if gated || speedGated {
		if gatedGrantWhy != "" {
			return skip(gatedGrantWhy)
		}
		if gap := staticGrantGap(f, st); gap != "" {
			return skip(gap)
		}
	}
	if gap := staticObserveGap(f, st); gap != "" {
		return skip(gap)
	}
	if gap := staticGap(st, affected); gap != "" {
		return skip(gap)
	}
	if gap := staticConditionGap(f, st); gap != "" {
		return skip(gap)
	}
	if gap := staticGrantGap(f, st); gap != "" {
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
func staticBase(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, plan staticProbePlan, cond *staticFixture) (oraclegen.Item, string) {
	st, _ := staticSlotOf(f, req)
	probes := append([]string{staticProbe}, plan.probes...)
	var base oraclegen.Item
	switch {
	case staticCounterGated(&st) && (!staticSelfETB(f) || stationGatedSelf(f, &st)):
		// The card starts on the battlefield holding the counters its gate
		// names, so the effect is already on at the first checkpoint. A card
		// with its own ETB trigger cannot use this path: XMage never fires a
		// setup-placed permanent's ETB (see staticSelfETB), so it stays on
		// the cast path below.
		kind, need, _ := staticCounterGate(&st)
		base = counterGatedBase(f, name, kind, need, plan.probes)
	case cond != nil && !cond.setupOnly() && (req.Face > 0 || plan.self || oraclegen.HasType(f, "Land")) && !cond.place:
		return base, "fixture needs steps the scenario has no cast for"
	case req.Face > 0 || plan.self || (cond != nil && cond.place):
		// A face-1 static is served by setup-on-back-face, not by casting:
		// the card cannot be cast on its back face (that is out of scope),
		// so it is placed there directly and observed from genesis. A self
		// static that needs the card attacking is placed the same way: a
		// cast creature is summoning sick and cannot attack.
		base = staticBackFaceScenario(f, name, req, probes, cond)
		if req.Face > 0 {
			staticBackFaceAttach(&base, f, name, st.ParamStr(cards.PKAffected))
		}
	case requestedFace(c, name) != f:
		return base, "face is not the castable face"
	case oraclegen.HasType(f, "Land"):
		base = playLandWith(reg, name, f, func(setup map[string]oraclegen.Seat) {
			p0 := setup["p0"]
			for _, probe := range probes {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, probe)
			}
			p1 := setup["p1"]
			if cond != nil {
				cond.seats(&p0, &p1)
			}
			setup["p0"], setup["p1"] = p0, p1
			oraclegen.Baseline(setup, f)
		})
	default:
		mana, why := oraclegen.PoolFor(f.ManaCost)
		if why != "" {
			return base, why
		}
		var setup func(*oraclegen.Fixture)
		if cond != nil {
			setup = cond.apply
		}
		it, sk := castResolveWith(reg, f, name, mana, probes, setup)
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
	if band := classStaticBand(&st); band >= 2 {
		// A ClassBand$ static is live only from its level on. The card is on
		// the battlefield by now (cast, played or placed), so raise the Class
		// through its own level-up activators before the effect is observed at
		// the final checkpoint. A face that cannot build the prelude keeps a
		// named skip.
		steps, xab, ok := classLevelPrelude(f, name, band)
		if !ok {
			return base, staticClassReason
		}
		base.Steps = append(base.Steps, steps...)
		base.XAbility = growXAbility(base.XAbility, len(base.Steps))
		copy(base.XAbility[len(base.Steps)-len(xab):], xab)
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

// shiftProbePT returns spec with its creature P/T raised by n/n: the probe
// holds P1P1 counters the fixture placed, so the compared baseline is the P/T
// the counters alone give it and only a change the static itself makes is
// observable.
func shiftProbePT(spec staticProbeSpec, n int32) staticProbeSpec {
	if !spec.creature || spec.pt == "" || n <= 0 {
		return spec
	}
	slash := strings.IndexByte(spec.pt, '/')
	if slash < 0 {
		return spec
	}
	p, err1 := strconv.Atoi(spec.pt[:slash])
	t, err2 := strconv.Atoi(spec.pt[slash+1:])
	if err1 != nil || err2 != nil {
		return spec
	}
	spec.pt = fmt.Sprintf("%d/%d", int32(p)+n, int32(t)+n)
	return spec
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
func staticBackFaceScenario(f *cards.Face, name string, req levelb.Requirement, probes []string, cond *staticFixture) oraclegen.Item {
	p0, p1 := oraclegen.Seat{Battlefield: []string{name}}, oraclegen.Seat{}
	setupBackFace(&p0, name, req)
	for _, probe := range probes {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, probe)
	}
	if cond != nil {
		cond.seats(&p0, &p1)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		// Observed from genesis, so no steps -- but an empty array, not
		// JSON null: the XMage driver reads "steps" as an array, and a null
		// crashed whole-set replays (LCI, TLA, 2026-10-06).
		Steps: []oraclegen.Step{},
	}
	oraclegen.Baseline(sc.Setup, f)
	return StaticApplies.item(f, name, sc)
}

// staticObserved reports whether the final snapshot shows a continuous effect:
// a probe off its printed P/T, evergreen keywords, types or colours; the card
// (p0's) off its printed P/T, evergreen keywords, types or colours, or showing
// the P/T of its own characteristic-defining static; or any permanent whose
// controller is not its owner under a GainControl$ static. The card is
// matched by its active face's printed name (f.Name), so a face-after-0 static
// whose permanent reports the back-face name is still recognised. A
// counter-gated card that became a creature it is not printed as (a
// Spacecraft's station, a Vehicle's crew condition) shows up in its types, the
// same staticCharsMoved comparison every other card uses.
func staticObserved(s rules.OracleSnapshot, f *cards.Face, name string, st cards.Static, probes map[string]staticProbeSpec) bool {
	return staticObservedWith(s, f, name, st, probes, false)
}

// staticObservedNamed reports whether the snapshot shows a continuous effect
// and, when it does, whether only the wider named-keyword vocabulary sees it.
// It tries the evergreen vocabulary first, so an item the evergreen set
// already observes keeps CompareKeywords; a grant of a named ability
// (Ward, Prowess, Wither, Persist, Firebending) is observed only under the
// named fold and makes the item opt in to CompareKeywordsNamed.
func staticObservedNamed(s rules.OracleSnapshot, f *cards.Face, name string, st cards.Static, probes map[string]staticProbeSpec) (observed, namedOnly bool) {
	if staticObservedWith(s, f, name, st, probes, false) {
		return true, false
	}
	if staticObservedWith(s, f, name, st, probes, true) {
		return true, true
	}
	return false, false
}

// staticObservedWith is staticObserved under a chosen keyword vocabulary.
// named selects the wider fold (ComparedKeywords(..., true)).
func staticObservedWith(s rules.OracleSnapshot, f *cards.Face, name string, st cards.Static, probes map[string]staticProbeSpec, named bool) bool {
	printedKW := oraclediff.ComparedKeywords(f.Keywords, named)
	printedChars := printedStaticChars(f)
	cardName := f.Name
	if cardName == "" {
		cardName = name
	}
	for _, p := range s.Permanents {
		spec, isProbe := probes[p.Name]
		switch {
		case isProbe:
			if p.PT != spec.pt || oraclediff.ComparedKeywords(p.Keywords, named) != spec.keywordsFor(named) || staticCharsMoved(p, spec.chars) {
				return true
			}
		case p.Controller == 0 && p.Name == cardName:
			if p.PT != "" && f.PT != "" && !strings.Contains(f.PT, "*") && p.PT != f.PT {
				return true
			}
			if p.PT != "" && strings.Contains(f.PT, "*") && staticSelfCDA(st) {
				return true
			}
			if oraclediff.ComparedKeywords(p.Keywords, named) != printedKW || staticCharsMoved(p, printedChars) {
				return true
			}
		}
		if p.Controller != p.Owner && st.HasParam(cards.PKGainControl) {
			return true
		}
	}
	return false
}

// keywordsFor returns the probe's printed keyword fold under the chosen
// vocabulary.
func (spec staticProbeSpec) keywordsFor(named bool) string {
	if named {
		return spec.namedKeywords
	}
	return spec.keywords
}

// counterGatedBase is the scenario for a static gated on its own counters:
// the card and the probe on p0's battlefield, the card holding need counters
// of kind plus the plan's extra probes, and no steps -- the first checkpoint
// already shows the effect.
func counterGatedBase(f *cards.Face, name, kind string, need int32, probes []string) oraclegen.Item {
	p0 := oraclegen.Seat{Battlefield: []string{name, staticProbe}}
	for _, probe := range probes {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, probe)
	}
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
