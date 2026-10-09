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
// on the card itself; an AddTrigger$ grant is observed by firing the granted
// trigger (static_granted_trigger.go); unserved grants fall through to their
// named gap.
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
	"sort"
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

// staticProbeAura is the inert Aura the enchanted-probe prelude casts onto
// the probe. Pacifism changes no compared field (P/T, keywords, types,
// colours), so only the static under test moves the probe.
const staticProbeAura = "Pacifism"

// addEnchantedProbeAura casts the inert Aura onto the probe, so a static on
// enchanted creatures (A Tale for the Ages, Archon of the Wild Rose) finds an
// enchanted probe. The Aura is cast, never setup-placed with an attach step:
// setup cannot place an unattached Aura -- XMage's state-based actions move
// one to its owner's graveyard at game start, so the attach step's permanent
// ref finds nothing (driver-batch-20261009T201406Z, Zoetic Glyph).
func addEnchantedProbeAura(reg *cards.Registry, base *oraclegen.Item, probe string) {
	p0 := base.Scenario.Setup["p0"]
	p0.Hand = appendFixtureUnique(p0.Hand, staticProbeAura)
	base.Scenario.Setup["p0"] = p0
	if st, ok := castProbe(reg, staticProbeAura, "p0:"+probe); ok {
		base.Steps = append(base.Steps, st, oraclegen.Step{Op: "resolve", Seat: 0})
	}
}

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
	// observable. It rides staticBaseline, which also carries a state
	// fixture's other own-contribution shifts (an attached Equipment's
	// constant P/T grant on the probe or the card) and the token specs the
	// token fixture created, so the observation reads only changes the
	// static itself makes.
	served := func(base oraclegen.Item, res rules.OracleResult, plan staticProbePlan, withHost bool, bl staticBaseline) (oraclegen.Item, bool) {
		specs := staticProbeSpecs(reg, append([]string{staticProbe}, plan.probes...))
		if bl.probePT != [2]int32{} {
			if spec, ok := specs[staticProbe]; ok {
				specs[staticProbe] = shiftProbePT2(spec, bl.probePT)
			}
		}
		snap := res.Snapshots[len(res.Snapshots)-1]
		if withHost {
			specs = staticWithAttachHost(reg, snap, f.Name, specs)
		}
		observed, namedOnly := staticObservedNamed(snap, f, name, st, specs, bl)
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
	// snapshot without rebuilding or replaying the scenario. Each carries
	// its fixture's baseline, so the fallback reads only changes the static
	// itself makes (an attached Equipment's own +2/+0, a token's printed
	// spec).
	type staticCandidate struct {
		base oraclegen.Item
		res  rules.OracleResult
		plan staticProbePlan
		bl   staticBaseline
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
		if it, ok := served(base, res, plan, false, staticBaseline{}); ok {
			return it, nil
		}
		candidates = append(candidates, staticCandidate{base, res, plan, staticBaseline{}})
	}
	// A condition or count the bare scenario leaves false or zero: retry each
	// candidate fixture with each probe plan. This runs only after every bare
	// scenario failed, so a row the bare scenarios serve keeps its bytes.
	for _, cond := range staticFixtures(reg, c, f, name, st) {
		for _, plan := range plans {
			base, why := staticBase(reg, c, f, name, req, plan, &cond)
			if why != "" {
				continue
			}
			res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				continue
			}
			// A fixture whose afterSteps ask XMage questions (an activate
			// step's cost) re-derives XAnswers from the full replay: the cast
			// path's own XAnswers cover only the steps it replayed. The cast
			// steps are named in the map so their own targets are not
			// scripted a second time, and the activation's cost selector is
			// scripted from the runner's observed picks at the step the
			// fixture's activation runs.
			if cond.reanswers {
				base.XAnswers = oraclegen.XAnswersForScenario(res, base.Scenario,
					oraclegen.ModeNumbers(f), castStepIndices(base.Scenario.Steps))
				if cond.activationCost != "" {
					for i := len(base.Scenario.Steps) - len(cond.afterSteps); i < len(base.Scenario.Steps); i++ {
						if i < 0 || i >= len(base.Scenario.Steps) || base.Scenario.Steps[i].Op != "activate" {
							continue
						}
						// The generic derivation already scripts an observed
						// material exile pick; the catalogue fixture appended
						// beside it would be an unused second answer on the
						// same target ask (XMage consumes the queue FIFO), so
						// the cost selector is scripted only when the runner
						// recorded no exile pick.
						if !exileCostPickObserved(res.Decisions, i) {
							addActivationCostAnswers(base.XAnswers, i, cond.activationCost, name, res.Decisions)
						}
					}
				}
			}
			bl := cond.baseline()
			if it, ok := served(base, res, plan, false, bl); ok {
				return it, nil
			}
			candidates = append(candidates, staticCandidate{base, res, plan, bl})
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
			if it, ok := served(base, res, plan, false, staticBaseline{probePT: [2]int32{shift, shift}}); ok {
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
		if it, ok := served(cand.base, cand.res, cand.plan, true, cand.bl); ok {
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
	// A has-all-abilities-of static (GainsAbilitiesOf$) is observed by using a
	// donor card's activated ability on the recipient (static_granted_donor.go).
	if it, ok := staticDonorItem(reg, f, name, req, st); ok {
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
	// A static whose AddTrigger$ grants a TRIGGERED ability is observed by
	// firing the granted trigger (static_granted_trigger.go). Tried after
	// every probe, fixture and offered path so an already-served row keeps
	// its scenario bytes; an unserved grant falls through to its named gap.
	if st.HasParam(cards.PKAddTrigger) {
		if it, ok := staticGrantedTriggerItem(reg, f, name, req, st); ok {
			return it, nil
		}
	}
	// A chosen-name mana grant (Petrified Hamlet's "Lands with the chosen
	// name have '{T}: Add {C}.') is observed on the named probe land the
	// source's ETB trigger names (static_named_enters.go); the grant gap's
	// fixture cannot give a recipient its chosen name.
	if namedCardManaGrant(f, &st) {
		if it, skip := staticGrantedNamedCardItem(reg, f, name, req, &st); skip == nil {
			return it, nil
		}
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

// castStepIndices is the set of steps that are casts: XAnswersForScenario's
// castSteps argument, so a cast step's own targets are not scripted a second
// time on a re-derived answer stream.
func castStepIndices(steps []oraclegen.Step) map[int]bool {
	out := map[int]bool{}
	for i, st := range steps {
		if st.Op == "cast" {
			out[i] = true
		}
	}
	return out
}

// exileCostPickObserved reports whether the replay recorded a material exile
// cost pick (a choose_n whose pick kind is "exilecost") at step: the cost
// selector whose observed pick the generic XAnswers derivation already
// scripts.
func exileCostPickObserved(ds []rules.OracleDecision, step int) bool {
	for _, d := range ds {
		if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" {
			continue
		}
		if hasPickKind(d, "exilecost") {
			return true
		}
	}
	return false
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
	selfKind, selfNeed, selfGated := staticSelfCounterGate(f, &st)
	var base oraclegen.Item
	switch {
	case req.Face > 0 && levelb.IsRoomCard(c):
		// A Room's second door is cast, not placed: no engine unlocks a
		// setup-placed door (CR 709.5). The face named by its own face name
		// is the door the runner binds to room_alt.
		return roomDoorCastBase(reg, c, f, name, req, probes)
	case cond != nil && cond.craft:
		// The static under test sits on the back face a Craft activation
		// transforms into, so the card is not placed on it: the front face
		// is cast here and the fixture's afterSteps run the Craft activation,
		// which returns the card transformed with the exile set populated.
		if len(c.Faces) == 0 {
			return base, "craft fixture needs a front face"
		}
		front := c.Faces[0]
		mana, why := oraclegen.PoolFor(front.ManaCost)
		if why != "" {
			return base, why
		}
		var setup func(*oraclegen.Fixture)
		if cond != nil {
			setup = cond.apply
		}
		it, sk := castResolveWith(reg, front, name, mana, probes, setup)
		if sk != nil {
			return base, sk.Reason
		}
		base = it
	case selfGated && !staticSelfETB(f):
		// A static gated on its own counters through CheckSVar$ X with
		// SVar:X:Count$CardCounters.<KIND> (Warden of the Inner Sky): the
		// card starts on the battlefield holding the counters its gate
		// names, so the effect is live at the first checkpoint. A card with
		// its own ETB trigger stays on the cast path (staticSelfETB).
		base = counterGatedBase(f, name, selfKind, selfNeed, plan.probes)
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
		if cond != nil && cond.manaExtra > 0 {
			// A count of the unspent mana pool: the cast is paid with more
			// mana than it needs and the pool keeps the rest.
			mana += strings.Repeat("C", cond.manaExtra)
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
	if cond != nil && len(cond.sourceCounters) > 0 && sourceOnBattlefield(base, name) {
		// A static whose amount is Count$CardCounters.<KIND> on the source
		// (Excalibur II): the card is placed holding the counters the fixture
		// names, so the amount is nonzero. An Equipment is attached to the
		// probe here (the cast path's attach ran only on the cast branch).
		applySourceCounters(&base, name, cond.sourceCounters)
		if oraclegen.HasType(f, "Equipment") && !hasAttachStep(base.Steps) {
			base.Steps = append(base.Steps, oraclegen.Step{
				Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + staticProbe,
			})
		}
	}
	if plan.aura {
		addEnchantedProbeAura(reg, &base, staticProbe)
	}
	if cond != nil && cond.equip != "" {
		// The fixture's Equipment attaches now: the card is on the
		// battlefield whichever path built it (cast, played, placed), and so
		// is the probe the attach targets.
		target := "p0:" + staticProbe
		if cond.attach == "self" {
			target = "p0:" + name
		}
		base.Steps = append(base.Steps, oraclegen.Step{
			Op: "attach", Seat: 0, Card: "p0:" + cond.equip, AttachedTo: target,
		})
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
		if cond != nil {
			attackers = append(attackers, cond.tokenAttackers...)
		}
		base.Steps = append(base.Steps, oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers})
	}
	if cond != nil && len(cond.afterSteps) > 0 {
		// The card is on the battlefield by now (cast, played or placed), so
		// a battlefield trigger of its own (the solved-Case "To solve"
		// sequence) can run and resolve before the final checkpoint, and an
		// activation the fixture's own prelude drives (the crew, the Craft)
		// carries the ability's XMage rule-text prefix.
		base.Steps = append(base.Steps, cond.afterSteps...)
		if len(cond.afterXab) > 0 {
			base.XAbility = growXAbility(base.XAbility, len(base.Steps))
			copy(base.XAbility[len(base.Steps)-len(cond.afterXab):], cond.afterXab)
		}
	}
	if cond != nil && cond.opponentTurn {
		// A Condition$ NotPlayerTurn static (Midnight Mangler) is false on
		// p0's own turn; advance to p1's first main phase so its controller
		// is not the active player at the final checkpoint. The shape is the
		// opponent-turn cost probe's (cost_other_spell.go): pass_to reaches
		// p1's turn, p1's pass leaves p0 with priority.
		base.Steps = append(base.Steps,
			oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p1"},
			oraclegen.Step{Op: "pass", Seat: 1})
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

// staticBaseline is the state a fixture itself contributes to the observed
// permanents, so the observation reads only what the static adds on top: the
// constant P/T grant a fixture's Equipment makes on the probe or on the card,
// and the printed specs of the tokens a fixture created.
type staticBaseline struct {
	probePT [2]int32
	cardPT  [2]int32
	tokens  map[string]staticProbeSpec
}

// baseline folds a fixture's own contribution into an observation baseline:
// an Equipment attached to the probe shifts the compared probe P/T, one
// attached to the card shifts the card's, and the token fixture's token spec
// is what its tokens are compared against.
func (s staticFixture) baseline() staticBaseline {
	var bl staticBaseline
	switch s.attach {
	case staticProbe:
		bl.probePT = s.attachPT
	case "self":
		bl.cardPT = s.attachPT
	}
	if s.tokenName != "" {
		bl.tokens = map[string]staticProbeSpec{s.tokenName: s.tokenSpec}
	}
	return bl
}

// shiftProbePT2 returns spec with its creature P/T raised by (p, t): the
// probe holds counters the fixture placed or wears the fixture's Equipment,
// so the compared baseline is the state the fixture alone gives it and only a
// change the static itself makes is observable.
func shiftProbePT2(spec staticProbeSpec, d [2]int32) staticProbeSpec {
	if !spec.creature || spec.pt == "" || (d[0] == 0 && d[1] == 0) {
		return spec
	}
	return staticProbeSpec{pt: shiftPT(spec.pt, d[0], d[1]), keywords: spec.keywords, namedKeywords: spec.namedKeywords, creature: spec.creature, chars: spec.chars}
}

// shiftPT raises a "p/t" string by (dp, dt), "" when it does not parse.
func shiftPT(pt string, dp, dt int32) string {
	slash := strings.IndexByte(pt, '/')
	if slash < 0 {
		return ""
	}
	p, err1 := strconv.Atoi(pt[:slash])
	t, err2 := strconv.Atoi(pt[slash+1:])
	if err1 != nil || err2 != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", int32(p)+dp, int32(t)+dt)
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
	return staticObservedWith(s, f, name, st, probes, false, staticBaseline{})
}

// staticObservedNamed reports whether the snapshot shows a continuous effect
// and, when it does, whether only the wider named-keyword vocabulary sees it.
// It tries the evergreen vocabulary first, so an item the evergreen set
// already observes keeps CompareKeywords; a grant of a named ability
// (Ward, Prowess, Wither, Persist, Firebending) is observed only under the
// named fold and makes the item opt in to CompareKeywordsNamed.
func staticObservedNamed(s rules.OracleSnapshot, f *cards.Face, name string, st cards.Static, probes map[string]staticProbeSpec, base staticBaseline) (observed, namedOnly bool) {
	if staticObservedWith(s, f, name, st, probes, false, base) {
		return true, false
	}
	if staticObservedWith(s, f, name, st, probes, true, base) {
		return true, true
	}
	return false, false
}

// staticObservedWith is staticObserved under a chosen keyword vocabulary.
// named selects the wider fold (ComparedKeywords(..., true)). base carries
// the fixture's own contribution (an attached Equipment's P/T grant, the
// created tokens' printed specs), which is never read as the static's effect.
func staticObservedWith(s rules.OracleSnapshot, f *cards.Face, name string, st cards.Static, probes map[string]staticProbeSpec, named bool, base staticBaseline) bool {
	printedKW := oraclediff.ComparedKeywords(f.Keywords, named)
	printedChars := printedStaticChars(f)
	cardName := f.Name
	if cardName == "" {
		cardName = name
	}
	expectedCardPT := f.PT
	if base.cardPT != [2]int32{} {
		if shifted := shiftPT(f.PT, base.cardPT[0], base.cardPT[1]); shifted != "" {
			expectedCardPT = shifted
		}
	}
	for _, p := range s.Permanents {
		spec, isProbe := probes[p.Name]
		switch {
		case p.Token && base.tokens != nil:
			// A token a fixture created: the static's grant lands on it, and
			// the printed spec is the token script's, not a card's. A token
			// whose spec is unknown is not read (a spec-less token can never
			// be mistaken for a changed one).
			if ts, ok := base.tokens[p.Name]; ok {
				if (p.PT != "" || ts.pt != "") && p.PT != ts.pt {
					return true
				}
				if oraclediff.ComparedKeywords(p.Keywords, named) != ts.keywordsFor(named) {
					return true
				}
			}
		case isProbe:
			if p.PT != spec.pt || oraclediff.ComparedKeywords(p.Keywords, named) != spec.keywordsFor(named) || staticCharsMoved(p, spec.chars) {
				return true
			}
		case p.Controller == 0 && p.Name == cardName:
			if p.PT != "" && f.PT != "" && !strings.Contains(f.PT, "*") && p.PT != expectedCardPT {
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

// sourceOnBattlefield reports whether the card under test is on p0's setup
// battlefield (the placed path), where setup counters can be seeded.
func sourceOnBattlefield(base oraclegen.Item, name string) bool {
	for _, n := range base.Scenario.Setup["p0"].Battlefield {
		if n == name {
			return true
		}
	}
	return false
}

// applySourceCounters seeds the fixture's counters on the card under test.
func applySourceCounters(base *oraclegen.Item, name string, counters map[string]int) {
	p0 := base.Scenario.Setup["p0"]
	kinds := make([]string, 0, len(counters))
	for kind := range counters {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		p0 = withSetupCounters(p0, name, kind, int32(counters[kind]))
	}
	base.Scenario.Setup["p0"] = p0
}

// hasAttachStep reports whether steps already carry an attach op.
func hasAttachStep(steps []oraclegen.Step) bool {
	for _, st := range steps {
		if st.Op == "attach" {
			return true
		}
	}
	return false
}

func hasString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
