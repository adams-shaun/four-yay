// Level-B static shapes for the remaining static modes (ticket
// levelb-remaining-static-modes). Each reports the exact parameter shape the
// matching template in compliance/oraclegen/templates serves; any other
// parameter keeps the requirement a named gap, so a shape a template cannot
// observe never changes class silently. The modes whose engine behaviour is
// not observed by any template (FlipCoinMod) or whose fixture cannot be set
// up (a Condition$ EnduringStory gate) keep a named gap here rather than the
// generic "static mode X" string.
package levelb

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// staticParamsOnly reports whether every parameter st carries is in keys
// (Mode$ and Description$ are always allowed: they are the mode itself and
// the display text every corpus body names).
func staticParamsOnly(st *cards.Static, keys ...string) bool {
	allowed := map[string]bool{"Mode": true, "Description": true}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range st.Params {
		if !allowed[k] {
			return false
		}
	}
	return true
}

// manaTypeLetter maps Forge's ManaType$ colour word to the pool letter the
// template puts in the mana op.
var manaTypeLetter = map[string]string{
	"white": "W", "blue": "U", "black": "B", "red": "R", "green": "G", "colorless": "C",
}

// serveStaticMode reports the classification for one static mode the
// remaining-modes ticket serves or names. A served shape returns its
// sub-family and ""; a mode this ticket owns whose shape no template observes
// returns "" and a named gap; a mode the ticket does not own returns "" and
// "" so the classifier's later chain runs unchanged.
func serveStaticMode(f *cards.Face, st *cards.Static) (sub, gap string) {
	gapSub := "static.gap:" + st.Mode
	switch strings.ToLower(st.Mode) {
	case "unspentmana":
		if sub, ok := unspentManaShape(st); ok {
			return sub, ""
		}
		return gapSub, "UnspentMana ManaType$ " + st.Params["ManaType"] +
			" is not one single colour word the pool fixture can hold"
	case "cantpreventdamage":
		if sub, ok := cantPreventDamageShape(st); ok {
			return sub, ""
		}
		return gapSub, "CantPreventDamage with ValidSource$ or an unmodelled IsCombat$ value is not observed"
	case "ignorehexproof":
		if ignoreHexproofShape(st) {
			return "static.ignore-hexproof", ""
		}
		return gapSub, "IgnoreHexproof ValidEntity$ " + st.ParamStr(cards.PKValidEntity) +
			" is not the opponent-creature scope the hexproof-probe template observes"
	case "nocleanupdamage":
		if noCleanupDamageShape(st) {
			return "static.no-cleanup-damage", ""
		}
		return gapSub, "NoCleanupDamage ValidCard$ " + st.ParamStr(cards.PKValidCard) +
			" is not the card's own scope the cleanup-step template observes"
	case "ignorelegendrule":
		if ignoreLegendRuleShape(st) {
			return "static.ignore-legend-rule", ""
		}
		return gapSub, "IgnoreLegendRule ValidCard$ " + st.ParamStr(cards.PKValidCard) +
			" is not a single type's you-control scope the legend-rule template observes"
	case "activateabilityasifhaste":
		if activateAsIfHasteShape(st) {
			return "static.activate-as-if-haste", ""
		}
		return gapSub, "ActivateAbilityAsIfHaste ValidCard$ " + st.ParamStr(cards.PKValidCard) +
			" is not a you-control creature scope the summoning-sickness template observes"
	case "cantbecopied":
		if cantBeCopiedShape(st) {
			return "static.cant-be-copied", ""
		}
		return gapSub, "CantBeCopied without the spell's own EffectZone$ Stack scope is not observed"
	case "activations":
		if activationsPowerUpShape(st) {
			return "static.activations-powerup", ""
		}
		return gapSub, "Activations ValidSA$ " + st.ParamStr(cards.PKValidSA) + " MinLimit$ " +
			st.ParamStr(cards.PKMinLimit) + " is not the positive PowerUp ceiling the power-up template observes"
	case "cantbesuspected":
		if cantBeSuspectedShape(st) {
			return "static.cant-be-suspected", ""
		}
		return gapSub, "CantBeSuspected ValidCard$ " + st.ParamStr(cards.PKValidCard) +
			" is not the enchanted-creature scope the suspect-probe template observes"
	case "plotzone":
		if plotZoneShape(st) {
			return "static.plot-zone", ""
		}
		return gapSub, "PlotZone ValidCard$ " + st.ParamStr(cards.PKValidCard) +
			" is not the top-of-library scope the plot-offer template observes"
	case "cantputcounter":
		if cantPutCounterShape(st) {
			return "static.cant-put-counter", ""
		}
		return gapSub, "CantPutCounter with CounterType$ or an unmodelled scope is not observed"
	case "manaconvert":
		// The creature-spell form was already served by the classifier's
		// mana-convert-creature-spells branch above; this case sees only the
		// rest, of which this ticket serves the two extra shapes.
		if sub, ok := manaConvertExtraShape(st); ok {
			return sub, ""
		}
		return gapSub, "ManaConvert ValidCard$ " + st.ParamStr(cards.PKValidCard) + " ValidSA$ " +
			st.ParamStr(cards.PKValidSA) + " is not a served conversion shape"
	case "cantattackunless":
		if cantAttackUnlessTaxShape(st) {
			return "static.cant-attack-unless-tax", ""
		}
		return gapSub, "CantAttackUnless " + staticUnlessShapeOf(st) + " is not a served unless-tax shape"
	case "cantblockunless":
		if cantBlockUnlessTaxShape(st) {
			return "static.cant-block-unless-tax", ""
		}
		return gapSub, "CantBlockUnless " + staticUnlessShapeOf(st) + " is not a served unless-tax shape"
	case "alternativecost":
		if strings.Contains(strings.ToLower(st.ParamStr(cards.PKCondition)), "enduringstory") {
			return gapSub, "AlternativeCost Condition$ EnduringStory cannot be set up by any fixture"
		}
		return gapSub, "AlternativeCost Cost$ " + st.ParamStr(cards.PKCost) +
			" needs an alternative-cost cast option the runner does not select"
	case "flipcoinmod":
		return gapSub, "FlipCoinMod is not read by the engine and has no coin-flip probe whose result a snapshot carries"
	}
	return "", ""
}

