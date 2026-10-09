// Level-B observation of a static whose AddStaticAbility$ grants a STATIC
// ability, observed through the granted static's own effect (ticket
// levelb-static-granted-abilities; the static slice the granted-ability,
// granted-trigger and donor templates left as the named skip). The outer
// static is a plain Mode$ Continuous whose Affected$ names the HOST the
// granted static lands on (the card itself for a chosen-mode Siege, the
// equipped creature for an Equipment, each planeswalker you control for
// Tomik); the granted SVar body is parsed with the same parser a printed S:
// line uses.
//
// Each shape is served by its own observation of the granted body's mode:
//
//   - ReduceCost: a spell in hand whose generic cost is one above the pool is
//     offered at the gate (max speed) and not without the source.
//   - Panharmonicon: the listener's trigger fires twice with the grant and
//     once in the source-removed control (the static_panharmonicon shape,
//     with the granted static's host as the listener for an attachment or
//     self grant).
//   - AttackRestrict: at the declare-attackers decision the attacked
//     planeswalker's option group is capped at the granted MaxAttackers$ and
//     uncapped in the source-removed control (the maxBlockers shape).
//   - Continuous: a probe off its printed P/T/keywords under the grant
//     (static_continuous's comparison), or a graveyard land offered as a play
//     for a granted MayPlay$ permission (static_offer's shape).
//
// Every scenario supplies the outer static's gate through grantedGateOf: the
// seat's speed, the source's counters, the as-enters mode choice (an explicit
// "modes" answer, so the runner's first-option fallback cannot pick the other
// mode), or the attachment the Affected$ names. An observation that does not
// hold gorge-side is never served, so the row keeps its named grant skip.
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

// grantedStaticItem is the AddStaticAbility$ observation entry point. ok is
// false when the granted body is a mode no shape here serves; the caller then
// falls through to its named grant skip.
func staticGrantedStaticItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	innerName := strings.TrimSpace(st.ParamStr(cards.PKAddStaticAbility))
	if innerName == "" {
		return oraclegen.Item{}, false
	}
	inners, ok := cards.ParseStaticLines(f.SVars[innerName])
	if !ok {
		return oraclegen.Item{}, false
	}
	gate := grantedGateOf(f, &st)
	for _, inner := range inners {
		switch {
		case strings.EqualFold(inner.Mode, "ReduceCost"):
			if it, ok := grantedReduceCostItem(reg, f, name, req, gate); ok {
				return it, true
			}
		case strings.EqualFold(inner.Mode, "Panharmonicon"):
			if it, ok := grantedPanharmoniconItem(reg, f, name, req, st, inner, gate); ok {
				return it, true
			}
		case strings.EqualFold(inner.Mode, "AttackRestrict"):
			if it, ok := grantedAttackRestrictItem(reg, f, name, req, inner, gate); ok {
				return it, true
			}
		case strings.EqualFold(inner.Mode, "Continuous"):
			if it, ok := grantedContinuousItem(reg, f, name, req, st, inner, gate); ok {
				return it, true
			}
		}
	}
	return oraclegen.Item{}, false
}

// grantedStaticScenario is staticScenario with the outer static's gate
// supplied: the seat's speed or counters in setup, and a mode or attachment
// gate moving the source to p0's hand and casting it (answering the as-enters
// mode choice explicitly, then attaching it to attachTo) before the
// observation steps. pre is the number of leading prelude steps, so a control
// can drop them when it drops the source.
func grantedStaticScenario(f *cards.Face, name string, gate grantedGate, attachTo string, battlefield, hand []string, steps []oraclegen.Step) (oraclegen.Scenario, int) {
	sc := staticScenario(f, name, battlefield, hand, steps)
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
	sc.Setup["p0"] = p0
	if gate.mode == "" && !gate.attach {
		return sc, 0
	}
	cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: gate.pool, Answers: xAnswers(f)}
	res := oraclegen.Step{Op: "resolve"}
	if gate.mode != "" {
		res.Answers = []oraclegen.Answer{{Kind: "modes", Pick: []string{gate.mode}}}
	}
	pre := []oraclegen.Step{cast, res}
	if gate.attach {
		pre = append(pre, oraclegen.Step{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + attachTo})
	}
	sc.Steps = append(pre, sc.Steps...)
	return sc, len(pre)
}

