package mzbridge

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// XMage spellings the state encoder hashes, and the gorge-side stand-ins for
// the strings XMage generates and gorge cannot (ability rules, effect texts,
// cost texts). Every table here was read from the XMage fork at 48e4918413;
// the file each one mirrors is named at its head.

// stepNames is mage/constants/PhaseStep.java. StateEncoder.java:635 hashes
// game.getTurnStepType().toString(), and PhaseStep's toString override is
// commented out in the fork, so the feature is the enum CONSTANT name. gorge
// has one combat-damage step; FIRST_COMBAT_DAMAGE is never emitted.
var stepNames = [...]string{
	state.StepUntap:            "UNTAP",
	state.StepUpkeep:           "UPKEEP",
	state.StepDraw:             "DRAW",
	state.StepMain1:            "PRECOMBAT_MAIN",
	state.StepBeginCombat:      "BEGIN_COMBAT",
	state.StepDeclareAttackers: "DECLARE_ATTACKERS",
	state.StepDeclareBlockers:  "DECLARE_BLOCKERS",
	state.StepCombatDamage:     "COMBAT_DAMAGE",
	state.StepEndCombat:        "END_COMBAT",
	state.StepMain2:            "POSTCOMBAT_MAIN",
	state.StepEnd:              "END_TURN",
	state.StepCleanup:          "CLEANUP",
}

// StepName is the PhaseStep constant name for a gorge step, "" when the
// step is out of range.
func StepName(s state.Step) string {
	if int(s) >= len(stepNames) {
		return ""
	}
	return stepNames[s]
}

// typeClass sorts one word of a gorge type line.
type typeClass uint8

const (
	typeSub typeClass = iota
	typeCard
	typeSuper
)

// classifyType maps a type-line word to its class and the name XMage's
// encoder hashes for it. Card types are mage/constants/CardType.java
// constant names (ct.name(), StateEncoder.java:155); Forge's older "Tribal"
// is XMage's KINDRED. Supertypes are never encoded upstream
// (StateEncoder reads getCardType and getSubtype only). Everything else is
// a subtype: mage/constants/SubType.java constant names, which are the
// printed word upper-cased with apostrophes dropped and '-' or ' ' turned
// into '_' (URZAS, POWER_PLANT, TIME_LORD).
func classifyType(word string) (typeClass, string) {
	switch word {
	case "Artifact":
		return typeCard, "ARTIFACT"
	case "Battle":
		return typeCard, "BATTLE"
	case "Conspiracy":
		return typeCard, "CONSPIRACY"
	case "Creature":
		return typeCard, "CREATURE"
	case "Dungeon":
		return typeCard, "DUNGEON"
	case "Enchantment":
		return typeCard, "ENCHANTMENT"
	case "Instant":
		return typeCard, "INSTANT"
	case "Land":
		return typeCard, "LAND"
	case "Phenomenon":
		return typeCard, "PHENOMENON"
	case "Plane":
		return typeCard, "PLANE"
	case "Planeswalker":
		return typeCard, "PLANESWALKER"
	case "Scheme":
		return typeCard, "SCHEME"
	case "Sorcery":
		return typeCard, "SORCERY"
	case "Kindred", "Tribal":
		return typeCard, "KINDRED"
	case "Vanguard":
		return typeCard, "VANGUARD"
	case "Basic", "Legendary", "Snow", "World", "Ongoing", "Elite", "Host", "Token":
		return typeSuper, word
	}
	return typeSub, subTypeName(word)
}

