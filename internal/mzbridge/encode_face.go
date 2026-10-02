package mzbridge

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The per-face half of the state encoder: everything StateEncoder.java
// reads off a Card that does not depend on the game (its types, its mana
// cost, and its abilities with their names, costs and effect texts). It is
// built once per *cards.Face and reused; a face is immutable after linking.

// zoneMask is the set of zones an ability functions in (XMage's
// Ability.getZone(), which Abilities.getStaticAbilities(zone) and its two
// siblings filter on: AbilitiesImpl.java:124-190).
type zoneMask uint8

const (
	inBattlefield zoneMask = 1 << iota
	inHand
	inGraveyard
	inExile
	inStack
	inAll zoneMask = 0xff
)

func parseZones(spec string, def zoneMask) zoneMask {
	if spec == "" {
		return def
	}
	var m zoneMask
	for _, z := range strings.Split(spec, ",") {
		switch strings.TrimSpace(z) {
		case "Battlefield":
			m |= inBattlefield
		case "Hand":
			m |= inHand
		case "Graveyard":
			m |= inGraveyard
		case "Exile":
			m |= inExile
		case "Stack":
			m |= inStack
		case "All", "Any":
			m |= inAll
		}
	}
	if m == 0 {
		return def
	}
	return m
}

type abilityKind uint8

const (
	abStatic abilityKind = iota
	abActivated
	abTriggered
)

// abilityInfo is one ability as the encoder emits it.
type abilityInfo struct {
	kind  abilityKind
	rule  string // the sub-node name: XMage's Ability.getRule()
	zones zoneMask
	// costs: StateEncoder.processCosts (85-92)
	mana      []string // mana-cost symbol texts, "_dynamic" appended on emission
	manaValue int
	costs     []string // non-mana cost texts
	effects   []string // effect texts, added to the ability node's parent (102)
	isMana    bool     // ActivatedAbility.isManaAbility (113)
	tap       bool     // the cost taps the source
	cast      bool     // the card's SpellAbility
	playLand  bool     // the card's PlayLandAbility
	index     int      // Face.Abilities index of an activated ability, else -1
	sa        *cards.SA
	trigger   string // Forge trigger mode
}

type faceInfo struct {
	cardTypes []string // CardType constant names, type-line order
	subTypes  []string // SubType constant names
	permanent bool
	manaValue int
	manaSyms  []string
	abilities []abilityInfo // statics, then activated, then triggered (306-329)
}

// effectTexts stands in for the Effect.getText of each effect of an ability
// (StateEncoder.java:100-104): one entry per link of the gorge ability
// chain, the link's own SpellDescription$ when it has one, else the
// structural name "Effect:<API>".
func effectTexts(sa *cards.SA) []string {
	var out []string
	for n := 0; sa != nil && n < 16; sa, n = sa.Sub, n+1 {
		if d := sa.Params["SpellDescription"]; d != "" {
			out = append(out, CleanString(thisName(d)))
		} else if sa.API != "" {
			out = append(out, "Effect:"+sa.API)
		}
	}
	return out
}

// xmageCostPrefix renders an activated ability's cost the way
// AbilityImpl.getRule does: mana symbols run together, then the other
// costs, joined with ", ".
func xmageCostPrefix(mana, costs []string) string {
	s := strings.Join(mana, "")
	for _, c := range costs {
		if s != "" {
			s += ", "
		}
		s += c
	}
	return s
}

