// Level-B observation of a continuous static that acts on cards OUTSIDE the
// battlefield: a play permission (MayPlay$ from the library top, the
// graveyard, exile or the hand) or a granted Flashback. Nothing on the
// battlefield changes, so staticObserved can never see it; the effect shows
// as a card now being offered as a cast/play option, the positive form of the
// "offered" observation the cannot-statics use (static_legality.go).
//
// The scenario puts the static's card on p0's battlefield, a probe card in the
// permitted zone, enough mana to pay the probe, and asserts the probe is
// offered at p0's first priority. A candidate is served only when
//
//   - gorge offers the probe with the source present, and
//   - gorge does NOT offer it at the identical checkpoint with the source
//     removed (the control), so the assertion is the static's doing and not a
//     card that is castable anyway (a hand card with mana in a main phase).
//
// A static on the card itself ("Card.Self" from the graveyard) has no source
// to remove: its probe is the card, placed in the graveyard, where no rule
// offers a cast without the permission.
package templates

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticLookAtReason is the named skip for a MayLookAt$ static: looking at a
// card is hidden information neither engine's snapshot carries.
const staticLookAtReason = "look-at not observable"

// staticOffBattlefieldGrantGap names the skip for an AddAbility$ grant whose
// static sits off the battlefield (the Surveyor cycle's graveyard "Max speed
// -- {3}, Exile this card from your graveyard: Draw a card."). The engine
// offers no activation for such a grant: collectAddAbilityCarriers is read
// only by the mana-ability loops (rules/mana_activation.go), so a graveyard
// AddAbility$ never reaches the offered-option list -- a graveyard activation
// is not an "activate" option even at max speed (measured: with the Surveyor
// in p0's graveyard and Speed 4, the pending options carry no activate). The
// grant is named separately from staticConditionGap's MaxSpeed reason so the
// census tells an engine gap apart from a setup gap. A BATTLEFIELD grant stays
// with the ordinary observations: the engine does offer it (a max-speed
// "{2}: Draw a card" on a permanent), so it is not this skip.
func staticOffBattlefieldGrantGap(st cards.Static) string {
	if st.ParamStr(cards.PKAddAbility) == "" {
		return ""
	}
	z := strings.TrimSpace(st.ParamStr(cards.PKEffectZone))
	if z == "" || strings.EqualFold(z, "Battlefield") {
		return ""
	}
	return "granted ability in " + z + " is not offered by the engine"
}

// offerProbeNames are the candidate probe cards, tried in this order. A static
// whose Affected$ filter the first probe does not satisfy is retried with the
// next, so the probe that serves a row is the first card gorge offers (and
// offers only because of the static).
var offerProbeNames = []string{
	"Shock", "Llanowar Elves", "Ornithopter", "Forest", "Gigantosaurus",
	"Hill Giant", "Giant Spider", "Shivan Dragon", "Ondu Cleric",
	"Boomerang Basics",
}

// The words the offer scenarios compare against: option kinds, the keywords
// that reduce a cost, and the AffectedZone$ value naming every zone.
const (
	offerKindCast    = "cast"
	offerKwConvoke   = "Convoke"
	offerKwAffinity  = "Affinity"
	offerKwImprovise = "Improvise"
	offerKwDelve     = "Delve"
	offerArgCreature = "Creature"
	offerArgArtifact = "Artifact"
	offerAllZones    = "All"
)

// offerZone is a zone a probe is placed in: the setup field it fills.
type offerZone string

const (
	offerGraveyard offerZone = "Graveyard"
	offerLibrary   offerZone = "Library"
	offerExile     offerZone = "Exile"
	offerHand      offerZone = "Hand"
)

// offerZoneOrder is the order a static's zones are tried in.
var offerZoneOrder = []offerZone{offerGraveyard, offerLibrary, offerExile, offerHand}

// offerTry is one candidate scenario: the probe in the zone and the mana that
// pays it. self marks a static on the probe itself (no source to remove).
type offerTry struct {
	probe string
	zone  offerZone
	mana  string
	kind  string
	// label narrows the offered option to one the label names (Plot is
	// offered as a "cast" option labelled "Plot <card>").
	label string
	self  bool
	// prelude runs at main phase before the mana is added (a creature dying
	// for "if a creature died this turn"); extraHand is the cards it casts.
	prelude   []oraclegen.Step
	extraHand []string
	// extraBF and extraGY are p0 cards a cost-reduction grant needs to
	// reduce with (an artifact to affinity or improvise, a graveyard card to
	// delve): the reduction is exactly one short of what mana pays for.
	extraBF, extraGY []string
	// firstPlay is a land p0 plays before the assertion (the extra land drop
	// is observed on the SECOND land); it is also in extraHand.
	firstPlay string
	// resolveFirstPlay resolves whatever the first land's entry put on the
	// stack before the second land is asserted: a source whose own trigger
	// fires on a land entering (Thranduil's Company's Landfall) otherwise
	// holds the stack, and the offer checkpoint is not at sorcery speed.
	resolveFirstPlay bool
	// attachProbe re-attaches an Aura source to the probe after its cast
	// (the fixture's cast target is an opposing permanent): a granted ability
	// on an EnchantedBy recipient is observed on the probe, so the Aura must
	// end up attached to it.
	attachProbe bool
	// probeLoyalty puts this many LOYALTY counters on the probe at setup, so
	// a granted loyalty ability whose cost exceeds the probe's printed
	// starting loyalty (Avatar of Burgeoning Echoes' [-10]) is still payable.
	probeLoyalty int32
}

