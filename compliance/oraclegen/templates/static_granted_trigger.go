// Level-B observation of a continuous static whose AddTrigger$ grants a
// TRIGGERED ability (ticket levelb-granted-triggers, the trigger slice of the
// levelb-static-granted-abilities class). The snapshot carries no list of a
// permanent's abilities, so the grant is observed by MAKING the granted
// trigger fire: the scenario supplies the static's gate (speed 4, the gate's
// own counters, the chosen mode, or the attachment the Affected$ names), the
// cause steps of the sub-family the granted trigger's body classifies to, and
// the item is produced only when gorge shows the granted trigger on the stack.
// A granted trigger's stack entry carries no Trigger slot
// (rules/oracle_snapshot.go stackTriggerSlot), so it is told apart from a
// printed trigger by the empty slot alone -- printed triggers of the same
// source are not exercised here, and an empty-slot entry with this source
// name can only be a grant of this static.
//
// The causes come from the same recipes the printed-trigger template uses
// (triggerRecipe), classified by levelb.classifyTrigger over the granted
// body. The three shapes the classifier leaves a gap map locally, without
// touching the requirement set, to a sub-family whose recipe the shape fits:
// granted combat damage once (DamageDealtOnce, combat damage from a creature
// you control or the attached one) to trigger.combat-damage, and an
// opponent's discard (Discarded, ValidCard$ Card.OppOwn) to
// trigger.discarded with the discard moved to p1. A grant on the source
// itself whose trigger names ValidCard$ Card.Self attacks with the source
// (the spacecraft's "Whenever this Spacecraft attacks") instead of a probe.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// grantedTriggerSlot is the Trigger slot a GRANTED trigger's stack entry
// carries (rules/oracle_snapshot.go stackTriggerSlot returns "" for anything
// that is not one of the source's printed face triggers).
const grantedTriggerSlot = ""

// grantedGate is the state a scenario must supply for the granting static to
// be live, read off the static's own gate parameters.
type grantedGate struct {
	speed       bool
	counters    bool
	counterKind string
	counterNeed int32
	mode        string // the ChosenMode<X> gate's mode name
	attach      bool   // the source must be attached to a permanent
	pool        string // the source's cast mana, when the gate needs it cast
}

// staticGrantedTriggerItem builds the item for one static requirement whose
// static grants a triggered ability. ok is false when no granted trigger of
// this static fired under a scenario the runner can play; the caller falls
// through to its named grant gap.
func staticGrantedTriggerItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	granted := grantedTriggers(f, &st)
	if len(granted) == 0 {
		return oraclegen.Item{}, false
	}
	gate := grantedGateOf(f, &st)
	for i := range granted {
		t := &granted[i]
		sub, gap, covered := levelb.ClassifyTrigger(f, t)
		if gap != "" || covered {
			mapped, known := grantedTriggerSub(t)
			if !known {
				continue
			}
			sub = mapped
		}
		var causes []triggerCause
		switch {
		case sub == "trigger.combat-damage" && mappedGranted(t):
			// The recipe refuses a non-creature grantor; the granted shape's
			// own cause is a probe creature attacking unblocked.
			causes = grantedCombatDamageCause()
		case (sub == "trigger.discarded" || sub == "trigger.discarded-opponent") &&
			filterHasTokenFold(t.ParamStr(cards.PKValidCard), "oppown"):
			// "Whenever an opponent discards a card": p1 discards a bear.
			// The classifier now names this shape trigger.discarded-opponent
			// (its own Mind-Rot-at-p1 recipe); a granted body keeps the
			// instant draw-and-discard probe cause this route has always
			// observed, so the pinned granted items are unchanged.
			causes = grantedOpponentDiscardCauses(reg)
		default:
			var why string
			causes, why = triggerRecipe(reg, f, name, t, sub)
			if why != "" {
				continue
			}
		}
		if grantOnSource(&st) && namesSelfFold(t.ParamStr(cards.PKValidCard)) {
			causes = selfAttackCauses(name, causes)
		}
		fxs := triggerFixtures(reg, f, t)
		for _, cause := range causes {
			if it, ok := grantedTriggerWith(reg, f, name, req, sub, cause, gate, fxs); ok {
				return it, true
			}
		}
	}
	return oraclegen.Item{}, false
}

