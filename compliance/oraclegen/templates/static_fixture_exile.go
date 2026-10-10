// Fixtures for a static over cards exiled with the source (ticket
// agent-20261009T153027Z-bc3dacf9, level-B class G7 "static effect
// unobservable"). A setup-placed exile carries no exiled-with association, so
// the offer and donor paths could not serve these rows; each builder here runs
// the card's OWN move -- its activated exile, or its cast ETB -- so the engine
// records the association and the static really applies. The classifier reads
// the card's compiled script; the fixture never fakes the association, and a
// row whose move the builder cannot run stays skipped with a named reason
// (staticExileWithGap).
package templates

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// staticExileTypeVictims are near-vanilla cards of distinct card types, in
// type order: a creature, an artifact, a land and an instant. A
// Count$ValidExile ...$CardTypes gate counts the DISTINCT types among the
// cards exiled with the source, so one victim per type is what reaches it.
var staticExileTypeVictims = []string{"Hill Giant", "Sol Ring", "Wastes", "Shock"}

// staticExileDonorNames are the artifact/land donors the exile-donor
// observation tries: each is a near-vanilla permanent whose only activated
// ability is a plain {T} non-mana ability with no target and no condition, so
// the offered option's label is the ability's stable rule text (the same
// contract donorProbeNames states for the battlefield donor).
var staticExileDonorNames = []string{"Braidwood Cup", "Marble Chalice", "Ghoulcaller's Bell"}

// staticExileFixtures is the observed-path family staticFixtures appends
// after the state fixtures: a self static whose own activated ability exiles
// the cards its count reads (Keen-Eyed Curator's "four or more card types
// among cards exiled with CARDNAME").
func staticExileFixtures(reg *cards.Registry, f *cards.Face, st cards.Static, affected string) []staticFixture {
	var out []staticFixture
	if fx, ok := staticExiledCardTypesFixture(f, st, affected); ok {
		out = append(out, fx)
	}
	return out
}

// staticExiledCardTypesFixture is the fixture for a self static gated on the
// number of distinct card TYPES among the cards exiled with the source. It
// places one victim per type in p0's graveyard and runs the source's own
// graveyard-to-exile activated ability once per victim (paying the ability's
// generic cost), so every exile carries the association the gate reads. ok is
// false unless the static is that self count and the face has the ability.
func staticExiledCardTypesFixture(f *cards.Face, st cards.Static, affected string) (staticFixture, bool) {
	if !hasWord(affectedWords(affected), "Self") {
		return staticFixture{}, false
	}
	bodies := strings.ToLower(strings.Join(staticSVarBodies(f, st), " "))
	if !strings.Contains(bodies, "validexile") || !strings.Contains(bodies, "exiledwithsource") || !strings.Contains(bodies, "cardtypes") {
		return staticFixture{}, false
	}
	n := staticCountFrom(st.ParamStr(cards.PKSVarCompare))
	if n < 1 || n > len(staticExileTypeVictims) {
		return staticFixture{}, false
	}
	idx, generic, ok := staticExileAbility(f)
	if !ok {
		return staticFixture{}, false
	}
	victims := staticExileTypeVictims[:n]
	fx := staticFixture{conditionPrelude: conditionPrelude{graveyard: append([]string(nil), victims...)}}
	for _, v := range victims {
		fx.afterSteps = append(fx.afterSteps, oraclegen.Step{
			Op: "activate", Seat: 0, Card: "p0:" + f.Name,
			AbilityIndex: intPtr(idx), Mana: strings.Repeat("C", generic),
			Targets: []string{"p0:" + v},
		})
	}
	// The activations are on the stack; one resolve settles them all before
	// the final checkpoint the observation reads.
	fx.afterSteps = append(fx.afterSteps, oraclegen.Step{Op: "resolve"})
	return fx, true
}