// staticOfferItem serves a play-permission or granted-Flashback static as an
// offered observation. ok is false when the static has neither shape or no
// candidate satisfies both the observation and its control. Each candidate is
// tried first as the card CAST in the level-A way (the permanent really
// arrives through the stack, as in play), then with the card placed on the
// battlefield by setup, for a card the scenario has no cast for.
func staticOfferItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	tries := offerTries(reg, f, name, st)
	if len(tries) == 0 {
		return oraclegen.Item{}, false
	}
	var cast *oraclegen.Item
	if base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil); why == "" {
		cast = &base
	}
	for _, t := range tries {
		bases := []*oraclegen.Item{cast, nil}
		if t.self {
			// the probe is the card, already in the graveyard: nothing to cast
			bases = []*oraclegen.Item{nil}
		}
		for _, base := range bases {
			if it, ok := offerItem(reg, f, name, req, t, base); ok {
				return it, true
			}
		}
	}
	return oraclegen.Item{}, false
}

// offerTries lists the candidate scenarios for one static, in preference order.
func offerTries(reg *cards.Registry, f *cards.Face, name string, st cards.Static) []offerTry {
	affected := st.ParamStr(cards.PKAffected)
	zones := offerZones(st.ParamStr(cards.PKAffectedZone))
	self := strings.Contains(affected, "Card.Self")
	var tries []offerTry
	switch {
	case grantOfferable(f, st):
		tries = grantTries(reg, f, st)
	case st.HasParam(cards.PKMayPlay):
		// An alternative cost the scenario cannot pay (exile from the card's
		// own zone) is not an observation of the permission; collect evidence
		// is paid from graveyard cards the setup places.
		var evidence []string
		if st.HasParam(cards.PKMayPlayAltManaCost) {
			var ok bool
			if evidence, ok = collectEvidenceCards(reg, st.ParamStr(cards.PKMayPlayAltManaCost)); !ok {
				return nil
			}
		}
		free := strings.EqualFold(st.ParamStr(cards.PKMayPlayWithoutManaCost), "True") || evidence != nil
		var died []oraclegen.Step
		var diedHand []string
		if staticNeedsCreatureDeath(f, st) {
			died, diedHand = creatureDeathPrelude(reg)
			if died == nil {
				return nil
			}
		}
		probes := offerProbeNames
		if self {
			probes = []string{name}
		}
		for _, z := range zones {
			for _, p := range probes {
				if t, ok := offerTryFor(reg, p, z, free, self); ok {
					t.extraGY, t.prelude, t.extraHand = evidence, died, diedHand
					tries = append(tries, t)
				}
			}
		}
	case strings.HasPrefix(st.ParamStr(cards.PKAddKeyword), "Flashback") && !self:
		// Flashback from the graveyard: the probe's own cost, or the fixed
		// cost the keyword names ("Flashback:1").
		fixed := ""
		if cost, ok := strings.CutPrefix(st.ParamStr(cards.PKAddKeyword), "Flashback:"); ok {
			fixed = cost
		}
		for _, p := range offerProbeNames {
			t, ok := offerTryFor(reg, p, offerGraveyard, false, false)
			if !ok || t.kind != offerKindCast {
				continue
			}
			if fixed != "" {
				pool, why := oraclegen.PoolFor(fixed)
				if why != "" {
					return nil
				}
				t.mana = pool
			}
			tries = append(tries, t)
		}
	case strings.HasPrefix(st.ParamStr(cards.PKAddKeyword), "Plot"):
		// Plot is a special action offered from the zone the card sits in,
		// for the card's own mana cost.
		for _, z := range zones {
			for _, p := range offerProbeNames {
				if t, ok := offerTryFor(reg, p, z, false, false); ok && t.kind == offerKindCast {
					t.label = "Plot " + p
					tries = append(tries, t)
				}
			}
		}
	case st.HasParam(cards.PKAddKeyword):
		tries = costGrantTries(reg, f, name, st.ParamStr(cards.PKAddKeyword))
	}
	return tries
}