func subTypeName(word string) string {
	var b strings.Builder
	b.Grow(len(word))
	for i := 0; i < len(word); i++ {
		c := word[i]
		switch {
		case c == '\'':
		case c == '-' || c == ' ':
			b.WriteByte('_')
		case c >= 'a' && c <= 'z':
			b.WriteByte(c - 'a' + 'A')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// counterName is the XMage counter name (mage/counters/CounterType.java)
// for a gorge counter kind: "+1/+1" and "-1/-1" for the boost counters
// (BoostCounter.name), the lower-cased word for every other kind
// (LOYALTY -> "loyalty", STUN -> "stun"). gorge's two internal markers are
// not counters and report "".
func counterName(kind string) string {
	switch kind {
	case "P1P1":
		return "+1/+1"
	case "M1M1":
		return "-1/-1"
	}
	if state.InternalCounterMarker(kind) {
		return ""
	}
	return strings.ToLower(kind)
}

// manaSymbol is the XMage cost text of one Forge mana-cost token
// (GenericManaCost.getText "{3}", ColoredManaCost.getText "{G}", hybrid
// "{G/W}"); ok is false for a token that is not a mana symbol.
func manaSymbol(tok string) (sym string, value int, ok bool) {
	if tok == "" {
		return "", 0, false
	}
	if n, err := strconv.Atoi(tok); err == nil && n >= 0 {
		return "{" + tok + "}", n, true
	}
	const letters = "WUBRGCSXP"
	for i := 0; i < len(tok); i++ {
		if tok[i] != '/' && strings.IndexByte(letters, tok[i]) < 0 {
			return "", 0, false
		}
	}
	switch {
	case tok == "X":
		return "{X}", 0, true
	case len(tok) == 1:
		return "{" + tok + "}", 1, true
	case len(tok) == 2 && strings.IndexByte(tok, '/') < 0:
		return "{" + tok[:1] + "/" + tok[1:] + "}", 1, true
	}
	return "{" + tok + "}", 1, true
}

// manaCost splits a Forge mana cost ("3 W W", "X G", "no cost") into its
// XMage symbol texts.
func manaCost(cost string) []string {
	if cost == "" || cost == "no cost" {
		return nil
	}
	var out []string
	for _, tok := range strings.Fields(cost) {
		if sym, _, ok := manaSymbol(tok); ok {
			out = append(out, sym)
		}
	}
	return out
}

// abilityCost splits a Forge Cost$ value into mana symbols, their total
// mana value, and the non-mana cost texts. "T" and "Q"/"Untap" are XMage's
// "{T}" and "{Q}" (TapSourceCost.getText); every other non-mana cost keeps
// its Forge token, a stable structural name for a text gorge does not have.
func abilityCost(cost string) (mana []string, value int, other []string, tap bool) {
	for _, tok := range strings.Fields(cost) {
		switch tok {
		case "T":
			other = append(other, "{T}")
			tap = true
			continue
		case "Q", "Untap":
			other = append(other, "{Q}")
			continue
		}
		if sym, v, ok := manaSymbol(tok); ok {
			mana = append(mana, sym)
			value += v
			continue
		}
		other = append(other, tok)
	}
	return mana, value, other, tap
}

// thisName replaces Forge's self-reference placeholders with XMage's
// "{this}" (AbilityImpl.getRule leaves "{this}" in the rule; StateEncoder
// hashes getRule(), not getRule(sourceName)).
func thisName(text string) string {
	if !strings.Contains(text, "CARDNAME") && !strings.Contains(text, "NICKNAME") {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "CARDNAME", "{this}"), "NICKNAME", "{this}")
}

// stripReminder removes every parenthesised run and trims the result.
func stripReminder(s string) string {
	if strings.IndexByte(s, '(') < 0 {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
		case depth == 0:
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

// oracleParagraphs is a face's Oracle text split into its paragraphs, with
// reminder text removed. The corpus stores the paragraph break as the two
// characters backslash-n.
func oracleParagraphs(f *cards.Face) []string {
	if f == nil || strings.TrimSpace(f.Oracle) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(f.Oracle, `\n`, "\n"), "\n") {
		if p = stripReminder(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// firstWordOf is Forge's NICKNAME default, the name's first word.
func firstWordOf(name string) string {
	if i := strings.IndexByte(name, ' '); i >= 0 {
		name = name[:i]
	}
	return strings.TrimRight(name, ",")
}

// attributeOracle finds the Oracle paragraph of f that an ability with the
// Forge description desc was printed as: the paragraph that equals the
// description once the description's placeholders are filled with the
// card's name, or, for an activated ability, the "cost: effect" paragraph
// whose effect half equals it. It returns that paragraph with the card's
// own name written as "{this}", and false when no paragraph, or more than
// one, matches.
func attributeOracle(f *cards.Face, desc string, activated bool) (string, bool) {
	if f == nil || desc == "" {
		return "", false
	}
	want := strings.ReplaceAll(desc, "CARDNAME", f.Name)
	want = stripReminder(strings.ReplaceAll(want, "NICKNAME", firstWordOf(f.Name)))
	if want == "" {
		return "", false
	}
	found, hits := "", 0
	for _, p := range oracleParagraphs(f) {
		ok := p == want
		if !ok && activated {
			if i := strings.Index(p, ": "); i >= 0 && p[i+2:] == want {
				ok = true
			}
		}
		if ok && p != found {
			found = p
			hits++
		}
	}
	if hits != 1 {
		return "", false
	}
	if f.Name != "" {
		found = strings.ReplaceAll(found, f.Name, "{this}")
	}
	return found, true
}

// keywordRule names a keyword ability. XMage's keyword rules are the
// lower-cased keyword ("flying", FlyingAbility.getRule); a Forge keyword
// with parameters ("Ward:2", "Enchant:Creature") keeps its Forge spelling,
// and a sentence keyword ("CARDNAME can't block.") keeps its case with the
// placeholder rewritten.
func keywordRule(kw string) string {
	switch {
	case strings.Contains(kw, "CARDNAME") || strings.Contains(kw, "NICKNAME"):
		return thisName(kw)
	case strings.IndexByte(kw, ':') >= 0:
		return kw
	}
	return strings.ToLower(kw)
}

// svarDescription pulls SpellDescription$ out of a raw SVar ability line.
func svarDescription(raw string) string {
	const key = "SpellDescription$ "
	i := strings.Index(raw, key)
	if i < 0 {
		return ""
	}
	raw = raw[i+len(key):]
	if j := strings.Index(raw, " | "); j >= 0 {
		raw = raw[:j]
	}
	return strings.TrimSpace(raw)
}