// staticExileAbility finds the face's activated ability that exiles a card
// from a graveyard -- the move every card of this family makes -- and returns
// its IR index and the generic mana its cost charges (the {T}/Blight parts
// are paid by the activation itself). ok is false when no ability has that
// shape.
func staticExileAbility(f *cards.Face) (idx, generic int, ok bool) {
	for i, ab := range f.Abilities {
		if ab == nil || ab.Kind != "AB" || ab.API != "ChangeZone" {
			continue
		}
		p := ab.Params
		if !strings.EqualFold(strings.TrimSpace(p["Origin"]), "Graveyard") ||
			!strings.EqualFold(strings.TrimSpace(p["Destination"]), "Exile") ||
			strings.TrimSpace(p["ValidTgts"]) == "" {
			continue
		}
		return i, staticCostGeneric(p["Cost"]), true
	}
	return 0, 0, false
}

// staticCostGeneric sums the pure-number tokens of a Forge cost string: the
// generic mana the activation charges ("2" is two, "T Blight<2>" is none).
func staticCostGeneric(cost string) int {
	n := 0
	for _, tok := range strings.Fields(cost) {
		if v, err := strconv.Atoi(tok); err == nil {
			n += v
		}
	}
	return n
}

// staticExileBlightCreature is the creature a Blight cost blights in the
// offer fixture: a 6/4 vanilla body that survives up to three -1/-1 counters
// and can carry the raise's CHARGE counters. It is not the probe (the probe
// must stay unblighted, since a -1/-1 counter would pair with its own +1/+1
// counters, CR 704.5q).
const staticExileBlightCreature = "Craw Wurm"

// staticExileActivation is the source's own exile move as the offer fixture
// must run it: the ability index, the generic mana it charges, the cost
// choices it poses, and the creature a Blight cost blights plus the CHARGE
// counters a RemoveAnyCounter raise needs on it.
type staticExileActivation struct {
	idx     int
	generic int
	answers []oraclegen.Answer
	// blight is the creature the ability's Blight cost blights.
	blight string
	// charge is the number of CHARGE counters a RaiseCost
	// RemoveAnyCounter<N/...> needs on a creature the caster controls. The
	// kind is CHARGE, never P1P1: CR 704.5q would remove a +1/+1 counter
	// paired with the Blight's -1/-1 counter, eating the raise's payment.
	charge int
}

// staticExileOfferItem serves a MayPlay static over the caster's OWN cards
// exiled with the source by running the source's own graveyard-to-exile
// ability and asserting the exiled card is offered as a cast. It is the
// offered sibling of staticExiledCardTypesFixture: the same association is
// the engine's, never a setup placement. Only the caster's own cards are
// served -- an opponent-owned exile is not offered by the engine today, and
// such a row keeps its named gap.
func staticExileOfferItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !st.HasParam(cards.PKMayPlay) {
		return oraclegen.Item{}, false
	}
	affected := st.ParamStr(cards.PKAffected)
	if !strings.Contains(affected, "ExiledWithSource") || !strings.Contains(affected, "YouOwn") {
		return oraclegen.Item{}, false
	}
	if st.HasParam(cards.PKMayPlayAltManaCost) {
		// The alt-cost delivery is an engine gap (staticExileWithGap names
		// it); the fixture must not paper over it with a scenario the engine
		// cannot price.
		return oraclegen.Item{}, false
	}
	victim, ok := staticExileFilterVictim(reg, affected)
	if !ok {
		return oraclegen.Item{}, false
	}
	act, ok := staticExileActivationFor(f, st)
	if !ok {
		return oraclegen.Item{}, false
	}
	var tail []oraclegen.Step
	if !st.HasParam(cards.PKMayPlayWithoutManaCost) {
		c, ok := reg.Lookup(victim)
		if !ok || len(c.Faces) == 0 {
			return oraclegen.Item{}, false
		}
		pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
		if why != "" && c.Faces[0].ManaCost != "" && !strings.EqualFold(c.Faces[0].ManaCost, "no cost") {
			return oraclegen.Item{}, false
		}
		if pool != "" {
			tail = []oraclegen.Step{{Op: "mana", Seat: 0, Mana: pool}}
		}
	}
	return staticExileOfferScenario(reg, f, name, req, victim, act, tail)
}