// costGrantTries are the candidates for a keyword that lets a spell be cast
// for less (Convoke, Affinity for artifacts or creatures, Improvise, Delve):
// a spell in hand with generic mana in its cost, p0 holding exactly the
// permanents (or graveyard cards) that pay for part of it, and a pool short by
// that many mana. Only the static makes the spell affordable, which the
// control (source removed) proves.
func costGrantTries(reg *cards.Registry, f *cards.Face, name, keyword string) []offerTry {
	kw, arg, _ := strings.Cut(keyword, ":")
	var extraBF, extraGY []string
	var counts func(*cards.Face) bool
	switch {
	case kw == offerKwConvoke:
		counts = func(pf *cards.Face) bool { return pf.IsCreature() }
	case kw == offerKwAffinity && arg == offerArgCreature:
		counts = func(pf *cards.Face) bool { return pf.IsCreature() }
	case kw == offerKwAffinity && arg == offerArgArtifact, kw == offerKwImprovise:
		counts = func(pf *cards.Face) bool { return oraclegen.HasType(pf, "Artifact") }
		extraBF = []string{"Ornithopter"}
	case kw == offerKwDelve:
		extraGY = []string{"Forest"}
	default:
		return nil
	}
	reduction := len(extraGY)
	if counts != nil {
		for _, n := range append([]string{name, staticProbe}, extraBF...) {
			if c, ok := reg.Lookup(n); ok && len(c.Faces) > 0 && counts(c.Faces[0]) {
				reduction++
			}
		}
	}
	var tries []offerTry
	for _, probe := range []string{"Hill Giant", "Lava Axe"} {
		c, ok := reg.Lookup(probe)
		if !ok || len(c.Faces) == 0 || reduction == 0 {
			continue
		}
		pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
		if why != "" {
			continue
		}
		if generic := strings.Count(pool, "C"); reduction > generic {
			reduction = generic
		}
		pool = strings.Replace(pool, "C", "", reduction)
		tries = append(tries, offerTry{probe: probe, zone: offerHand, mana: pool, kind: "cast", extraBF: extraBF, extraGY: extraGY})
	}
	return tries
}

// offerZones is the zones an AffectedZone$ value names, in offerZoneOrder; an
// "All" value names every one. A zone the scenario cannot place a probe in
// (Command, Stack, Battlefield) is dropped.
func offerZones(value string) []offerZone {
	var out []offerZone
	for _, z := range offerZoneOrder {
		if strings.EqualFold(value, offerAllZones) || strings.Contains(","+value+",", ","+string(z)+",") {
			out = append(out, z)
		}
	}
	return out
}

// offerTryFor is the candidate that places probe in zone and pays its printed
// cost, or nothing when the probe is not a corpus card gorge can pay for.
func offerTryFor(reg *cards.Registry, probe string, zone offerZone, free, self bool) (offerTry, bool) {
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return offerTry{}, false
	}
	pf := c.Faces[0]
	t := offerTry{probe: probe, zone: zone, kind: "cast", self: self}
	if oraclegen.HasType(pf, "Land") {
		t.kind = "play"
		return t, true
	}
	if !free {
		pool, why := oraclegen.PoolFor(pf.ManaCost)
		if why != "" && pf.ManaCost != "" && !strings.EqualFold(pf.ManaCost, "no cost") {
			return offerTry{}, false
		}
		t.mana = pool
	}
	return t, true
}