// grantedTriggers resolves the SVar trigger bodies st's AddTrigger$ names,
// each the same shape a printed T: line has.
func grantedTriggers(f *cards.Face, st *cards.Static) []cards.Trigger {
	raw := strings.TrimSpace(st.ParamStr(cards.PKAddTrigger))
	if raw == "" {
		return nil
	}
	var out []cards.Trigger
	for _, nm := range cards.SplitGrantNames(raw) {
		if t, ok := cards.ParseTriggerLine(f.SVars[nm]); ok {
			out = append(out, t)
		}
	}
	return out
}

// grantedTriggerSub maps the granted-trigger shapes the level-B classifier
// leaves a gap to the sub-family a recipe serves, without touching the
// requirement set: only granted bodies take this route.
func grantedTriggerSub(t *cards.Trigger) (string, bool) {
	switch t.ModeKind() {
	case cards.TriggerDamageDealtOnce, cards.TriggerDamageDoneOnce:
		if strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True") &&
			(namesYouCtrlFold(t.ParamStr(cards.PKValidSource)) || containsAnyFold(t.ParamStr(cards.PKValidSource), "equippedby")) {
			return "trigger.combat-damage", true
		}
	case cards.TriggerDiscarded:
		if filterHasTokenFold(t.ParamStr(cards.PKValidCard), "oppown") {
			return "trigger.discarded", true
		}
	}
	return "", false
}

// mappedGranted reports whether the shape the local sub mapping served came
// from one of the two once-per-event damage modes, so the custom cause is
// built only for it and a cleanly-classified combat-damage trigger (its
// source itself attacking) keeps its recipe.
func mappedGranted(t *cards.Trigger) bool {
	k := t.ModeKind()
	return k == cards.TriggerDamageDealtOnce || k == cards.TriggerDamageDoneOnce
}

// grantedCombatDamageCause is the combat-damage cause for a granted trigger:
// the probe bear attacks unblocked and deals combat damage to p1. The trigger
// resolves inside the pass to main2, so the emitted item shows no stack; the
// probe stops in end-combat, where the trigger is still on it.
func grantedCombatDamageCause() []triggerCause {
	attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + bearsProbe}}
	return []triggerCause{{
		battlefield: []string{bearsProbe},
		steps:       []oraclegen.Step{attack, {Op: "pass_to", Step: "main2"}},
		probeSteps:  []oraclegen.Step{attack, {Op: "pass_to", Step: "end-combat"}},
	}}
}

// grantedOpponentDiscardCauses discard a card from p1's hand on p0's first
// turn: p0 passes, p1 casts an instant draw-and-discard probe at itself, and
// whatever p1 discards is p1-owned. A sorcery probe is not offered on p0's
// turn (CR 117.1a), so the probes are instants.
func grantedOpponentDiscardCauses(reg *cards.Registry) []triggerCause {
	var out []triggerCause
	for _, probe := range []string{"Occult Epiphany", "Enhanced Awareness"} {
		if !oraclegen.XMageKnown(probe) {
			continue
		}
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		pool, why := oraclegen.PoolFor(card.Faces[0].ManaCost)
		if why != "" {
			continue
		}
		st := oraclegen.Step{Op: "cast", Seat: 1, Card: "p1:" + probe, Mana: pool, Answers: xAnswers(card.Faces[0])}
		out = append(out, triggerCause{
			opponentHand: []string{probe, bearsProbe},
			steps: []oraclegen.Step{
				{Op: "pass", Seat: 0}, st, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0},
				// The spell resolves between the last two passes and the
				// granted trigger goes on the stack; the pass_to stops there,
				// so a checkpoint shows it (a resolve op would drain it).
				{Op: "pass_to", Decision: "priority"},
			},
		})
	}
	return out
}