// staticUnlessShapeOf summarises an unserved unless static's shape for the gap.
func staticUnlessShapeOf(st *cards.Static) string {
	var parts []string
	for _, key := range []cards.ParamKey{cards.PKValidCard, cards.PKIsPresent, cards.PKCondition, cards.PKCost} {
		if v := st.ParamStr(key); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

// unspentManaShape reports whether st is a single-colour unspent-mana keep
// ("You don't lose unspent red mana", Electro): ValidPlayer$ You, ManaType$
// one colour word, no other parameter.
func unspentManaShape(st *cards.Static) (string, bool) {
	if !strings.EqualFold(st.ParamStr(cards.PKValidPlayer), "You") {
		return "", false
	}
	if manaTypeLetter[strings.ToLower(st.Params["ManaType"])] == "" {
		return "", false
	}
	if !staticParamsOnly(st, "ValidPlayer", "ManaType") {
		return "", false
	}
	return "static.unspent-mana", true
}

// cantPreventDamageShape reports whether st is a CantPreventDamage static the
// damage templates observe: an unconditional one (Sunspine Lynx, Spider-Punk)
// or the combat-only IsCombat$ True form (Frenzied Baloth). ValidSource$ and
// the other conditional parameters stay named gaps.
func cantPreventDamageShape(st *cards.Static) (string, bool) {
	switch v := strings.ToLower(st.ParamStr(cards.PKIsCombat)); v {
	case "":
		if !staticParamsOnly(st) {
			return "", false
		}
		return "static.cant-prevent-damage", true
	case "true":
		if !staticParamsOnly(st, "IsCombat") {
			return "", false
		}
		return "static.cant-prevent-damage-combat", true
	default:
		return "", false
	}
}

// ignoreHexproofShape reports whether st is the "creatures your opponents
// control can be targeted as though they didn't have hexproof" scope
// (Nowhere to Run).
func ignoreHexproofShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidEntity), "Creature.OppCtrl") {
		return false
	}
	return staticParamsOnly(st, "ValidEntity")
}

// noCleanupDamageShape reports whether st is "damage isn't removed from this
// creature during cleanup steps" scoped to the card itself (Ancient
// Adamantoise).
func noCleanupDamageShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.Self") {
		return false
	}
	return staticParamsOnly(st, "ValidCard")
}

// ignoreLegendRuleShape reports whether st is a single-type legend-rule
// exemption ("The legend rule doesn't apply to Spiders you control",
// Spider-Verse): one ValidCard$ alternative naming <Type>.YouCtrl.
func ignoreLegendRuleShape(st *cards.Static) bool {
	vc := st.ParamStr(cards.PKValidCard)
	alts := 0
	for alt := range strings.SplitSeq(vc, ",") {
		alts++
		if alts > 1 {
			return false
		}
		words := strings.Split(strings.ToLower(strings.TrimSpace(alt)), ".")
		if len(words) != 2 || words[1] != "youctrl" || words[0] == "" ||
			strings.Contains(words[0], "+") {
			return false
		}
	}
	if alts == 0 {
		return false
	}
	return staticParamsOnly(st, "ValidCard")
}

// activateAsIfHasteShape reports whether st is the "may activate abilities of
// creatures you control as though those creatures had haste" scope (Shang-Chi
// and the corpus-wide rest): Creature.YouCtrl, with or without the
// inZoneBattlefield qualifier.
func activateAsIfHasteShape(st *cards.Static) bool {
	switch strings.ToLower(st.ParamStr(cards.PKValidCard)) {
	case "creature.youctrl", "creature.youctrl+inzonebattlefield":
	default:
		return false
	}
	return staticParamsOnly(st, "ValidCard")
}