// grantedStaticControl drops the source and its prelude from sc: the same
// board and steps with nothing granted, so an observation that still holds
// there is not the grant's doing.
func grantedStaticControl(sc oraclegen.Scenario, name string, pre int) oraclegen.Scenario {
	ctl := sc
	ctl.Setup = make(map[string]oraclegen.Seat, len(sc.Setup))
	for k, v := range sc.Setup {
		ctl.Setup[k] = v
	}
	p0 := ctl.Setup["p0"]
	p0.Battlefield = removeString(p0.Battlefield, name)
	p0.Hand = removeFixtureOnce(p0.Hand, name)
	ctl.Setup["p0"] = p0
	ctl.Steps = append([]oraclegen.Step(nil), sc.Steps[pre:]...)
	return ctl
}

// grantedStaticItem finishes a served observation: the item, its XMage
// answers and, for a characteristic grant, the keyword opt-in.
func grantedStaticItem(f *cards.Face, name string, req levelb.Requirement, sc oraclegen.Scenario, res rules.OracleResult, cr []string, keywords bool) oraclegen.Item {
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, cr, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	if keywords {
		it.Compare = []string{oraclediff.CompareKeywords}
	}
	return it
}

// grantedReduceCostItem serves a granted ReduceCost static (Racers'
// Scoreboard's max-speed "Spells you cast cost {1} less"): a spell whose
// generic cost is one above the pool is offered while the source is on the
// battlefield and the gate holds, and not in the source-removed control.
func grantedReduceCostItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, gate grantedGate) (oraclegen.Item, bool) {
	spell := "Hill Giant"
	c, ok := reg.Lookup(spell)
	if !ok || len(c.Faces) == 0 || !oraclegen.XMageKnown(spell) {
		return oraclegen.Item{}, false
	}
	pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
	if why != "" || strings.Count(pool, "C") == 0 {
		return oraclegen.Item{}, false
	}
	pool = strings.Replace(pool, "C", "", 1)
	offered := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:" + spell}, Want: boolPtr(want)}}
	}
	steps := []oraclegen.Step{
		{Op: "mana", Seat: 0, Mana: pool},
		{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority", Expect: offered(true)},
	}
	sc, pre := grantedStaticScenario(f, name, gate, "", []string{name}, []string{spell}, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	ctl := grantedStaticControl(sc, name, pre)
	ctl.Steps[len(ctl.Steps)-1].Expect = offered(false)
	if cres, cok := runStatic(reg, ctl); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"601.2f"}, false), true
}

// grantedPanharmoniconItem serves a granted Panharmonicon static: the
// granted body's own filter decides the listener and cause. Two shapes:
// a creature-dying cause whose ValidCard$ names the host itself (The
// Masamune's equipped creature), and an attack cause (Windcrag Siege's
// "a creature attacking causes ... to trigger an additional time").
func grantedPanharmoniconItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, inner cards.Static, gate grantedGate) (oraclegen.Item, bool) {
	valid := strings.ToLower(inner.ParamStr(cards.PKValidCard))
	modes := inner.ParamStr(cards.PKValidMode)
	cause := strings.ToLower(strings.TrimSpace(inner.ParamStr(cards.PKValidCause)))
	dest := strings.ToLower(strings.TrimSpace(inner.ParamStr(cards.PKDestination)))
	origin := strings.ToLower(inner.ParamStr(cards.PKOrigin))
	switch {
	case cause == "creature" && dest == "graveyard" && strings.Contains(origin, "battlefield") && strings.Contains(valid, "self"):
		return grantedPanharmoniconDiesItem(reg, f, name, req, gate)
	case strings.Contains(modes, "Attacks"):
		return grantedPanharmoniconAttackItem(reg, f, name, req, gate)
	}
	return oraclegen.Item{}, false
}

