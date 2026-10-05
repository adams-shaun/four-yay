package cost

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/state"
)

// FormatCost writes the parsed cost back in the whitespace-delimited Forge
// notation understood by the client. Parsing first is intentional: malformed
// tokens retain the engine's real conservative one-generic interpretation,
// and every recognised non-mana component remains visible so a client that
// cannot price it can still fail closed.
func FormatCost(c Cost) string {
	// The tokens are written straight into one buffer, joined by single
	// spaces exactly as strings.Join(tokens, " ") joins them (an empty token
	// still takes its separator).
	var buf [64]byte
	b := buf[:0]
	nTok := 0
	add := func(tok string) {
		if nTok > 0 {
			b = append(b, ' ')
		}
		nTok++
		b = append(b, tok...)
	}
	if c.Generic > 0 {
		add(strconv.FormatInt(int64(c.Generic), 10))
	}
	const faces = "WUBRGC"
	for i, face := range []byte(faces) {
		for n := int32(0); n < c.Colored[i]; n++ {
			add(string(face))
		}
	}
	for range c.X {
		add("X")
	}
	for _, h := range c.Hybrid {
		add(string([]byte{h.A, '/', h.B}))
	}
	for _, t := range c.Twobrid {
		add(strconv.FormatInt(int64(t.Generic), 10) + "/" + string(t.Col))
	}
	for _, p := range c.Phyrexian {
		add(string([]byte{p, 'P'}))
	}
	for _, hp := range c.HybridPhyrexian {
		add(string([]byte{hp.A, '/', hp.B, '/', 'P'}))
	}
	for n := c.Snow; n > 0; n-- {
		add("S")
	}
	if c.Life > 0 {
		add("PayLife<" + strconv.FormatInt(int64(c.Life), 10) + ">")
	}
	for range c.LifeX {
		add("PayLife<X>")
	}
	for _, part := range c.DamageYou {
		add("DamageYou<" + strconv.FormatInt(int64(part.N), 10) + ">")
	}
	for _, part := range c.GainLife {
		tok := "GainLife<" + strconv.FormatInt(int64(part.N), 10) + "/" + part.Spec
		if part.Each {
			tok += "/*"
		}
		add(tok + ">")
	}
	if c.Tap {
		add("T")
	}
	if c.Untap {
		add("Q")
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			add("PayEnergy<X>")
		} else {
			add("PayEnergy<" + strconv.FormatInt(int64(part.N), 10) + ">")
		}
	}
	appendCostParts := func(kind string, costs []CostPart) {
		for _, part := range costs {
			n := strconv.FormatInt(int64(part.N), 10)
			if part.Dyn != "" {
				n = part.Dyn
			}
			add(kind + "<" + n + "/" + part.Spec + ">")
		}
	}
	appendCostParts("Sac", c.Sac)
	appendCostParts("Discard", c.Discard)
	appendCostParts("Draw", c.Draw)
	appendCostParts("SubCounter", c.SubCounter)
	appendCostParts("AddCounter", c.AddCounter)
	for _, part := range c.Exile {
		if part.ZoneSet != 0 {
			n := strconv.FormatInt(int64(part.N), 10)
			if part.Announced {
				n = "X"
			}
			add("ExileCtrlOrGrave<" + n + "/" + part.Spec + ">")
			continue
		}
		var head string
		switch part.Zone {
		case state.ZBattlefield:
			head = "Exile"
		case state.ZGraveyard:
			head = "ExileFromGrave"
		default:
			head = "ExileFromHand"
		}
		n := strconv.FormatInt(int64(part.N), 10)
		if part.Announced {
			n = "X"
		}
		add(head + "<" + n + "/" + part.Spec + ">")
	}
	for _, part := range c.ExileFromTop {
		add("ExileFromTop<" + strconv.FormatInt(int64(part.N), 10) + "/" + part.Spec + ">")
	}
	appendCostParts("Reveal", c.Reveal)
	// RevealOrChoose prints its own head so Compile/Decompile round-trips back
	// into the distinct slice (appendCostParts' generic head would print the
	// bare kind name and re-parse as the fallback). The optional trailing
	// description field is preserved when present, matching the parser's
	// three-field form.
	for _, part := range c.RevealOrChoose {
		headName := "RevealOrChoose"
		if part.ChooseCard {
			headName = "ChooseCard"
		}
		head := headName + "<" + strconv.FormatInt(int64(part.N), 10) + "/" + part.Spec
		if part.Desc != "" {
			head += "/" + part.Desc
		}
		add(head + ">")
	}
	for _, part := range c.RevealChosen {
		// RevealChosen<Player> has no trailing field; RevealChosen<Type/...>
		// prints its description. Both are re-parseable by revealChosenCost.
		if part.Desc == "" {
			add("RevealChosen<" + part.Spec + ">")
		} else {
			add("RevealChosen<" + part.Spec + "/" + part.Desc + ">")
		}
	}
	appendCostParts("Behold", c.Behold)
	appendCostParts("ExiledMoveToGrave", c.MoveToGrave)
	for _, part := range c.Mill {
		add("Mill<" + strconv.FormatInt(int64(part.N), 10) + ">")
	}
	appendCostParts("tapXType", c.TapPermanent)
	for _, part := range c.Blight {
		if part.Announced {
			add("Blight<X>")
			continue
		}
		add("Blight<" + strconv.FormatInt(int64(part.N), 10) + ">")
	}
	appendCostParts("Return", c.Return)
	for range c.Exert {
		add("Exert<1/CARDNAME>")
	}
	if c.Forage {
		add("Forage")
	}
	appendCostParts("PayEnergy", c.Energy)
	for _, part := range c.PutToLib {
		zone := "Battlefield"
		switch part.Zone {
		case state.ZHand:
			zone = "Hand"
		case state.ZGraveyard:
			zone = "Grave"
		}
		add("PutCardToLibFrom" + zone + "<" + strconv.FormatInt(int64(part.N), 10) + "/" +
			strconv.FormatInt(int64(part.LibraryPos), 10) + "/" + part.Spec + ">")
	}
	return string(b)
}