func buildFaceInfo(f *cards.Face) *faceInfo {
	fi := &faceInfo{permanent: f.IsPermanent(), manaValue: int(f.Cmc()), manaSyms: manaCost(f.ManaCost)}
	for _, w := range f.Types {
		switch class, name := classifyType(w); class {
		case typeCard:
			fi.cardTypes = append(fi.cardTypes, name)
		case typeSub:
			if name != "" {
				fi.subTypes = append(fi.subTypes, name)
			}
		}
	}

	// Static abilities: keywords, S: lines, R: lines.
	for _, kw := range f.Keywords {
		fi.abilities = append(fi.abilities, abilityInfo{kind: abStatic, rule: keywordRule(kw), zones: inBattlefield, index: -1})
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		desc := st.Params["Description"]
		rule, ok := attributeOracle(f, desc, false)
		switch {
		case ok:
		case desc != "":
			rule = thisName(desc)
		default:
			rule = "S:" + st.Mode
		}
		fi.abilities = append(fi.abilities, abilityInfo{kind: abStatic, rule: rule,
			zones: parseZones(st.Params["EffectZone"], inBattlefield), index: -1})
	}
	for i := range f.Repls {
		rp := &f.Repls[i]
		desc := rp.Params["Description"]
		rule, ok := attributeOracle(f, desc, false)
		switch {
		case ok:
		case desc != "":
			rule = thisName(desc)
		default:
			rule = "R:" + rp.Event
		}
		fi.abilities = append(fi.abilities, abilityInfo{kind: abStatic, rule: rule,
			zones: parseZones(rp.Params["ActiveZones"], inBattlefield), index: -1})
	}

	// Activated abilities. The card's own cast (SpellAbility, zone HAND,
	// rule "" for a permanent and the spell text otherwise) or land play
	// (PlayLandAbility, rule "Play <name>") comes first, as XMage's card
	// constructor adds it first.
	sp := f.SpellAbility()
	switch {
	case f.IsLand():
		fi.abilities = append(fi.abilities, abilityInfo{kind: abActivated, rule: "Play " + f.Name, zones: inHand, playLand: true, index: -1})
	default:
		a := abilityInfo{kind: abActivated, zones: inHand, cast: true, index: -1, sa: sp,
			mana: fi.manaSyms, manaValue: fi.manaValue}
		if sp != nil {
			a.effects = effectTexts(sp)
			if !fi.permanent {
				desc := sp.Params["SpellDescription"]
				if rule, ok := attributeOracle(f, desc, false); ok {
					a.rule = rule
				} else if desc != "" {
					a.rule = thisName(desc)
				} else {
					a.rule = "SP:" + sp.API
				}
			}
		}
		fi.abilities = append(fi.abilities, a)
	}
	for i, ab := range f.Abilities {
		if ab == nil || ab.Kind != "AB" {
			continue
		}
		a := abilityInfo{kind: abActivated, index: i, sa: ab, effects: effectTexts(ab),
			zones:  parseZones(ab.Params["ActivationZone"], inBattlefield),
			isMana: ab.API == "Mana" || ab.API == "ManaReflected"}
		a.mana, a.manaValue, a.costs, a.tap = abilityCost(ab.Params["Cost"])
		desc := ab.Params["SpellDescription"]
		// The mana ability a basic land type grants (CR 305.6) is added by
		// the engine with no text; XMage's is "{T}: Add {G}."
		// (BasicManaAbility).
		if p := ab.Params["Produced"]; desc == "" && a.isMana && len(p) == 1 && strings.Contains("WUBRGC", p) {
			desc = "Add {" + p + "}."
			a.effects = []string{desc}
		}
		if rule, ok := attributeOracle(f, desc, true); ok {
			a.rule = rule
		} else if desc != "" {
			a.rule = thisName(desc)
			if prefix := xmageCostPrefix(a.mana, a.costs); prefix != "" {
				a.rule = prefix + ": " + a.rule
			}
		} else {
			a.rule = "AB:" + ab.API
		}
		fi.abilities = append(fi.abilities, a)
	}

	// Triggered abilities.
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if t.Params["Secondary"] == "True" {
			continue
		}
		desc := t.Params["TriggerDescription"]
		rule, ok := attributeOracle(f, desc, false)
		switch {
		case ok:
		case desc != "":
			rule = thisName(desc)
		default:
			rule = "T:" + t.Mode
		}
		zones := parseZones(t.Params["TriggerZones"], inBattlefield)
		// ZoneChangeTriggeredAbility also counts in its destination zone
		// (AbilitiesImpl.java:183-187).
		if t.Mode == "ChangesZone" && strings.Contains(t.Params["ValidCard"], "Self") {
			zones |= parseZones(t.Params["Destination"], 0)
		}
		fi.abilities = append(fi.abilities, abilityInfo{kind: abTriggered, rule: rule, zones: zones,
			effects: effectTexts(t.Effect), index: -1, sa: t.Effect, trigger: t.Mode})
	}
	return fi
}