// staticExileActivationFor classifies the face's own exile ability's cost:
// the Blight<N> it charges (which needs a surviving blight creature and the
// choose answer that elects it) and the RemoveAnyCounter<N/...> surcharge the
// MayPlay static's RaiseCost$ adds (which needs N CHARGE counters on a
// creature the caster controls). ok is false on a shape the fixture cannot
// run, including an announced Blight<X> and a Blight deeper than the blight
// creature survives.
func staticExileActivationFor(f *cards.Face, st cards.Static) (staticExileActivation, bool) {
	idx, generic, ok := staticExileAbility(f)
	if !ok {
		return staticExileActivation{}, false
	}
	act := staticExileActivation{idx: idx, generic: generic}
	tokens := strings.Fields(f.Abilities[idx].Params["Cost"])
	tokens = append(tokens, strings.Fields(st.Params["RaiseCost"])...)
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "Blight<"):
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(tok, "Blight<"), ">"))
			if err != nil || n < 1 || n > 3 {
				return staticExileActivation{}, false
			}
			act.blight = staticExileBlightCreature
			act.answers = append(act.answers, oraclegen.Answer{Kind: "choose", Pick: []string{act.blight}})
		case strings.HasPrefix(tok, "RemoveAnyCounter<"):
			arg := strings.TrimSuffix(strings.TrimPrefix(tok, "RemoveAnyCounter<"), ">")
			if i := strings.IndexByte(arg, '/'); i >= 0 {
				arg = arg[:i]
			}
			n, err := strconv.Atoi(arg)
			if err != nil || n < 1 {
				return staticExileActivation{}, false
			}
			act.charge = n
		}
	}
	return act, true
}

// staticExileOfferScenario builds and replays one offer candidate: the source
// and the probe on p0's battlefield, the victim in p0's graveyard, the
// source's own exile activation (with `tail` steps before the assertion), and
// the offered-cast assertion at p0's priority. The control keeps everything
// but the source, so the offer is proven to be the permission's doing.
func staticExileOfferScenario(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, victim string, act staticExileActivation, tail []oraclegen.Step) (oraclegen.Item, bool) {
	want := true
	steps := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: "p0:" + name, AbilityIndex: intPtr(act.idx),
			Mana: strings.Repeat("C", act.generic), Targets: []string{"p0:" + victim}, Answers: act.answers},
		{Op: "resolve"},
	}
	steps = append(steps, tail...)
	steps = append(steps, oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority",
		Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:" + victim}, Want: &want}}})
	bf := []string{name, staticProbe}
	if act.blight != "" {
		bf = append(bf, act.blight)
	}
	sc := staticScenario(f, name, bf, nil, steps)
	p0 := sc.Setup["p0"]
	p0.Graveyard = appendFixtureUnique(p0.Graveyard, victim)
	if act.charge > 0 {
		holder := staticProbe
		if act.blight != "" {
			holder = act.blight
		}
		p0 = oraclegen.WithCounters(p0, holder, "CHARGE", int32(act.charge))
	}
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	no := false
	control := staticScenario(f, name, []string{staticProbe}, nil, []oraclegen.Step{
		{Op: "pass_to", Seat: 0, Decision: "priority",
			Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:" + victim}, Want: &no}}},
	})
	c0 := control.Setup["p0"]
	c0.Graveyard = appendFixtureUnique(c0.Graveyard, victim)
	control.Setup["p0"] = c0
	if cres, cok := runStatic(reg, control); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"601.3"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// staticExileFilterVictim picks the graveyard victim the static's Affected
// filter selects: the probe table's card for the filter's first creature
// word (a Dinosaur victim for Dinosaur.ExiledWithSource+YouOwn), else a plain
// creature for a bare Creature word. ok is false when the filter names no
// type the table can stand in for.
func staticExileFilterVictim(reg *cards.Registry, affected string) (string, bool) {
	words := affectedWords(affected)
	for _, w := range words {
		for _, row := range staticProbeTable {
			if row.word != w {
				continue
			}
			if c, ok := reg.Lookup(row.card); ok && len(c.Faces) > 0 && c.Faces[0].IsCreature() {
				return row.card, true
			}
			return "", false
		}
	}
	if hasWord(words, "Creature") {
		return "Hill Giant", true
	}
	return "", false
}

// staticExileDonorItem serves a has-all-abilities-of static whose donor sits
// in exile (GainsAbilitiesOf=Card.ExiledWithSource, GainsAbilitiesOfZones=
// Exile): the donor is cast along with the source, so the source's own ETB
// exiles it with the association, and the source is offered the donor's
// activation labelled "<source>: <ability text>". The setup-placed donor the
// ordinary donor path offers carries no association, which is why this
// variant exists.
func staticExileDonorItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.Contains(st.ParamStr(cards.PKGainsAbilitiesOf), "ExiledWithSource") {
		return oraclegen.Item{}, false
	}
	zones, all, ok := donorZones(st)
	if !ok || (!all && !containsZone(zones, state.ZExile)) {
		return oraclegen.Item{}, false
	}
	for _, donor := range staticExileDonorNames {
		c, ok := reg.Lookup(donor)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		desc, ok := donorActivationText(c.Faces[0])
		if !ok {
			continue
		}
		if it, ok := staticExileDonorScenario(reg, f, name, req, donor, desc); ok {
			return it, true
		}
	}
	return oraclegen.Item{}, false
}

// staticExileDonorScenario replays one donor candidate: the source is cast
// with the donor on p0's battlefield, its ETB exiles the donor, and the
// source is offered the donor's activation. The control drops the donor, so
// the ETB has no target and no exile carries the association.
func staticExileDonorScenario(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, donor, desc string) (oraclegen.Item, bool) {
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Item{}, false
	}
	label := name + ": " + desc
	want := true
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority",
			Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + name, Label: label}, Want: &want}}},
	}
	sc := staticScenario(f, name, []string{donor}, []string{name}, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	no := false
	control := staticScenario(f, name, nil, []string{name}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority",
			Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + name, Label: label}, Want: &no}}},
	})
	if cres, cok := runStatic(reg, control); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// containsZone reports whether z is in zones.