// CostPhrase renders a parsed cost for a PLAYER-FACING prompt or option
// label: Forge's non-mana heads become English, and the mana half stays in
// the card's own Forge notation ("2 B", "U") so it matches the card view's
// mana_cost (view/view.go) and the web's mana.ts symbol renderer. It is the
// deliberate sibling of formatCost: formatCost writes the raw whitespace-
// delimited token back out ("Sac<1/Creature.Other>") and is retained for
// the machine wire field decision.Option.Cost (manaActivationCostMarker).
//
// Forge embeds a human-readable "/description" on most non-mana tokens
// (Sac<1/Creature.Other/another creature>); costPhrase uses it verbatim when
// present (it is the card's own wording) and otherwise synthesizes a
// reasonable noun phrase from the head and spec. The fallback is
// deliberately not an oracle-text generator -- see CostPart.Desc.
//
// An empty Cost (or one whose only tokens were the payment-mode marker
// "Mandatory") renders as the empty string, so a caller that would
// otherwise emit "pay ?" can detect the no-cost case instead.
func CostPhrase(c Cost) string {
	var clauses []string
	if m := formatManaClause(c); m != "" {
		clauses = append(clauses, m)
	}
	if c.Tap {
		clauses = append(clauses, "tap this permanent")
	}
	if c.Untap {
		clauses = append(clauses, "untap this permanent")
	}
	if c.Life > 0 {
		clauses = append(clauses, "pay "+strconv.FormatInt(int64(c.Life), 10)+" life")
	}
	for range c.LifeX {
		clauses = append(clauses, "pay X life")
	}
	for _, part := range c.DamageYou {
		clauses = append(clauses, "take "+countPhrase(part.N)+" damage")
	}
	for _, part := range c.GainLife {
		clauses = append(clauses, gainLifeCostPhrase(part))
	}
	for _, part := range c.Sac {
		clauses = append(clauses, "sacrifice "+ObjectPhrase(part, "permanent"))
	}
	for _, part := range c.Discard {
		clauses = append(clauses, "discard "+ObjectPhrase(part, "card"))
	}
	for _, part := range c.Draw {
		if part.Dyn != "" {
			clauses = append(clauses, "draw X cards")
		} else {
			clauses = append(clauses, "draw "+countPhrase(part.N)+" card"+pluralSuffix(part.N))
		}
	}
	for _, part := range c.SubCounter {
		n := countPhrase(part.N)
		if part.Announced {
			n = "X"
		}
		clause := "remove " + n + " " + strings.ToUpper(part.Spec) + " counter" + pluralSuffix(part.N)
		if part.Target != "" && !SubCounterTargetsSource(part.Target) && part.Desc != "" {
			clause += " from " + part.Desc
		}
		clauses = append(clauses, clause)
	}
	for _, part := range c.AddCounter {
		kind := "loyalty"
		if !strings.EqualFold(part.Spec, "LOYALTY") {
			kind = strings.ToUpper(part.Spec)
		}
		clauses = append(clauses, "add "+countPhrase(part.N)+" "+kind+" counter"+pluralSuffix(part.N))
	}
	if len(c.Exert) > 0 {
		clauses = append(clauses, "exert it")
	}
	for _, part := range c.Exile {
		clauses = append(clauses, "exile "+ObjectPhrase(part, "card"))
	}
	for _, part := range c.ExileFromTop {
		// Top-of-library payment: prose must not imply the payer picks any
		// card from the library, only the top N in order.
		clauses = append(clauses, "exile the top "+strconv.FormatInt(int64(part.N), 10)+" card"+pluralSuffix(part.N)+" of your library")
	}
	for _, part := range c.MoveToGrave {
		clauses = append(clauses, "put "+ObjectPhrase(part, "card")+" from exile into its owner's graveyard")
	}
	for _, part := range c.Mill {
		clauses = append(clauses, "mill "+countPhrase(part.N)+" card"+pluralSuffix(part.N))
	}
	for _, part := range c.Evidence {
		n := countPhrase(part.N)
		if part.Dyn != "" {
			n = part.Dyn
		}
		clauses = append(clauses, "exile cards with total mana value "+n+" or greater from your graveyard")
	}
	for _, part := range c.Reveal {
		clauses = append(clauses, "reveal "+ObjectPhrase(part, "card"))
	}
	for _, part := range c.RevealOrChoose {
		if part.ChooseCard {
			clauses = append(clauses, "choose a creature you control or a warped creature card you own in exile")
		} else {
			clauses = append(clauses, "reveal "+ObjectPhrase(part, "card")+" or choose "+ObjectPhrase(part, "permanent")+" you control")
		}
	}
	for _, part := range c.RevealChosen {
		if strings.EqualFold(part.Spec, "Player") {
			clauses = append(clauses, "reveal the chosen player")
		} else {
			clauses = append(clauses, "reveal the chosen creature type")
		}
	}
	for _, part := range c.Behold {
		clauses = append(clauses, "behold "+ObjectPhrase(part, "card"))
	}
	for _, part := range c.TapPermanent {
		clauses = append(clauses, "tap "+ObjectPhrase(part, "permanent"))
	}
	for _, part := range c.Blight {
		if part.Announced {
			clauses = append(clauses, "blight X")
			continue
		}
		clauses = append(clauses, "blight "+countPhrase(part.N))
	}
	for _, part := range c.Return {
		clauses = append(clauses, "return "+ObjectPhrase(part, "permanent")+" to its owner's hand")
	}
	for _, part := range c.PutToLib {
		clauses = append(clauses, "put "+ObjectPhrase(part, "card")+" from your "+zoneNoun(part.Zone)+" into your library")
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			clauses = append(clauses, "pay X energy")
		} else {
			clauses = append(clauses, "pay "+countPhrase(part.N)+" energy")
		}
	}
	if c.Forage {
		clauses = append(clauses, "forage")
	}
	return joinClauses(clauses)
}

