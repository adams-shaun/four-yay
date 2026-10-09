// Turned-face-up trigger recipes (Level B, ticket
// cli-20261006T144107Z-fd029c8f). A "When this is turned face up" trigger
// (Forge Mode$ TurnFaceUp, CR 708.6) has one cause a p0-only turn-1 scenario
// can produce: cast a morph-family card face down for {3}, resolve it, then
// take the battlefield turn-face-up special action for its family cost. Both
// halves already exist rules-side; this recipe only asks for them.
//
//   - A filter that names the source (Card.Self, including the comma forms
//     Card.Self,Creature.Other+YouCtrl and Card.Self,Permanent.Other+YouCtrl)
//     casts the card itself from hand: the timer's .selfInHand puts it in p0's
//     hand and the cast step chooses the family's face-down cast offer.
//   - A filter that names another permanent (Permanent, Permanent.YouCtrl,
//     Detective.YouCtrl, ...) leaves the card on the battlefield and turns up
//     a separate Disguise probe, which gorge's own matcher (filterProbe) must
//     accept.
//
// All three morph families are driven (Morph, Megamorph, Disguise): the engine
// turns each up with one turn_face_up special action priced from the printed
// keyword head, and XMage spells both Morph and Megamorph casts "<card> using
// Morph" while Disguise is "<card> using Disguise". Megamorph's turn-up
// ability rule text carries an extra counter clause XMage requires the
// activated-ability selector to prefix-match.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// faceDownCastPool is the family-independent {3} a Morph, Megamorph or
// Disguise card is cast face down for (CR 702.37a/702.168a/702.169a). The
// turn-up cost is the printed keyword head's own parameter.
const faceDownCastPool = "CCC"

// turnUpXRule is the XMage rule text TurnFaceUpAbility renders with the
// cost's toString() prefix (Mage/.../common/TurnFaceUpAbility.java: "Turn
// this face-down permanent face up"); the scenario's activate step selects it.
// A Megamorph permanent's ability appends the +1/+1 counter rider, and XMage
// matches an activation by a true prefix of that rendered text, so the "."
// after "face up" would stop the short form matching (MegamorphTest activates
// by the shorter "{5}{G}: Turn").
const (
	turnUpXRule     = "Turn this face-down permanent face up."
	turnUpXRuleMega = "Turn this face-down permanent face up and put a +1/+1 counter on it."
)

// morphFamily is one printed morph family: the keyword head that names its
// cost parameter, the cast mode the driver maps to XMage's cast spelling, and
// whether the turn-up rider adds a +1/+1 counter.
type morphFamily struct {
	head      string
	castMode  string
	megamorph bool
}

// morphFamilies is the family search order, matching the engine's
// morphDownFamily (rules/cast_altcost.go): Morph, then Megamorph, then
// Disguise. A card prints at most one family head, so the order only fixes
// which one a (hypothetical) multi-head card would pick.
var morphFamilies = []morphFamily{
	{head: "Morph", castMode: "morphed"},
	{head: "Megamorph", castMode: "megamorphed", megamorph: true},
	{head: "Disguise", castMode: "disguised"},
}

// morphFamilyCost returns the printed family's raw Forge cost, the mana pool
// that funds it, the family's cast mode and whether the turn-up adds a
// counter. why is a named skip when the face carries no morph family or the
// mana-pool renderer cannot fund the head's cost.
func morphFamilyCost(f *cards.Face) (raw, pool, mode string, megamorph bool, why string) {
	for _, fam := range morphFamilies {
		raw, ok := f.KeywordParam(fam.head)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		pool, gap := oraclegen.PoolFor(raw)
		if gap != "" {
			return "", "", "", false, "turned-face-up: " + strings.ToLower(fam.head) + " cost " + gap
		}
		return raw, pool, fam.castMode, fam.megamorph, ""
	}
	return "", "", "", false, "turned-face-up: not a morph-family card"
}

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
		if _, _, _, _, why := morphFamilyCost(f); why == "" {
			return turnedFaceUpSelfCause(f, name)
		}
		// The card carries no morph family (Cryptid Inspector's combined
		// "CARDNAME or another permanent you control is turned face up").
		// When its filter also accepts another permanent, turn up a probe;
		// otherwise report the self recipe's own "not a morph-family card".
		if causes, why, ok := turnedFaceUpOtherCause(reg, t, name); ok && why == "" {
			return causes, "", true
		}
		return turnedFaceUpSelfCause(f, name)
	case levelb.TurnedFaceUpOtherSub:
		return turnedFaceUpOtherCause(reg, t, name)
	}
	return nil, "", false
}

// turnedFaceUpSelfCause casts the card itself face down and turns it up. why
// is a named skip for a card with no morph family or one whose family cost
// the mana-pool renderer cannot fund.
func turnedFaceUpSelfCause(f *cards.Face, name string) ([]triggerCause, string, bool) {
	raw, pool, mode, megamorph, why := morphFamilyCost(f)
	if why != "" {
		return nil, why, true
	}
	steps, xab := turnUpSteps(name, raw, pool, mode, megamorph)
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
	raw, pool, mode, megamorph, why := morphFamilyCost(c.Faces[0])
	if why != "" {
		return nil, why, true
	}
	steps, xab := turnUpSteps(probe, raw, pool, mode, megamorph)
	return []triggerCause{{hand: []string{probe}, steps: steps, xability: xab}}, "", true
}

// turnUpSteps is the shared cause: cast ref face down for {3}, resolve it,
// then take the turn-face-up special action for the family head's cost. mode
// is the cast mode the family names ("morphed"/"megamorphed"/"disguised");
// megamorph appends the +1/+1 counter rider to the ability's XMage selector.
// The activate step is the third, so its XMage rule-text prefix sits at
// index 2. The gorge Ability label is family-independent: the engine offers
// every family's turn-up as one "Turn face up (<cost>)" special action.
func turnUpSteps(ref, raw, pool, mode string, megamorph bool) ([]oraclegen.Step, []string) {
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + ref, Mana: faceDownCastPool, CastMode: mode},
		{Op: "resolve"},
		{Op: "activate", Seat: 0, Card: "p0:" + ref, Mana: pool, Ability: "Turn face up"},
	}
	rule := turnUpXRule
	if megamorph {
		rule = turnUpXRuleMega
	}
	xab := make([]string, len(steps))
	xab[len(steps)-1] = xmageManaText(raw) + ": " + rule
	return steps, xab
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