// grantedPanharmoniconDiesItem is the Masamune shape: the equipped creature's
// "whenever a creature dies" trigger fires twice while the Equipment is
// attached and once in the source-removed control. Venomcrawler is the
// corpus listener: one untargeted, mandatory "another creature dies" trigger
// with no other printed ability.
func grantedPanharmoniconDiesItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, gate grantedGate) (oraclegen.Item, bool) {
	const listener, victim = "Venomcrawler", "Grizzly Bears"
	if _, ok := reg.Lookup(listener); !ok || !oraclegen.XMageKnown(listener) {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{
		{Op: "move", Seat: 0, Card: "p0:" + victim, To: "graveyard"},
		{Op: "pass_to", Seat: 0, Decision: "priority"},
	}
	sc, pre := grantedStaticScenario(f, name, gate, listener, []string{listener, victim}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if n, others := listenerAbilities(res.Snapshots[len(res.Snapshots)-1], "p0:"+listener); n != 2 || others != 0 {
		return oraclegen.Item{}, false
	}
	ctl := grantedStaticControl(sc, name, pre)
	cres, cok := runStatic(reg, ctl)
	if !cok || len(cres.Fails) != 0 || len(cres.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if n, _ := listenerAbilities(cres.Snapshots[len(cres.Snapshots)-1], "p0:"+listener); n != 1 {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"603.2d"}, false), true
}

// grantedPanharmoniconAttackItem is the Windcrag shape: a permanent you
// control's attack trigger fires twice with the Mardu mode chosen and once in
// the source-removed control. Kiln Walker is the corpus listener: one
// untargeted, mandatory attack trigger and no other printed ability. Exactly
// ONE attacker is declared -- a multi-attacker DeclareAttackers event has no
// singular cause and the echo scan fails closed.
func grantedPanharmoniconAttackItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, gate grantedGate) (oraclegen.Item, bool) {
	const listener = "Kiln Walker"
	if _, ok := reg.Lookup(listener); !ok || !oraclegen.XMageKnown(listener) {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + listener}},
		{Op: "pass_to", Seat: 0, Decision: "priority"},
	}
	sc, pre := grantedStaticScenario(f, name, gate, "", []string{listener}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if n, others := listenerAbilities(res.Snapshots[len(res.Snapshots)-1], "p0:"+listener); n != 2 || others != 0 {
		return oraclegen.Item{}, false
	}
	ctl := grantedStaticControl(sc, name, pre)
	cres, cok := runStatic(reg, ctl)
	if !cok || len(cres.Fails) != 0 || len(cres.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if n, _ := listenerAbilities(cres.Snapshots[len(cres.Snapshots)-1], "p0:"+listener); n != 1 {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"603.2d"}, false), true
}

// grantedAttackRestrictItem serves a granted AttackRestrict static (Tomik's
// "No more than one creature can attack this planeswalker", whose
// ValidDefender$ Card.Self resolves against the granted static's own source):
// at p0's declare-attackers decision the attacked planeswalker's option group
// is capped at MaxAttackers$ with the granting planeswalker on p1, and
// uncapped in the source-removed control. The expectation is the decision's
// own Group/GroupLimits read, the same rule Decision.Validate and the bot
// repair enforce, so the item cannot pass on a cap nothing applies.
func grantedAttackRestrictItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, inner cards.Static, gate grantedGate) (oraclegen.Item, bool) {
	const walker, first, second = "Ajani Goldmane", "Grizzly Bears", "Savannah Lions"
	if !strings.Contains(strings.ToLower(inner.ParamStr(cards.PKValidDefender)), "self") {
		return oraclegen.Item{}, false
	}
	cap, err := strconv.Atoi(strings.TrimSpace(inner.ParamStr(cards.PKMaxAttackers)))
	if err != nil || cap <= 0 {
		return oraclegen.Item{}, false
	}
	if _, ok := reg.Lookup(walker); !ok || !oraclegen.XMageKnown(walker) {
		return oraclegen.Item{}, false
	}
	declare := func(max int) oraclegen.Step {
		return oraclegen.Step{Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers",
			Expect: []oraclegen.Expect{
				attackCapExpect(cardAt(0, first), cardAt(1, walker), max),
				attackCapExpect(cardAt(0, second), cardAt(1, walker), max),
			}}
	}
	build := func(withCard bool) oraclegen.Scenario {
		p0 := oraclegen.Seat{Battlefield: []string{first, second}}
		p1 := oraclegen.Seat{Battlefield: []string{walker}}
		if withCard {
			p1.Battlefield = append([]string{name}, p1.Battlefield...)
		}
		sc := oraclegen.Scenario{
			Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
			SetupAnswers: oraclegen.OpeningHandAnswers(f),
			Steps:        []oraclegen.Step{declare(cap)},
		}
		oraclegen.Baseline(sc.Setup, f)
		return sc
	}
	sc := build(true)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	ctl := build(false)
	ctl.Steps[0] = declare(0)
	if cres, cok := runStatic(reg, ctl); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"508.1c"}, false), true
}