// cantBeCopiedShape reports whether st is the spell's own "this spell can't
// be copied" (Choreographed Sparks): Card.Self while the spell is on the
// stack.
func cantBeCopiedShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.Self") ||
		!strings.EqualFold(st.ParamStr(cards.PKEffectZone), "Stack") {
		return false
	}
	return staticParamsOnly(st, "ValidCard", "EffectZone")
}

// activationsPowerUpShape reports whether st is the "each power-up ability of
// permanents you control can be activated an additional time" ceiling (Wonder
// Man): ValidSA$ Activated.PowerUp, a you-control scope, a positive finite
// MinLimit$ ceiling, and no condition parameter.
func activationsPowerUpShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidSA), "Activated.PowerUp") {
		return false
	}
	switch strings.ToLower(st.ParamStr(cards.PKValidCard)) {
	case "permanent.youctrl", "creature.youctrl":
	default:
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(st.ParamStr(cards.PKMinLimit)))
	if err != nil || n < 2 {
		return false
	}
	switch strings.ToLower(st.ParamStr(cards.PKValidPlayer)) {
	case "", "you":
	default:
		return false
	}
	if t := st.ParamStr(cards.PKPlayerTurn); t != "" && !strings.EqualFold(t, "You") {
		return false
	}
	return staticParamsOnly(st, "ValidSA", "ValidCard", "MinLimit", "ValidPlayer", "PlayerTurn")
}

// cantBeSuspectedShape reports whether st is the "enchanted creature can't
// become suspected" scope (Airtight Alibi).
func cantBeSuspectedShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Creature.EnchantedBy") {
		return false
	}
	return staticParamsOnly(st, "ValidCard", "Secondary")
}

// plotZoneShape reports whether st is the "you may plot nonland cards from
// the top of your library" scope (Fblthp, Lost on the Range).
func plotZoneShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.TopLibrary+YouCtrl+nonLand") {
		return false
	}
	return staticParamsOnly(st, "ValidCard")
}

// cantPutCounterShape reports whether st is the "enchanted creature can't have
// counters put on it" scope (Blossombind): the enchanted host, no CounterType$
// narrowing, only parameters the engine's CantPutCounterParamsReadable
// whitelist already reads.
func cantPutCounterShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.EnchantedBy") {
		return false
	}
	if st.HasParam(cards.PKCounterType) {
		return false
	}
	return staticParamsOnly(st, "ValidCard", "Secondary")
}

// manaConvertExtraShape reports whether st is one of the two ManaConvert
// shapes beyond the existing creature-spell one the ticket serves: the Case
// spells (Case File Auditor) and the activated abilities of creatures you
// control (Agatha's Soul Cauldron). Both share ManaConversion$ AnyType->AnyColor.
func manaConvertExtraShape(st *cards.Static) (string, bool) {
	if !strings.EqualFold(st.ParamStr(cards.PKValidPlayer), "You") ||
		!strings.EqualFold(st.ParamStr(cards.PKManaConversion), "AnyType->AnyColor") {
		return "", false
	}
	vc := strings.ToLower(st.ParamStr(cards.PKValidCard))
	vsa := strings.ToLower(st.ParamStr(cards.PKValidSA))
	switch {
	case vc == "case.youctrl" && vsa == "spell":
		if !staticParamsOnly(st, "ValidPlayer", "ValidCard", "ValidSA", "ManaConversion") {
			return "", false
		}
		return "static.mana-convert-case-spells", true
	case vc == "creature.youctrl+inzonebattlefield" && vsa == "activated":
		if !staticParamsOnly(st, "ValidPlayer", "ValidCard", "ValidSA", "ManaConversion") {
			return "", false
		}
		return "static.mana-convert-abilities", true
	}
	return "", false
}

// cantAttackUnlessTaxShape reports whether st is Archangel of Tithes's attack
// tax: creatures can't attack you unless their controller pays {1}, gated on
// the card itself being untapped. Condition$/CheckSVar$ carriers (Dáin) stay
// named gaps.
func cantAttackUnlessTaxShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Creature") ||
		!strings.EqualFold(st.ParamStr(cards.PKIsPresent), "Card.Self+untapped") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(st.ParamStr(cards.PKCost)))
	if err != nil || n < 1 {
		return false
	}
	switch strings.ToLower(st.ParamStr(cards.PKTarget)) {
	case "you", "you,planeswalker.youctrl":
	default:
		return false
	}
	return staticParamsOnly(st, "ValidCard", "Target", "Cost", "IsPresent")
}

// cantBlockUnlessTaxShape reports whether st is Archangel of Tithes's block
// tax: creatures can't block unless their controller pays {1}, gated on the
// card itself attacking.
func cantBlockUnlessTaxShape(st *cards.Static) bool {
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Creature") ||
		!strings.EqualFold(st.ParamStr(cards.PKIsPresent), "Card.Self+attacking") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(st.ParamStr(cards.PKCost)))
	if err != nil || n < 1 {
		return false
	}
	return staticParamsOnly(st, "ValidCard", "Cost", "IsPresent")
}