// formatManaClause renders the mana half of a cost in Forge notation with a
// "pay " prefix ("pay 2 B"). It is empty when the cost has no mana
// component, so costPhrase can omit the clause entirely rather than emit a
// bare "pay".
func formatManaClause(c Cost) string {
	var parts []string
	if c.Generic > 0 {
		parts = append(parts, strconv.FormatInt(int64(c.Generic), 10))
	}
	const faces = "WUBRGC"
	for i, face := range []byte(faces) {
		for n := int32(0); n < c.Colored[i]; n++ {
			parts = append(parts, string(face))
		}
	}
	for range c.X {
		parts = append(parts, "X")
	}
	for _, h := range c.Hybrid {
		parts = append(parts, string([]byte{h.A, '/', h.B}))
	}
	for _, t := range c.Twobrid {
		parts = append(parts, strconv.FormatInt(int64(t.Generic), 10)+"/"+string(t.Col))
	}
	for _, p := range c.Phyrexian {
		parts = append(parts, string([]byte{p, 'P'}))
	}
	for _, hp := range c.HybridPhyrexian {
		parts = append(parts, string([]byte{hp.A, '/', hp.B, '/', 'P'}))
	}
	for n := c.Snow; n > 0; n-- {
		parts = append(parts, "S")
	}
	if len(parts) == 0 {
		return ""
	}
	return "pay " + strings.Join(parts, " ")
}

// ObjectPhrase renders one non-mana cost part's noun: Forge's embedded
// /description verbatim when the corpus supplies one (the card's own
// wording), else the count as digits plus a noun derived from the spec's
// leading type word or the head's default. An announced part (Sac<X>,
// SubCounter<X>) renders its "X" count. On the description path a count of
// one is dropped, so "another creature" is not prefixed with "1 ".
func ObjectPhrase(part CostPart, defNoun string) string {
	count := ""
	if part.Announced {
		count = "X "
	} else if part.N != 1 || part.Desc == "" {
		// The synthesized path keeps the count as digits (even 1), per the
		// brief; only the embedded-description path drops a count of one,
		// so "another creature" is not prefixed with "1 ".
		count = strconv.FormatInt(int64(part.N), 10) + " "
	}
	if part.Desc != "" {
		// The corpus description already names the object ("another
		// creature", "this artifact"); only an explicit count > 1 needs the
		// number in front of it.
		if part.N != 1 && !part.Announced {
			return count + part.Desc
		}
		return part.Desc
	}
	return count + SpecNoun(part.Spec, defNoun)
}