// selfAttackCauses rewrites every attack step's attacker to the source
// itself, for a grant that lands on the source and a trigger whose
// ValidCard$ names Self ("Whenever this Spacecraft attacks"): the probe bear
// the recipe picked does not match the filter.
func selfAttackCauses(name string, causes []triggerCause) []triggerCause {
	for i := range causes {
		for j, s := range causes[i].steps {
			if s.Op == "attack" {
				causes[i].steps[j].Attackers = []string{"p0:" + name}
			}
		}
		for j, s := range causes[i].probeSteps {
			if s.Op == "attack" {
				causes[i].probeSteps[j].Attackers = []string{"p0:" + name}
			}
		}
	}
	return causes
}

// grantOnSource reports whether the static's grant lands on the static's own
// source permanent: the normalized Affected$ names Self, or no filter at all
// (the engine's Card.Self default for a self-only static, rules/layers.go).
func grantOnSource(st *cards.Static) bool {
	aff := strings.TrimSpace(st.ParamStr(cards.PKAffected))
	return aff == "" || namesSelfFold(aff)
}

// grantedGateOf reads the static's gate into the state a scenario supplies.
// A gate that would need the source cast (a ChosenMode choice or an
// attachment) also resolves the cast's mana pool; a face without one leaves
// the gate unbuilt and the row keeps its named skip.
func grantedGateOf(f *cards.Face, st *cards.Static) grantedGate {
	g := grantedGate{speed: staticGatedOnMaxSpeed(st)}
	if kind, need, ok := staticCounterGate(st); ok {
		g.counters, g.counterKind, g.counterNeed = true, kind, need
	}
	g.mode = chosenModeGate(st)
	aff := st.ParamStr(cards.PKAffected)
	g.attach = containsAnyFold(aff, "attachedto", "attachedby", "enchantedby", "equippedby", "fortifiedby") ||
		(!grantOnSource(st) && oraclegen.HasType(f, "Equipment"))
	if g.mode != "" || g.attach {
		pool, why := oraclegen.PoolFor(f.ManaCost)
		if why != "" {
			g.mode, g.attach = "", false
		}
		g.pool = pool
	}
	return g
}

// chosenModeGate returns the mode name a `Card.Self+ChosenMode<X>` Affected$
// gate reads, or "" when st has none.
func chosenModeGate(st *cards.Static) string {
	aff := st.ParamStr(cards.PKAffected)
	if i := strings.Index(aff, "ChosenMode"); i >= 0 {
		return strings.TrimSpace(aff[i+len("ChosenMode"):])
	}
	return ""
}

// grantedTriggerWith tries one cause against each target fixture, mirroring
// the printed-trigger template's flow (triggerWithFixture) with the granted
// slot in place of the printed one and the gate supplied to every scenario.
func grantedTriggerWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, sub string, c triggerCause, gate grantedGate, fxs []oraclegen.Fixture) (oraclegen.Item, bool) {
	for i := range fxs {
		if it, ok := grantedTriggerWithFixture(reg, f, name, req, sub, c, gate, &fxs[i]); ok {
			return it, true
		}
	}
	return oraclegen.Item{}, false
}

// grantedOnStack reports whether a snapshot shows the granted trigger on the
// stack: an ability entry with the granted trigger's empty Trigger slot whose
// source names the card -- or, when the gate attached it, the permanent the
// card is attached to (an attachment grant's recipient carries the trigger,
// so its stack entry names the host). A printed trigger of the same source
// carries its slot, and the scenario never activates the source, so an
// empty-slot entry can only be this static's grant.
func grantedOnStack(snaps []rules.OracleSnapshot, name, faceName string, gate grantedGate) bool {
	wants := []string{strings.ToLower(name), strings.ToLower(faceName)}
	if gate.attach {
		wants = append(wants, strings.ToLower(staticProbe))
	}
	for _, s := range snaps {
		for _, e := range s.Stack {
			if e.Kind != "ability" || e.Trigger != grantedTriggerSlot {
				continue
			}
			source := strings.ToLower(e.Source)
			for _, want := range wants {
				if want != "" && strings.Contains(source, want) {
					return true
				}
			}
		}
	}
	return false
}

