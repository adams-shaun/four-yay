// Level-B observations for the offer-reading static modes (ticket
// levelb-remaining-static-modes): an activated ability a summoning-sick
// creature may still activate, a power-up ability's additional activation
// ceiling, a plot offer for the top card of the library, a hexproof creature
// that can be targeted after all, and a suspect designation that never lands.
// Offered-style claims ride the runner's Expect vocabulary so a later
// regression fails the frozen replay; the paired control replays the same
// checkpoint with the card absent.
package templates

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// hasteProbe is the summoning-sick creature whose {T} ability the exemption
// makes activatable: exactly one activated ability, so the offered check
// without a label names it unambiguously.
const hasteProbe = "Prodigal Sorcerer"

// offeredYes is a want=true Offered expectation.
func offeredYes(seat int, kind, card string) []oraclegen.Expect {
	return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: seat, Kind: kind, Card: card}, Want: boolPtr(true)}}
}

// activateAsIfHasteItem serves ActivateAbilityAsIfHaste (Shang-Chi): the
// probe is cast this turn, so its {T} ability is withheld at the first
// priority; with the card on the battlefield the same checkpoint offers it
// (the control) or refuses it (the observation), both asserted on the frozen
// replay.
func activateAsIfHasteItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "ActivateAbilityAsIfHaste"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	probe, ok := reg.Lookup(hasteProbe)
	if !ok || len(probe.Faces) == 0 {
		return skip("probe not in corpus")
	}
	if len(probe.Faces[0].Abilities) != 1 {
		return skip("probe does not carry exactly one activated ability")
	}
	pool, why := oraclegen.PoolFor(probe.Faces[0].ManaCost)
	if why != "" {
		return skip("probe mana: " + why)
	}
	// Control: the probe cast alone; its ability is withheld (summoning
	// sickness) once it resolves and the priority round reaches it.
	control := staticScenario(f, name, nil, []string{hasteProbe}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + hasteProbe, Mana: pool},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offeredNot(0, "activate", "p0:"+hasteProbe)},
	})
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not withhold the sick probe's ability (ok=%v fails=%v)", ok, cres.Fails))
	}
	// Observation: the card on the battlefield lifts the sickness for the
	// activation.
	sc := withBackFace(staticScenario(f, name, []string{name}, []string{hasteProbe}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + hasteProbe, Mana: pool},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offeredYes(0, "activate", "p0:"+hasteProbe)},
	}), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("the sick probe's ability is still withheld with the card (ok=%v fails=%v)", ok, res.Fails))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"302.6"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// powerUpProbe is the creature whose power-up ability the ceiling serves: one
// activated ability, a plain {4}{W} cost with no tap.
const powerUpProbe = "Brave Brawler"