// attackCapExpect asserts the AttackRestrict cap the (attacker, battle)
// option's Group publishes; max 0 requires no group.
func attackCapExpect(attacker, battle string, max int) oraclegen.Expect {
	return oraclegen.Expect{CanAttack: &oraclegen.CanAttack{
		Attacker: attacker, Battle: battle, MaxAttackers: &max,
	}, Want: boolPtr(true)}
}

// grantedContinuousItem serves a granted Mode$ Continuous body: a granted
// MayPlay$ permission over a graveyard land (Glacierwood Siege's Sultai
// mode), or a granted characteristic change on a probe (Frostcliff Siege's
// Temur mode).
func grantedContinuousItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, inner cards.Static, gate grantedGate) (oraclegen.Item, bool) {
	if inner.HasParam(cards.PKMayPlay) {
		return grantedMayPlayItem(reg, f, name, req, inner, gate)
	}
	if inner.HasParam(cards.PKAddPower) || inner.HasParam(cards.PKAddToughness) ||
		inner.HasParam(cards.PKSetPower) || inner.HasParam(cards.PKSetToughness) || inner.HasParam(cards.PKAddKeyword) {
		return grantedProbeItem(reg, f, name, req, st, gate)
	}
	return oraclegen.Item{}, false
}

// grantedMayPlayItem serves a granted "you may play lands from your
// graveyard": a land in p0's graveyard is offered as a play while the source
// is on the battlefield with the gate mode chosen, and not in the
// source-removed control.
func grantedMayPlayItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, inner cards.Static, gate grantedGate) (oraclegen.Item, bool) {
	const probe = "Forest"
	zones := offerZones(inner.ParamStr(cards.PKAffectedZone))
	if len(zones) != 1 || zones[0] != offerGraveyard {
		return oraclegen.Item{}, false
	}
	t, ok := offerTryFor(reg, probe, offerGraveyard, false, false)
	if !ok || t.kind != "play" {
		return oraclegen.Item{}, false
	}
	offered := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: t.kind, Card: "p0:" + probe}, Want: boolPtr(want)}}
	}
	steps := []oraclegen.Step{{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offered(true)}}
	sc, pre := grantedStaticScenario(f, name, gate, "", nil, nil, steps)
	p0 := sc.Setup["p0"]
	p0.Graveyard = appendFixtureUnique(append([]string(nil), p0.Graveyard...), probe)
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	ctl := grantedStaticControl(sc, name, pre)
	ctl.Steps[len(ctl.Steps)-1].Expect = offered(false)
	if cres, cok := runStatic(reg, ctl); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"305.1", "611.3"}, false), true
}

// grantedProbeItem serves a granted characteristic static: the probe is off
// its printed P/T or keywords under the gate while the source is on the
// battlefield, and unchanged in the source-removed control. The comparison
// is static_continuous's own (staticObservedNamed over staticProbeSpecs), so
// a grant the engine does not apply cannot be served by accident.
func grantedProbeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, gate grantedGate) (oraclegen.Item, bool) {
	specs := staticProbeSpecs(reg, []string{staticProbe})
	observed := func(res rules.OracleResult) bool {
		if len(res.Snapshots) == 0 {
			return false
		}
		ok, _ := staticObservedNamed(res.Snapshots[len(res.Snapshots)-1], f, name, st, specs)
		return ok
	}
	sc, pre := grantedStaticScenario(f, name, gate, "", []string{staticProbe}, nil, nil)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || !observed(res) {
		return oraclegen.Item{}, false
	}
	ctl := grantedStaticControl(sc, name, pre)
	cres, cok := runStatic(reg, ctl)
	if !cok || len(cres.Fails) != 0 || observed(cres) {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"611.3", "613"}, true), true
}