// grantedTriggerWithFixture is one cause against one target fixture.
func grantedTriggerWithFixture(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, sub string, c triggerCause, gate grantedGate, fx *oraclegen.Fixture) (oraclegen.Item, bool) {
	probe := c.probeSteps
	if probe == nil {
		probe = c.steps
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	settled := append(append([]oraclegen.Step(nil), probe...), passes...)
	checkpoint := append(append([]oraclegen.Step(nil), settled...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	ordered := append(append([]oraclegen.Step(nil), probe...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	fired := false
	for _, steps := range [][]oraclegen.Step{probe, settled, checkpoint, ordered} {
		if _, res, ok := oraclegen.Settle(reg, grantedTriggerScenario(f, name, c, req, steps, fx, gate)); ok &&
			grantedOnStack(res.Snapshots, name, f.Name, gate) {
			fired = true
			break
		}
	}
	if !fired {
		return oraclegen.Item{}, false
	}
	sc := grantedTriggerScenario(f, name, c, req, c.steps, fx, gate)
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	if sub == "trigger.phase" && !grantedOnStack(res.Snapshots, name, f.Name, gate) {
		for _, d := range res.Decisions {
			if d.GorgeKind == "trigger_order" {
				sc.Steps = append(sc.Steps, oraclegen.Step{Op: "pass_to", Decision: "priority"})
				break
			}
		}
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
		if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
			sc, res = yes, res2
		}
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	it.XAnswers = scriptPreludeSacrifice(it.XAnswers, c.prelude, len(sc.Steps))
	it.XAnswers = scriptPreludeActivationCost(it.XAnswers, c.prelude, c.preludeActivationCost, len(sc.Steps), res.Decisions)
	it.XAnswers = scriptCauseActivationCost(it.XAnswers, c, sc.Steps, res.Decisions)
	return it, true
}

// grantedTriggerScenario builds the cause's scenario with the gate supplied:
// the seat's speed or the source's counters in setup, and a ChosenMode or
// attachment gate moving the source to p0's hand and casting it (resolving
// into the chosen mode, then attaching it) before the cause steps run.
func grantedTriggerScenario(f *cards.Face, name string, c triggerCause, req levelb.Requirement, steps []oraclegen.Step, fx *oraclegen.Fixture, gate grantedGate) oraclegen.Scenario {
	sc := triggerScenario(f, name, c, req, steps, fx)
	p0 := sc.Setup["p0"]
	if gate.speed {
		p0.Speed = maxSpeed
	}
	if gate.counters {
		p0 = oraclegen.WithCounters(p0, name, gate.counterKind, gate.counterNeed)
	}
	if gate.mode != "" || gate.attach {
		p0.Battlefield = removeString(p0.Battlefield, name)
		p0.Hand = append(p0.Hand, name)
	}
	if gate.attach {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, staticProbe)
	}
	sc.Setup["p0"] = p0
	if gate.mode != "" || gate.attach {
		cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: gate.pool, Answers: xAnswers(f)}
		res := oraclegen.Step{Op: "resolve"}
		if gate.mode != "" {
			res.Answers = []oraclegen.Answer{{Kind: "modes", Pick: []string{gate.mode}}}
		}
		pre := []oraclegen.Step{cast, res}
		if gate.attach {
			pre = append(pre, oraclegen.Step{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + staticProbe})
		}
		sc.Steps = append(pre, sc.Steps...)
	}
	return sc
}

// containsAnyFold reports whether s contains any of the subs, case-insensitively.
func containsAnyFold(s string, subs ...string) bool {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(low, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// namesYouCtrlFold reports whether a filter selects a permanent you control
// (the "youctrl" token form of filterHasTokenFold).
func namesYouCtrlFold(filter string) bool { return filterHasTokenFold(filter, "youctrl") }
