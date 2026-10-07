// Turned-face-up trigger recipes (Level B, ticket
// cli-20261006T144107Z-fd029c8f). A "When this is turned face up" trigger
// (Forge Mode$ TurnFaceUp, CR 708.6) has one cause a p0-only turn-1 scenario
// can produce: cast a Disguise card face down for {3}, resolve it, then take
// the battlefield turn-face-up special action for its disguise cost. Both
// halves already exist rules-side; this recipe only asks for them.
//
//   - A filter that names the source (Card.Self, including the comma forms
//     Card.Self,Creature.Other+YouCtrl and Card.Self,Permanent.Other+YouCtrl)
//     casts the card itself from hand: the timer's .selfInHand puts it in p0's
//     hand and the cast step chooses the engine's "disguised" offer.
//   - A filter that names another permanent (Permanent, Permanent.YouCtrl,
//     Detective.YouCtrl, ...) leaves the card on the battlefield and turns up
//     a separate Disguise probe, which gorge's own matcher (filterProbe) must
//     accept.
//
// Only the Disguise family is driven: XMage's castSpelling selects a face-down
// cast by the keyword name ("... using Disguise"), and the driver maps only
// that mode. A Morph or Megamorph TurnFaceUp trigger reports a named skip
// until a driver path for those keywords lands.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// faceDownCastPool is the family-independent {3} a Disguise card is cast face
// down for (CR 702.169a). The turn-up cost is the keyword's own parameter.
const faceDownCastPool = "CCC"

// turnUpXRule is the XMage rule text TurnFaceUpAbility renders with the
// cost's toString() prefix (Mage/.../common/TurnFaceUpAbility.java: "Turn
// this face-down permanent face up"); the scenario's activate step selects it.
const turnUpXRule = "Turn this face-down permanent face up."

// turnedFaceUpDisguiseProbes are Disguise cards with no TurnFaceUp trigger of
// their own, tried in order for the other-permanent filter: Bolrac-Clan
// Basher (a plain creature, accepted by any creature/permanent filter), then
// a Vampire Detective and an Elf Crocodile Detective so a Detective filter has
// a candidate. The trigger's own matcher decides, not this list.
var turnedFaceUpDisguiseProbes = []string{
	"Bolrac-Clan Basher", "Basilica Stalker", "Undercover Crocodelf",
}

// turnedFaceUpRecipe builds the causes for the two turned-face-up
// sub-families. ok is false for every other sub-family.
func turnedFaceUpRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	switch sub {
	case levelb.TurnedFaceUpSub:
		return turnedFaceUpSelfCause(f, name)
	case levelb.TurnedFaceUpOtherSub:
		return turnedFaceUpOtherCause(reg, t, name)
	}
	return nil, "", false
}

// turnedFaceUpSelfCause casts the card itself face down and turns it up. why
// is a named skip for a card with no Disguise keyword or one whose disguise
// cost the mana-pool renderer cannot fund.
func turnedFaceUpSelfCause(f *cards.Face, name string) ([]triggerCause, string, bool) {
	raw, pool, why := disguiseCost(f)
	if why != "" {
		return nil, why, true
	}
	steps, xab := turnUpSteps(name, raw, pool)
	return []triggerCause{{
		selfInHand: true,
		hand:       []string{name},
		steps:      steps,
		xability:   xab,
	}}, "", true
}

// turnedFaceUpOtherCause leaves the card on the battlefield and turns up a
// Disguise probe the trigger's own filter accepts. why is a named skip when
// no candidate's real card satisfies the filter.
func turnedFaceUpOtherCause(reg *cards.Registry, t *cards.Trigger, name string) ([]triggerCause, string, bool) {
	probe := turnedFaceUpAcceptedProbe(reg, t.ParamStr(cards.PKValidCard), name)
	if probe == "" {
		return nil, "turned-face-up: no Disguise probe the filter accepts", true
	}
	c, found := reg.Lookup(probe)
	if !found || len(c.Faces) == 0 {
		return nil, "turned-face-up: probe not in corpus", true
	}
	raw, pool, why := disguiseCost(c.Faces[0])
	if why != "" {
		return nil, why, true
	}
	steps, xab := turnUpSteps(probe, raw, pool)
	return []triggerCause{{hand: []string{probe}, steps: steps, xability: xab}}, "", true
}

// turnUpSteps is the shared cause: cast ref face down for {3}, resolve it,
// then take the turn-face-up special action for the disguise cost. The
// activate step is the third, so its XMage rule-text prefix sits at index 2.
func turnUpSteps(ref, raw, pool string) ([]oraclegen.Step, []string) {
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + ref, Mana: faceDownCastPool, CastMode: "disguised"},
		{Op: "resolve"},
		{Op: "activate", Seat: 0, Card: "p0:" + ref, Mana: pool, Ability: "Turn face up"},
	}
	xab := make([]string, len(steps))
	xab[len(steps)-1] = xmageManaText(raw) + ": " + turnUpXRule
	return steps, xab
}

// disguiseCost returns the Disguise keyword's raw Forge cost, the mana pool
// that funds it and a named skip when the face carries no Disguise or the
// renderer cannot fund the cost.
func disguiseCost(f *cards.Face) (raw, pool, why string) {
	raw, ok := f.KeywordParam("Disguise")
	if !ok || strings.TrimSpace(raw) == "" {
		return "", "", "turned-face-up: not a Disguise card"
	}
	pool, gap := oraclegen.PoolFor(raw)
	if gap != "" {
		return "", "", "turned-face-up: disguise cost " + gap
	}
	return raw, pool, ""
}

// turnedFaceUpAcceptedProbe returns the first plain Disguise card the
// trigger's own matcher accepts as the turned-up permanent, or "" when none
// does. The probe is placed face up on the battlefield, which is the state
// the trigger sees when it fires.
func turnedFaceUpAcceptedProbe(reg *cards.Registry, spec, name string) string {
	fp := newFilterProbe(spec, state.ZBattlefield)
	for _, cand := range turnedFaceUpDisguiseProbes {
		if cand == name {
			continue
		}
		card, ok := reg.Lookup(cand)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		if _, ok := card.Faces[0].KeywordParam("Disguise"); !ok {
			continue
		}
		if fp.accepts(card) {
			return cand
		}
	}
	return ""
}

// xmageManaText renders a Forge mana cost ("RW RW", "3", "G") in XMage's
// rule-text braces, including hybrid halves: "RW" is "{R/W}". It mirrors
// poolFor's symbol table, because the two must agree on which cost a step
// pays.
func xmageManaText(cost string) string {
	var b strings.Builder
	for _, sym := range strings.Fields(cost) {
		switch {
		case sym == "0":
			b.WriteString("{0}")
		case strings.Trim(sym, "0123456789") == "":
			b.WriteString("{" + sym + "}")
		case len(sym) == 1:
			b.WriteString("{" + sym + "}")
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			b.WriteString("{" + sym[:1] + "/" + sym[1:] + "}")
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			b.WriteString("{2/" + sym[1:] + "}")
		default:
			b.WriteString("{" + sym + "}")
		}
	}
	return b.String()
}