// offerScenario is the checkpoint scenario for one candidate. With a cast base
// the source arrives through the base's own steps (it is in hand until then);
// without one the source is placed on the battlefield by setup. Either way p0
// has a Grizzly Bears, the probe sits in its zone, and the mana is added at
// main phase -- after the pass to main1, because a floating pool empties
// between steps -- before the offered assertion at p0's priority. A control
// (castControl) drops the source: its cast steps and its hand card.
func offerScenario(f *cards.Face, name string, t offerTry, want bool, base *oraclegen.Item, control bool) oraclegen.Scenario {
	expect := []oraclegen.Expect{
		{Offered: &oraclegen.Offered{Seat: 0, Kind: t.kind, Card: "p0:" + t.probe, Label: t.label}, Want: boolPtr(want)},
	}
	var tail []oraclegen.Step
	if t.firstPlay != "" {
		tail = append(tail, oraclegen.Step{Op: "play", Seat: 0, Card: "p0:" + t.firstPlay})
		if t.resolveFirstPlay {
			tail = append(tail, oraclegen.Step{Op: "resolve"})
		}
	}
	if t.mana != "" {
		tail = append(tail, oraclegen.Step{Op: "mana", Seat: 0, Mana: t.mana})
	}
	tail = append(tail, oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: expect})
	toMain := oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"}
	var sc oraclegen.Scenario
	switch {
	case base != nil && !control:
		sc = base.Scenario
		steps := append([]oraclegen.Step(nil), base.Steps...)
		if t.attachProbe {
			steps = append(steps, oraclegen.Step{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + t.probe})
		}
		sc.Steps = append(steps, tail...)
	case base != nil:
		sc = base.Scenario
		sc.Steps = append([]oraclegen.Step{toMain}, tail...)
	default:
		bf := []string{staticProbe}
		if !t.self && !control {
			bf = append([]string{name}, bf...)
		}
		steps := []oraclegen.Step{toMain}
		if !(t.self && control) {
			steps = append(steps, t.prelude...)
		}
		sc = staticScenario(f, name, bf, nil, append(steps, tail...))
	}
	setup := make(map[string]oraclegen.Seat, len(sc.Setup))
	for k, v := range sc.Setup {
		setup[k] = v
	}
	p0 := setup["p0"]
	if control && base != nil && !t.self {
		p0.Hand = removeFixtureOnce(p0.Hand, name)
	}
	switch t.zone {
	case offerGraveyard:
		p0.Graveyard = appendFixtureUnique(append([]string(nil), p0.Graveyard...), t.probe)
	case offerLibrary:
		p0.LibraryTop = appendFixtureUnique(append([]string(nil), p0.LibraryTop...), t.probe)
	case offerExile:
		p0.Exile = appendFixtureUnique(append([]string(nil), p0.Exile...), t.probe)
	case offerHand:
		p0.Hand = appendFixtureUnique(append([]string(nil), p0.Hand...), t.probe)
	}
	for _, n := range t.extraHand {
		p0.Hand = appendFixtureUnique(append([]string(nil), p0.Hand...), n)
	}
	for _, n := range t.extraBF {
		p0.Battlefield = appendFixtureUnique(append([]string(nil), p0.Battlefield...), n)
	}
	for _, n := range t.extraGY {
		p0.Graveyard = appendFixtureUnique(append([]string(nil), p0.Graveyard...), n)
	}
	if t.probeLoyalty > 0 {
		p0 = oraclegen.WithCounters(p0, t.probe, "LOYALTY", t.probeLoyalty)
	}
	setup["p0"] = p0
	sc.Setup = setup
	return sc
}

// offerItem replays one candidate and its control; the item is the served
// level-B row, identified by the requirement key like every other static item.
func offerItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, t offerTry, base *oraclegen.Item) (oraclegen.Item, bool) {
	sc := offerScenario(f, name, t, true, base, false)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	if !t.self {
		// The same checkpoint without the source must NOT offer the probe.
		if cres, cok := runStatic(reg, offerScenario(f, name, t, false, base, true)); !cok || len(cres.Fails) != 0 {
			return oraclegen.Item{}, false
		}
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"601.3"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// removeFixtureOnce returns xs without its first name.
func removeFixtureOnce(xs []string, name string) []string {
	var out []string
	removed := false
	for _, x := range xs {
		if x == name && !removed {
			removed = true
			continue
		}
		out = append(out, x)
	}
	return out
}

// collectEvidenceCards is the graveyard cards that pay "CollectEvidence<N>":
// corpus cards whose mana values total at least N.
func collectEvidenceCards(reg *cards.Registry, alt string) ([]string, bool) {
	arg, ok := strings.CutPrefix(strings.TrimSpace(alt), "CollectEvidence<")
	if !ok {
		return nil, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(arg, ">"))
	if err != nil || n <= 0 {
		return nil, false
	}
	var out []string
	total := 0
	for _, name := range []string{"Shivan Dragon", "Hill Giant", "Giant Spider", "Hill Giant"} {
		if total >= n {
			break
		}
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			return nil, false
		}
		pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
		if why != "" || slices.Contains(out, name) {
			continue
		}
		out = append(out, name)
		total += len(pool)
	}
	return out, total >= n
}

// staticNeedsCreatureDeath reports whether the static's "as long as" count is
// the creatures that went from the battlefield to the graveyard this turn.
func staticNeedsCreatureDeath(f *cards.Face, st cards.Static) bool {
	for _, body := range staticSVarBodies(f, st) {
		if strings.Contains(strings.ToLower(body), "thisturnentered_graveyard_from_battlefield") {
			return true
		}
	}
	return false
}

// creatureDeathPrelude kills the fixture creature with a Shock held for the
// purpose: the steps run at main phase, then the Shock is in hand.
func creatureDeathPrelude(reg *cards.Registry) ([]oraclegen.Step, []string) {
	if _, ok := reg.Lookup("Shock"); !ok {
		return nil, nil
	}
	return []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p0:" + staticProbe}},
		{Op: "resolve"},
	}, []string{"Shock"}
}