// activationsPowerUpItem serves Activations (Wonder Man, Hollywood Hero): the
// probe's power-up ability is offered once (the control, and the ceiling the
// observation ends on) and a second time with the card on the battlefield.
// The mana op covers the ability's cost; the counters the two activations put
// on the probe are the compared trace.
func activationsPowerUpItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "Activations"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	probe, ok := reg.Lookup(powerUpProbe)
	if !ok || len(probe.Faces) == 0 {
		return skip("probe not in corpus")
	}
	ab := probe.Faces[0].Abilities
	if len(ab) != 1 || ab[0].ParamStr(cards.PKPowerUp) != "True" {
		return skip("probe's only ability is not a PowerUp one")
	}
	pool := "WWWWW"
	one := func(want bool) []oraclegen.Step {
		return []oraclegen.Step{
			{Op: "mana", Seat: 0, Mana: pool},
			{Op: "activate", Seat: 0, Card: "p0:" + powerUpProbe, AbilityIndex: intPtr(0)},
			{Op: "resolve"},
			{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{
				Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + powerUpProbe}, Want: boolPtr(want),
			}}},
		}
	}
	countersOf := func(res rules.OracleResult) int32 {
		p, ok := permByNameIn(res, powerUpProbe)
		if !ok {
			return -1
		}
		return p.Counters["P1P1"]
	}
	// Control: the probe alone; its power-up ability is offered once and the
	// second activation is refused.
	control := staticScenario(f, name, []string{powerUpProbe}, nil, one(false))
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 || countersOf(cres) != 2 {
		return skip(fmt.Sprintf("control does not cap the power-up activation at once (counters %d, ok=%v fails=%v)", countersOf(cres), ok, cres.Fails))
	}
	// Observation: the card's ceiling is 2, so the second activation is
	// offered and the third refused.
	two := append([]oraclegen.Step(nil), one(false)...)
	two[0] = oraclegen.Step{Op: "mana", Seat: 0, Mana: "WWWWWWWWWW"}
	second := []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + powerUpProbe, AbilityIndex: intPtr(0)}, {Op: "resolve"}}
	two = append(two[:3:3], append(second, one(false)[3])...)
	sc := withBackFace(staticScenario(f, name, []string{name, powerUpProbe}, nil, two), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if countersOf(res) != 4 {
		return skip(fmt.Sprintf("the second power-up activation did not happen (counters %d)", countersOf(res)))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"602.5b"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// plotTopProbe is the nonland card on top of p0's library the plot offer
// names; its cost is 0, so the offer never needs pool mana.
const plotTopProbe = "Memnite"

// plotZoneItem serves PlotZone (Fblthp, Lost on the Range): the top card of
// p0's library (given the card's own plot-granting static) is offered as a
// plot cast at p0's first priority; the control without the card offers
// nothing of the kind. The claim rides the snapshot's Offered list, which
// both engines compare.
func plotZoneItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "PlotZone"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	if _, ok := reg.Lookup(plotTopProbe); !ok {
		return skip("plot probe not in corpus")
	}
	offeredPlot := func(res rules.OracleResult) bool {
		if len(res.Snapshots) == 0 {
			return false
		}
		for _, o := range res.Snapshots[len(res.Snapshots)-1].Offered {
			if o.Kind == "cast" && o.Label == "Plot "+plotTopProbe {
				return true
			}
		}
		return false
	}
	topIs := func(res rules.OracleResult, want string) bool {
		if len(res.Snapshots) == 0 || len(res.Snapshots[len(res.Snapshots)-1].Players) == 0 {
			return false
		}
		top := res.Snapshots[len(res.Snapshots)-1].Players[0].LibraryTop
		return len(top) > 0 && top[0] == want
	}
	mk := func(battlefield []string) oraclegen.Scenario {
		sc := staticScenario(f, name, battlefield, nil, []oraclegen.Step{{Op: "pass_to", Seat: 0, Decision: "priority"}})
		p0 := sc.Setup["p0"]
		p0.LibraryTop = []string{plotTopProbe}
		sc.Setup["p0"] = p0
		return sc
	}
	// Control: the card absent, no plot offer for the top card.
	control := mk(nil)
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not replay (ok=%v fails=%v)", ok, cres.Fails))
	}
	if offeredPlot(cres) {
		return skip("control offers the plot without the card")
	}
	if !topIs(cres, plotTopProbe) {
		return skip("the probe is not the library top at the checkpoint")
	}
	// Observation: the card live, the plot cast is offered for the top card.
	sc := withBackFace(mk([]string{name}), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if !offeredPlot(res) {
		return skip("the plot offer is absent with the card on the battlefield")
	}
	if !onBattlefield(res, "p0:"+name) {
		return skip("card is not on the battlefield at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"701.34a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// hexproofProbe is the hexproof creature p0's spell points at: on p1's side,
// where the card's ValidEntity$ Creature.OppCtrl scope reads it.
const hexproofProbe = "Witchstalker"

// hexproofDamageProbe and hexproofHit are the burn spell and the prevention
// probe: the burn alone (targeting p1) proves the cast machinery in both
// runs, then the same burn pointed at the hexproof creature is the claim.
const (
	hexproofDamageProbe = "Shock"
	hexproofBurnProbe   = "Lightning Strike"
	hexproofHit         = "Witchstalker"
)

// ignoreHexproofItem serves IgnoreHexproof (Nowhere to Run): the hexproof
// probe on p1's side is refused as a target without the card (the control's
// replay fails at that cast) and takes the burn with it on the battlefield.
func ignoreHexproofItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "IgnoreHexproof"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	probe, ok := reg.Lookup(hexproofProbe)
	if !ok || len(probe.Faces) == 0 {
		return skip("probe not in corpus")
	}
	if !hasKeywordFace(probe.Faces[0], "Hexproof") {
		return skip("the probe does not carry hexproof")
	}
	// Control: the card absent; the burn at p1 resolves, then the burn at the
	// hexproof creature is refused (the replay errors there).
	control := staticScenario(f, name, nil, []string{hexproofDamageProbe, hexproofBurnProbe}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + hexproofDamageProbe, Mana: "R", Targets: []string{"p1"}},
		{Op: "resolve"},
		{Op: "cast", Seat: 0, Card: "p0:" + hexproofBurnProbe, Mana: "RR", Targets: []string{"p1:" + hexproofProbe}},
	})
	control.Setup["p1"] = oraclegen.Seat{Battlefield: []string{hexproofProbe}}
	if cres, ok := runStatic(reg, control); ok && len(cres.Fails) == 0 {
		return skip("control cast the hexproof probe without the card")
	}
	// Observation: the card live; the hexproof creature is targetable and
	// takes the 2 damage.
	sc := staticScenario(f, name, []string{name}, []string{hexproofDamageProbe, hexproofBurnProbe}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + hexproofDamageProbe, Mana: "R", Targets: []string{"p1"}},
		{Op: "resolve"},
		{Op: "cast", Seat: 0, Card: "p0:" + hexproofBurnProbe, Mana: "RR", Targets: []string{"p1:" + hexproofProbe}},
		{Op: "resolve"},
	})
	sc.Setup["p1"] = oraclegen.Seat{Battlefield: []string{hexproofProbe}}
	sc = withBackFace(sc, name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if onBattlefield(res, "p1:"+hexproofProbe) {
		return skip("the hexproof creature survived the burn it was targeted with")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.11b"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// hasKeywordFace reports whether the face carries the keyword.
func hasKeywordFace(f *cards.Face, kw string) bool {
	for _, k := range f.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

// hasKeywordSnap reports whether a snapshot permanent's keywords include kw.
func hasKeywordSnap(p rules.OracleSnapPerm, kw string) bool {
	for _, k := range p.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

// suspectHost is the creature the suspect designation lands on (or, with the
// card live, never lands on).
const suspectHost = "Giant Spider"

// suspectMaker is the Aura whose enters trigger suspects its enchanted
// creature.
const suspectMaker = "Incriminating Impetus"

// cantBeSuspectedItem serves CantBeSuspected (Airtight Alibi): the suspect
// Aura alone marks the host suspected (the CR 702.157 menace designation the
// snapshot's keywords carry); with the card attached first, the designation
// never lands and the host shows the card's own hexproof grant instead.
func cantBeSuspectedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBeSuspected"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	impetus, ok := reg.Lookup(suspectMaker)
	if !ok || len(impetus.Faces) == 0 {
		return skip("suspect probe not in corpus")
	}
	impMana, why := oraclegen.PoolFor(impetus.Faces[0].ManaCost)
	if why != "" {
		return skip("suspect probe mana: " + why)
	}
	auraMana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return skip("aura mana: " + why)
	}
	keywordsOf := func(res rules.OracleResult) ([]string, bool) {
		p, ok := permByNameIn(res, suspectHost)
		return p.Keywords, ok
	}
	hasKw := func(kws []string, kw string) bool {
		for _, k := range kws {
			if strings.EqualFold(k, kw) {
				return true
			}
		}
		return false
	}
	// Control: the suspect Aura alone; the host becomes suspected (menace).
	control := staticScenario(f, name, []string{suspectHost}, []string{suspectMaker}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + suspectMaker, Mana: impMana, Targets: []string{"p0:" + suspectHost}},
		{Op: "resolve"},
	})
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not replay (ok=%v fails=%v)", ok, cres.Fails))
	}
	kws, ok := keywordsOf(cres)
	if !ok || !hasKw(kws, "Menace") {
		return skip("control does not mark the host suspected")
	}
	// Observation: the card attaches first; the designation is suppressed and
	// the card's own hexproof grant is the live trace.
	sc := staticScenario(f, name, []string{suspectHost}, []string{name, suspectMaker}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: auraMana, Targets: []string{"p0:" + suspectHost}},
		{Op: "resolve"},
		{Op: "cast", Seat: 0, Card: "p0:" + suspectMaker, Mana: impMana, Targets: []string{"p0:" + suspectHost}},
		{Op: "resolve"},
	})
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	kws, ok = keywordsOf(res)
	if !ok || hasKw(kws, "Menace") {
		return skip("the host still shows the suspected designation's menace")
	}
	if !hasKw(kws, "Hexproof") {
		return skip("the card's own grant did not reach the host at the checkpoint")
	}
	if !onBattlefield(res, "p0:"+name) {
		return skip("the card is not attached at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.157"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}
