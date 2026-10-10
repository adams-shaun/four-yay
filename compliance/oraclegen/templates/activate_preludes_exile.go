// Extra activation preludes the level-B activate template needs beyond the
// restriction preludes (activate_restriction.go) and the cost preludes
// (activate_costs.go): a tapped creature a "Return<.../Creature.tapped>" cost
// must return, and (below) the play/craft preludes a back-face or
// reflected-mana requirement needs. Each returns a conditionPrelude the
// serving loop tries only after the bare scenario failed, so an already-served
// row keeps its bytes.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
)

// returnTappedPrelude supplies the prelude that taps the fixture creature a
// "Return<1/Creature.tapped>" activation cost must return. Setup's Tapped
// list is undone by the genesis untap step (p0 is the active player), so the
// catalogue creature the cost fixture places on the battlefield is untapped
// and cannot pay the cost; a real tap spell ({1}{W} Pressure Point) taps it
// before the activation instead. ok is false for a cost without such a part.
func returnTappedPrelude(reg *cards.Registry, cost string) (conditionPrelude, bool) {
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "Return") || returnCreatureFixture(tok) == "" {
			continue
		}
		cast, ok := castProbe(reg, tapSpellProbe, "p0:"+bearsProbe)
		if !ok {
			return conditionPrelude{}, false
		}
		return conditionPrelude{
			hand:  []string{tapSpellProbe},
			steps: []oraclegen.Step{cast, {Op: "resolve"}},
		}, true
	}
	return conditionPrelude{}, false
}

// charmSlotSets lists the candidate target-slot sets for an activated AB$
// Charm whose root names no ValidTgts$: the union of every Choices$ mode's
// slots first, then each mode's slots alone (one legal mode is enough for the
// offer). nil when the ability is not a charm or no mode names a target. The
// engine chooses an activated charm's mode AND target at RESOLUTION
// (rules/cast_asks.go), so these slots only seed the board -- the activate
// step itself carries no targets.
func charmSlotSets(f *cards.Face, sa *cards.SA) [][]oraclegen.Slot {
	if sa == nil || sa.API != "Charm" {
		return nil
	}
	var modes [][]oraclegen.Slot
	var all []oraclegen.Slot
	for _, name := range strings.Split(sa.ParamStr(cards.PKChoices), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slots := oraclegen.ChainSlotSpecs(f, name)
		if len(slots) == 0 {
			continue
		}
		modes = append(modes, slots)
		all = append(all, slots...)
	}
	if len(all) == 0 {
		return nil
	}
	return append([][]oraclegen.Slot{all}, modes...)
}

// manaReflectedPlayPrelude supplies the prelude for a ManaReflected ability
// whose Valid$ is "Defined.ExiledWith" (Pit of Offerings): the source is
// PLAYED so its enters-the-battlefield trigger exiles a card, recording the
// source's forward exile list (the record the reflect scan reads), then the
// land untaps for the {T} ability. The source moves from the battlefield to
// the hand (the auraSourceAttach shape), the play step carries the graveyard
// card the ETB exiles, and two pass_to steps reach the land's untapped turn.
// ok is false for every other ability.
func manaReflectedPlayPrelude(sa *cards.SA, name string, p0 *oraclegen.Seat) ([]oraclegen.Step, bool) {
	if effects.ManaReflectedOf(sa).Valid != "Defined.ExiledWith" {
		return nil, false
	}
	const material = "Grizzly Bears"
	for i, n := range p0.Battlefield {
		if n == name {
			p0.Battlefield = append(p0.Battlefield[:i], p0.Battlefield[i+1:]...)
			break
		}
	}
	p0.Hand = appendFixtureUnique(p0.Hand, name)
	p0.Graveyard = appendFixtureUnique(p0.Graveyard, material)
	return []oraclegen.Step{
		{Op: "play", Seat: 0, Card: "p0:" + name, Targets: []string{"p0:" + material}},
		{Op: "resolve"},
		{Op: "pass_to", Step: "upkeep", Active: "p0"},
		{Op: "pass_to", Step: "main1", Active: "p0"},
	}, true
}

// craftActivatePrelude supplies the prelude for a requirement on a face after
// 0 whose front face has a Craft ability (Sunbird Standard's Sunbird Effigy):
// the front face is placed on the battlefield and its Craft activation exiles
// a material, so the card transforms and the back face enters with the
// ExiledWith set populated. The caller must NOT mark the card as a back-face
// setup. ok is false for a face-0 requirement or a front face with no Craft.
func craftActivatePrelude(reg *cards.Registry, name string, face int, p0 *oraclegen.Seat) (steps []oraclegen.Step, xab []string, cost string, ok bool) {
	if face <= 0 {
		return nil, nil, "", false
	}
	c, found := reg.Lookup(name)
	if !found || len(c.Faces) < 2 || craftAbilityIndex(c.Faces[0]) < 0 {
		return nil, nil, "", false
	}
	steps, xab, materials, cost, ok := craftPrelude(c.Faces[0], name)
	if !ok {
		return nil, nil, "", false
	}
	p0.Graveyard = append(p0.Graveyard, materials...)
	return steps, xab, cost, true
}
