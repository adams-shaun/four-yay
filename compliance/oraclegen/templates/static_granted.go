// Level-B observation of a continuous static that grants an ABILITY rather
// than P/T or a keyword (ticket levelb-static-granted-abilities). The
// snapshot carries no list of a permanent's abilities, so the effect is
// observed by USING it, as an offered option (the positive form of the
// "offered" observation static_offer.go serves a play permission with):
//
//   - AddAbility$ naming a mana ability ("Creatures you control have '{T}: Add
//     one mana of any color'"): a probe the grant reaches is offered that
//     mana ability, by label, at p0's first priority. The control drops the
//     source, so the label proves the static's doing; a probe that already
//     taps for the same mana (a Forest and "Add G") is not a witness for it.
//   - AddAbility$ naming a non-mana activated ability ("Artifacts you control
//     have '{2}, Sacrifice this artifact: ...'"): the recipient probe is
//     offered the activation with the ability's rule text, paying its mana
//     cost. An Aura's cast attaches it to an opposing permanent, so the try
//     re-attaches it to its own probe (attachProbe) before the assertion.
//   - AddAbility$ naming a loyalty ability ("Planeswalkers you control have
//     '[-2]: ...'"): a planeswalker probe with enough loyalty is offered an
//     activation labelled with the granted ability's first sentence.
//   - AdjustLandPlays$ (a second land drop): p0 holds two lands, plays one,
//     and the other is offered as a play. Without the static the control has
//     no second drop.
//
// Every other grant shape keeps a named skip (staticGrantGap): a granted
// trigger, a static ability, abilities gained from another card and an SVar a
// granted trigger reads. Each needs an observation this file does not build,
// named in the skip so the census tells the shapes apart.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules/pay"
)

// grantProbeNames are the candidate recipients of a granted mana ability, in
// preference order: the fixture's Grizzly Bears first (so a creature grant
// keeps the common probe), then a basic land, an artifact, and one card per
// subtype a grant names (Frog, Ally, Cave, Food). The first probe the engine
// offers the grant on, and does not offer it without the source, serves the
// row.
var grantProbeNames = []string{
	"Grizzly Bears", "Forest", "Ornithopter", "Pond Prophet", "South Pole Voyager",
	"Secret Tunnel", "Lembas",
}

// grantPlaneswalkerNames are the candidate planeswalker recipients of a
// granted loyalty ability, by ascending starting loyalty; a probe is eligible
// when its loyalty pays the ability's cost.
var grantPlaneswalkerNames = []string{"Ajani Goldmane", "Gideon Jura", "Ugin, the Spirit Dragon"}

// grantLandPlayFirst and grantLandPlaySecond are the two lands of the
// second-land-drop observation: the first is played, the second is the probe.
const (
	grantLandPlayFirst  = "Plains"
	grantLandPlaySecond = "Forest"
)

// grantedManaAbility is the mana ability an AddAbility$ static grants, nil
// when the grant is not one (a loyalty ability, an activated ability).
func grantedManaAbility(f *cards.Face, st cards.Static) *cards.SA {
	name := strings.TrimSpace(st.ParamStr(cards.PKAddAbility))
	if name == "" || st.ParamStr(cards.PKEffectZone) != "" && !strings.EqualFold(st.ParamStr(cards.PKEffectZone), "Battlefield") {
		return nil
	}
	sa := cards.ResolveSVar(f.SVars, name)
	if sa == nil || sa.Kind != "AB" || sa.API != "Mana" || grantedLoyaltyCost(sa) >= 0 {
		return nil
	}
	return sa
}