func containsZone(zones []state.Zone, z state.Zone) bool {
	for _, x := range zones {
		if x == z {
			return true
		}
	}
	return false
}

// staticExileWithGap names why a static over cards exiled with the source no
// builder made observable, when the cause is a measured engine gap rather
// than a missing fixture. "" keeps the existing condition-gap classification.
func staticExileWithGap(f *cards.Face, st cards.Static) string {
	all := strings.ToLower(strings.Join(faceSVarBodies(f), " "))
	switch {
	case strings.Contains(strings.ToLower(st.ParamStr(cards.PKGainsAbilitiesOfDefined)), "exiledwith"):
		// The Enigma Jewel's Locus back face: the exiled-with cards are the
		// craft materials, and the driver has no craft op.
		return "needs the craft activation (the driver has no craft op)"
	case strings.Contains(all, "exiledwith$amount"):
		// Veteran Survivor: the ExiledWith$Amount count reads only the
		// reverse scalar, which this source's own exile never stamps.
		return "the ExiledWith$Amount count reads only the reverse exiled-with stamp (engine gap)"
	case st.HasParam(cards.PKMayPlay) && strings.Contains(st.ParamStr(cards.PKAffected), "ExiledWithSource"):
		switch {
		case st.HasParam(cards.PKMayPlayAltManaCost):
			// Valgavoth, Terror Eater: mayPlayStatic withholds the zone
			// permission from a cost-carrying static, and mayPlayAltCosts
			// delivers the alternative only on the ordinary hand cast.
			return "a MayPlayAltManaCost$ card in exile is not offered (the engine delivers the alternative cost only on a hand cast)"
		case strings.Contains(all, "db$ dig"):
			// Maralen, Fae Ascendant: the trigger's Dig exile goes through
			// moveZoneEvent without the exiled-with association.
			return "cards exiled by the source's Dig carry no exiled-with association (engine gap)"
		case !strings.Contains(st.ParamStr(cards.PKAffected), "YouOwn"):
			// Azula, Cunning Usurper / Null Summoner: the exiled card is
			// owned by the opponent, and the may-play spell walk covers only
			// the caster's own cards.
			return "an opponent-owned exiled card is not offered (the engine's may-play walk covers only the caster's own cards)"
		}
	}
	return ""
}

// faceSVarBodies is every SVar body on the face, lowercased and in sorted
// key order (map order never reaches the classifier's boolean result): the
// exile-move classifier needs the trigger's body (a Dig) even when the
// static's own params reach no SVar.
func faceSVarBodies(f *cards.Face) []string {
	keys := make([]string, 0, len(f.SVars))
	for k := range f.SVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, strings.ToLower(f.SVars[k]))
	}
	return out
}