// SpecNoun derives a readable noun from a cost spec's leading type word
// ("Creature.Other" -> "creature"). It is the fallback only: the corpus's
// embedded /description wins wherever it exists (objectPhrase). An unknown
// base word falls back to the head's own default noun rather than echoing
// raw filter syntax at the player.
func SpecNoun(spec, defNoun string) string {
	base := spec
	if i := strings.IndexAny(base, ".+, ;"); i >= 0 {
		base = base[:i]
	}
	if v, ok := specNounTab.Get(strings.ToLower(base)); ok {
		return v
	}
	return defNoun
}

// zoneNoun names the origin zone a PutCardToLib part pays from, for the
// prose phrase.
func zoneNoun(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "hand"
	case state.ZGraveyard:
		return "graveyard"
	default:
		return "battlefield"
	}
}

// countPhrase renders a cost part's count as digits ("2"), or "X" for an
// announced part. No number-to-word table: the brief pins digits.
func countPhrase(n int32) string {
	return strconv.FormatInt(int64(n), 10)
}

// gainLifeCostPhrase renders one GainLife<N/Player...> part as player-facing
// prose: "an opponent gains N life" for the bare Player.Opponent form and
// "each other player gains N life" for the /* marker (Reverent Silence,
// Skyshroud Cutter). An unrecognised player word falls back to the generic
// "a player" rather than echoing raw Forge filter syntax.
func gainLifeCostPhrase(part CostPart) string {
	who := "a player"
	switch gainLifeCostPhraseCodes.Code(string(strings.TrimSpace(part.Spec))) {
	case gainLifeCostPhrasePlayerOpponent:
		who = "an opponent"
	case gainLifeCostPhrasePlayerOther:
		if part.Each {
			who = "each other player"
		} else {
			who = "another player"
		}
	}
	verb := "gains"
	if who == "each other player" {
		verb = "gain"
	}
	return who + " " + verb + " " + countPhrase(part.N) + " life"
}

// pluralSuffix returns "s" for a count that is not exactly one.
func pluralSuffix(n int32) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// joinClauses joins prose cost clauses the way a sentence reads: "a", "a
// and b", "a, b and c".
func joinClauses(clauses []string) string {
	switch len(clauses) {
	case 0:
		return ""
	case 1:
		return clauses[0]
	case 2:
		return clauses[0] + " and " + clauses[1]
	default:
		return strings.Join(clauses[:len(clauses)-1], ", ") + " and " + clauses[len(clauses)-1]
	}
}

// CapitaliseFirst upper-cases the first rune, for an option label built from
// a costPhrase.
func CapitaliseFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// ManaCostBeyondTap reports whether paying this cost takes anything beyond
// a bare tap: any mana pip (generic, coloured, X, snow, hybrid, Phyrexian,
// twobrid) or any non-mana component (HasNonMana). A bare-tap mana ability
// (every plain land's) and a tapless free one report false — exactly the
// windows the client's empty-priority-window floor may pass away unseen.
func ManaCostBeyondTap(c Cost) bool {
	c.Tap = false
	c.Untap = false
	if c.Generic > 0 || c.X > 0 || c.Snow > 0 || c.Colored != (state.Mana{}) {
		return true
	}
	if len(c.Hybrid) > 0 || len(c.Phyrexian) > 0 || len(c.Twobrid) > 0 || len(c.HybridPhyrexian) > 0 {
		return true
	}
	return c.HasNonMana()
}

var specNounTab = state.NewStrTable[string](
	state.StrEntry[string]{Key: "creature", Val: "creature"},
	state.StrEntry[string]{Key: "artifact", Val: "artifact"},
	state.StrEntry[string]{Key: "enchantment", Val: "enchantment"},
	state.StrEntry[string]{Key: "land", Val: "land"},
	state.StrEntry[string]{Key: "planeswalker", Val: "planeswalker"},
	state.StrEntry[string]{Key: "permanent", Val: "permanent"},
	state.StrEntry[string]{Key: "card", Val: "card"},
	state.StrEntry[string]{Key: "token", Val: "token"},
)

type gainLifeCostPhraseCode uint16

const (
	gainLifeCostPhrasePlayerOpponent gainLifeCostPhraseCode = iota + 1
	gainLifeCostPhrasePlayerOther
)

var gainLifeCostPhraseCodes = state.NewStrCodes(
	state.StrEntry[gainLifeCostPhraseCode]{Key: "Player.Opponent", Val: gainLifeCostPhrasePlayerOpponent},
	state.StrEntry[gainLifeCostPhraseCode]{Key: "Player.Other", Val: gainLifeCostPhrasePlayerOther},
)