// grantedLoyaltyCost is the loyalty a granted ability removes (0 for a plus
// or zero ability), or -1 when its cost has no loyalty part.
func grantedLoyaltyCost(sa *cards.SA) int {
	cost := sa.ParamStr(cards.PKCost)
	for _, kind := range []string{"SubCounter<", "AddCounter<"} {
		_, rest, ok := strings.Cut(cost, kind)
		if !ok {
			continue
		}
		num, spec, _ := strings.Cut(rest, "/")
		if !strings.HasPrefix(spec, "LOYALTY") {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil || kind == "AddCounter<" {
			return 0
		}
		return n
	}
	return -1
}

// grantedLoyaltyAbility is the loyalty ability an AddAbility$ static grants,
// nil for a loyalty ability that adds mana: its offered option's label carries
// none of its text (measured: "Add", "Add {R}" and "[+1]" miss it on a
// planeswalker probe while the control's own abilities answer "+1"), so there
// is nothing to tell the grant from the probe's printed abilities.
func grantedLoyaltyAbility(f *cards.Face, st cards.Static) *cards.SA {
	sa := cards.ResolveSVar(f.SVars, strings.TrimSpace(st.ParamStr(cards.PKAddAbility)))
	if sa == nil || sa.Kind != "AB" || grantedLoyaltyCost(sa) < 0 || sa.API == "Mana" {
		return nil
	}
	return sa
}

// grantedLoyaltyLabel is the first sentence of the granted ability's
// description, the stable part of the option's label.
func grantedLoyaltyLabel(sa *cards.SA) string {
	d := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
	if i := strings.Index(d, ". "); i >= 0 {
		d = d[:i]
	}
	return strings.Trim(strings.TrimSpace(d), `."`)
}

// grantedManaLabel is the label the engine's offer carries for the granted
// ability, without the cost prefix: "Add any color".
func grantedManaLabel(sa *cards.SA) string {
	label := pay.ManaAbilityLabel(sa, "")
	if i := strings.LastIndex(label, ": "); i >= 0 {
		label = label[i+2:]
	}
	return label
}

// grantedLandPlays reports whether the static is the plain extra-land-drop
// grant: a literal positive AdjustLandPlays$ on the player.
func grantedLandPlays(st cards.Static) bool {
	n, err := strconv.Atoi(strings.TrimSpace(st.ParamStr(cards.PKAdjustLandPlays)))
	return err == nil && n > 0
}

// firstPlayNeedsResolve reports whether the source card's own trigger fires
// when the first land enters or is played (a Landfall ChangesZone or a
// LandPlayed trigger): the trigger sits on the stack at the second land's
// offer checkpoint, where the land-play gate is not at sorcery speed, so the
// scenario resolves it first. A source with no land-entry trigger keeps its
// scenario bytes (the resolve is only emitted for a source that has one).
func firstPlayNeedsResolve(f *cards.Face) bool {
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		switch tr.Mode {
		case "LandPlayed":
			return true
		case "ChangesZone":
			if !strings.Contains(tr.ParamStr(cards.PKDestination), "Battlefield") {
				continue
			}
			for _, w := range affectedWords(tr.ParamStr(cards.PKValidCard)) {
				if w == "Land" {
					return true
				}
			}
		}
	}
	return false
}

// grantedActivatedAbility is the non-mana, non-loyalty activated ability an
// AddAbility$ static grants to another permanent (the recipient carries it),
// nil when the grant is not one. A granted activated ability is observed the
// same way a granted mana ability is: the recipient probe is offered the
// activation, labelled with the ability's description. Unlike the mana path
// the offered option carries the ability's rule text (the engine's
// legal_walk_battlefield.go labels it "<recipient>: <SpellDescription>"), so
// the scenario must pay the ability's mana cost to be offered at all.
func grantedActivatedAbility(f *cards.Face, st cards.Static) *cards.SA {
	name := strings.TrimSpace(st.ParamStr(cards.PKAddAbility))
	if name == "" || st.ParamStr(cards.PKEffectZone) != "" && !strings.EqualFold(st.ParamStr(cards.PKEffectZone), "Battlefield") {
		return nil
	}
	sa := cards.ResolveSVar(f.SVars, name)
	if sa == nil || sa.Kind != "AB" || sa.API == "Mana" || grantedLoyaltyCost(sa) >= 0 {
		return nil
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription)) == "" {
		return nil
	}
	return sa
}

// grantOfferable reports whether grantTries has candidates for the static.
func grantOfferable(f *cards.Face, st cards.Static) bool {
	return grantedLandPlays(st) || grantedManaAbility(f, st) != nil || grantedLoyaltyAbility(f, st) != nil ||
		grantedActivatedAbility(f, st) != nil
}

