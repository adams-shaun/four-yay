// Level-B observation of a has-all-abilities-of static (Forge's
// GainsAbilitiesOf$: "CARDNAME has all activated abilities of ..."). The
// snapshot carries no list of a permanent's abilities, so the grant is
// observed by USING one of the donor card's activated abilities on the
// recipient: the source sits on p0's battlefield, a donor card matching the
// grant's filter sits in the GainsAbilitiesOfZones$ zone (default the
// battlefield), and the source is offered the donor's activation, labelled
// "<source>: <ability text>" (the engine's gained-ability option label,
// rules/legal_walk_battlefield.go). The control removes the donor, so the
// assertion is the grant's doing and not an ability the source already had.
//
// The filter itself is the ENGINE's to decide: the generator only offers
// donor candidates, and a candidate the filter does not select shows no offer
// and is dropped, so a narrower filter keeps its named skip rather than
// serving a wrong row.
package templates

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// donorProbeNames are the candidate donor cards, in preference order: a
// near-vanilla creature whose only activated ability is a no-mana {T} ability
// whose text carries no CARDNAME (Wellwisher), so the offer needs no mana and
// its label is stable across engines. A donor must satisfy the grant's own
// filter (`Creature.YouCtrl` and `Elf.YouOwn` both admit it).
var donorProbeNames = []string{"Wellwisher"}

// donorZoneOrder is the placement order for a grant's
// GainsAbilitiesOfZones$ zones, mirroring the engine's staticSourceZones
// order (rules/layers_static.go) so the two cannot drift.
var donorZoneOrder = []state.Zone{
	state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZHand, state.ZLibrary,
}

// staticDonorItem serves a has-all-abilities-of static requirement, or false
// when the static carries no GainsAbilitiesOf$ or no candidate donor is
// offered.
func staticDonorItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if strings.TrimSpace(st.ParamStr(cards.PKGainsAbilitiesOf)) == "" {
		return oraclegen.Item{}, false
	}
	zones, all, ok := donorZones(st)
	if !ok {
		return oraclegen.Item{}, false
	}
	for _, z := range donorZoneOrder {
		if !all && !slices.Contains(zones, z) {
			continue
		}
		for _, donor := range donorProbeNames {
			c, ok := reg.Lookup(donor)
			if !ok || len(c.Faces) == 0 {
				continue
			}
			desc, ok := donorActivationText(c.Faces[0])
			if !ok {
				continue
			}
			if it, ok := donorItem(reg, f, name, req, donor, z, desc); ok {
				return it, true
			}
		}
	}
	// An exile donor must be exiled by the source's own move: the setup
	// placement above carries no association, so the engine's
	// Card.ExiledWithSource filter excludes it (static_fixture_exile.go).
	if it, ok := staticExileDonorItem(reg, f, name, req, st); ok {
		return it, true
	}
	return oraclegen.Item{}, false
}

// donorZones is the grant's GainsAbilitiesOfZones$ value as engine zone
// values. An empty value defaults to the battlefield -- Forge's
// StaticAbilityContinuous default and the same default gainedFacesForSpec
// takes.
func donorZones(st cards.Static) ([]state.Zone, bool, bool) {
	raw := strings.TrimSpace(st.ParamStr(cards.PKGainsAbilitiesOfZones))
	if raw == "" {
		return []state.Zone{state.ZBattlefield}, false, true
	}
	return effects.ParseZones(raw)
}

// donorActivationText is the first activated, non-mana, plain-{T} ability of
// the donor face with a rule text to assert; ok is false when it has none.
func donorActivationText(f *cards.Face) (string, bool) {
	for _, ab := range f.Abilities {
		if ab == nil || ab.Kind != "AB" || cards.IsManaAbilityAPI(ab.API) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKCost)), "T") {
			continue
		}
		if d := strings.TrimSpace(ab.ParamStr(cards.PKSpellDescription)); d != "" {
			return d, true
		}
	}
	return "", false
}

// donorItem runs one candidate and its control. The observation asserts the
// source is offered the donor ability's label with the donor present; the
// control keeps every card and step except the donor, and the same checkpoint
// must NOT offer it. Only a candidate that passes both is served.
func donorItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, donor string, zone state.Zone, desc string) (oraclegen.Item, bool) {
	label := name + ": " + desc
	sc := donorScenario(f, name, donor, zone, label, true)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	cres, cok := runStatic(reg, donorScenario(f, name, donor, zone, label, false))
	if !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// donorScenario is the checkpoint scenario for one donor placement: the
// source is placed on p0's battlefield (setup-placed, so a {T} cost is not
// summoning-sick), the Bear is the target fodder, and the donor sits in the
// grant's zone -- on the battlefield, or in the graveyard/exile/hand/library
// of p0. The offered assertion is at p0's main-phase priority. want false is
// the control, which omits the donor.
func donorScenario(f *cards.Face, name, donor string, zone state.Zone, label string, want bool) oraclegen.Scenario {
	bf := []string{name, staticProbe}
	if want && zone == state.ZBattlefield {
		bf = append(bf, donor)
	}
	steps := []oraclegen.Step{
		{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{
			Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + name, Label: label},
			Want:    boolPtr(want),
		}}},
	}
	sc := staticScenario(f, name, bf, nil, steps)
	if !want {
		return sc
	}
	p0 := sc.Setup["p0"]
	switch zone {
	case state.ZGraveyard:
		p0.Graveyard = appendFixtureUnique(append([]string(nil), p0.Graveyard...), donor)
	case state.ZExile:
		p0.Exile = appendFixtureUnique(append([]string(nil), p0.Exile...), donor)
	case state.ZHand:
		p0.Hand = appendFixtureUnique(append([]string(nil), p0.Hand...), donor)
	case state.ZLibrary:
		p0.LibraryTop = appendFixtureUnique(append([]string(nil), p0.LibraryTop...), donor)
	}
	sc.Setup["p0"] = p0
	return sc
}