// grantTries are the candidate scenarios for a granted mana ability (one per
// probe) or an extra land drop (one).
func grantTries(reg *cards.Registry, f *cards.Face, st cards.Static) []offerTry {
	if grantedLandPlays(st) {
		t := offerTry{
			probe: grantLandPlaySecond, zone: offerHand, kind: "play",
			firstPlay: grantLandPlayFirst, extraHand: []string{grantLandPlayFirst},
			resolveFirstPlay: firstPlayNeedsResolve(f),
		}
		// An "as long as" rider (Thranduil's Company's IsPresent$
		// Elf.YouCtrl+Other) is an intervening-if the engine evaluates, so
		// the scenario must hold the board the gate reads or the grant never
		// exists and the observation proves nothing. The same presence
		// fixture machinery the condition fixtures use builds it; a presence
		// no fixture reaches leaves the row to its named gap.
		if present := st.ParamStr(cards.PKIsPresent); present != "" {
			zone := st.ParamStr(cards.PKPresentZone)
			if zone == "" {
				zone = "Battlefield"
			}
			fx, ok := staticPresence(present, zone, 1)
			if !ok {
				return nil
			}
			t.extraBF = fx.battlefield
		}
		return []offerTry{t}
	}
	if sa := grantedLoyaltyAbility(f, st); sa != nil {
		var tries []offerTry
		for _, p := range grantPlaneswalkerNames {
			c, ok := reg.Lookup(p)
			if !ok || len(c.Faces) == 0 {
				continue
			}
			if loyalty, err := strconv.Atoi(c.Faces[0].Loyalty); err == nil && loyalty >= grantedLoyaltyCost(sa) {
				tries = append(tries, offerTry{probe: p, kind: "activate", label: grantedLoyaltyLabel(sa), extraBF: []string{p}})
			}
		}
		return tries
	}
	sa := grantedManaAbility(f, st)
	if sa != nil {
		label := grantedManaLabel(sa)
		var tries []offerTry
		for _, p := range grantProbeNames {
			tries = append(tries, offerTry{probe: p, kind: "activate", label: label, extraBF: []string{p}})
		}
		return tries
	}
	sa = grantedActivatedAbility(f, st)
	if sa == nil {
		return nil
	}
	pool, gap := activationCost(sa.ParamStr(cards.PKCost))
	if gap != "" {
		return nil
	}
	desc := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
	// An Aura's fixture cast targets an opposing permanent; the grant only
	// lands on the recipient the Aura is attached to, so the try re-attaches
	// the Aura to its probe after the cast. An Equipment's base already
	// attaches it (staticBase), so it needs no re-attach.
	attach := staticGrantAttached(st) && !oraclegen.HasType(f, "Equipment")
	var tries []offerTry
	for _, p := range grantProbeNames {
		face := p
		if c, ok := reg.Lookup(p); ok && len(c.Faces) > 0 {
			face = c.Faces[0].Name
		}
		tries = append(tries, offerTry{
			probe: p, kind: "activate", label: face + ": " + desc, mana: pool,
			extraBF: []string{p}, attachProbe: attach,
		})
	}
	return tries
}

// staticGrantAttached reports whether the grant's Affected$ names the
// permanent the source is attached to (an Aura's EnchantedBy, an Equipment's
// EquippedBy/AttachedBy, a Fortification's FortifiedBy): the recipient is only
// on the battlefield as the source's host.
func staticGrantAttached(st cards.Static) bool {
	for _, w := range []string{"EnchantedBy", "EquippedBy", "AttachedBy", "FortifiedBy"} {
		if strings.Contains(st.ParamStr(cards.PKAffected), w) {
			return true
		}
	}
	return false
}

// The named skips for a grant no observation here serves, one per shape.
const (
	staticGrantManaReason        = "grants a mana ability to a recipient the fixture cannot give it (a token or a chosen-name recipient)"
	staticGrantLoyaltyReason     = "grants a loyalty ability the probe planeswalkers cannot pay for"
	staticGrantLoyaltyManaReason = "grants a loyalty ability that adds mana (its offered label names no text to assert)"
	staticGrantActivateReason    = "grants an activated ability (needs the driver's activate on a granted ability)"
	staticGrantTriggerReason     = "grants a triggered ability (needs a probe-sourced trigger cause)"
	staticGrantReplacementReason = "grants a replacement effect (needs an event the replacement can change)"
	staticGrantStaticReason      = "grants a static ability (observed only through its own effect)"
	staticGrantGainsReason       = "gains the activated abilities of other cards (needs a donor card)"
	staticGrantSVarReason        = "adds an SVar a granted trigger reads (needs that trigger's cause)"
)

// staticGrantGap names why a static that grants an ability shows nothing on a
// probe or the card, by the shape it grants; "" is the next gap's turn. A
// static with several grant keys is named for the first in this order.
func staticGrantGap(f *cards.Face, st cards.Static) string {
	switch {
	case st.HasParam(cards.PKAddAbility):
		switch sa := cards.ResolveSVar(f.SVars, strings.TrimSpace(st.ParamStr(cards.PKAddAbility))); {
		case sa != nil && grantedLoyaltyCost(sa) >= 0 && sa.API == "Mana":
			return staticGrantLoyaltyManaReason
		case sa != nil && grantedLoyaltyCost(sa) >= 0:
			return staticGrantLoyaltyReason
		case sa != nil && sa.API == "Mana":
			return staticGrantManaReason
		}
		return staticGrantActivateReason
	case st.HasParam(cards.PKAddTrigger), st.HasParam(cards.PKAddTriggers):
		return staticGrantTriggerReason
	case st.HasParam(cards.PKAddStaticAbility):
		return staticGrantStaticReason
	case st.Params["AddReplacementEffect"] != "":
		return staticGrantReplacementReason
	case st.HasParam(cards.PKGainsAbilitiesOf):
		return staticGrantGainsReason
	case st.HasParam(cards.PKAddSVar), st.HasParam(cards.PKAddSVars):
		return staticGrantSVarReason
	}
	return ""
}
