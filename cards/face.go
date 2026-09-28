package cards

import (
	"slices"
	"strconv"
	"strings"
)

// manaBraceForm normalises a brace-form mana cost ("{2}{U}{U}") to the
// space-separated form cmcFromManaCost parses. Built once at package scope,
// not per call, because derive runs on every face of the corpus at load.
var manaBraceForm = strings.NewReplacer("{", " ", "}", " ")

func (f *Face) hasType(t string) bool {
	if mask := typeMaskFor(t); mask != 0 && f.compiledCatalog != nil && f.compiledID != 0 && int(f.compiledID) <= len(f.compiledCatalog.Faces) {
		return f.compiledCatalog.Faces[f.compiledID-1].TypeMask&mask != 0
	}
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

func (f *Face) IsLand() bool         { return f.hasType("Land") }
func (f *Face) IsBasic() bool        { return f.hasType("Basic") }
func (f *Face) IsLegendary() bool    { return f.hasType("Legendary") }
func (f *Face) IsWorld() bool        { return f.hasType("World") }
func (f *Face) IsCreature() bool     { return f.hasType("Creature") }
func (f *Face) IsInstant() bool      { return f.hasType("Instant") }
func (f *Face) IsSorcery() bool      { return f.hasType("Sorcery") }
func (f *Face) IsArtifact() bool     { return f.hasType("Artifact") }
func (f *Face) IsSpacecraft() bool   { return f.hasType("Spacecraft") }
func (f *Face) IsVehicle() bool      { return f.hasType("Vehicle") }
func (f *Face) IsEnchantment() bool  { return f.hasType("Enchantment") }
func (f *Face) IsPlaneswalker() bool { return f.hasType("Planeswalker") }
func (f *Face) IsBattle() bool       { return f.hasType("Battle") }
func (f *Face) IsRoom() bool         { return f.hasType("Room") }

// IsPermanent reports whether resolving this face puts it onto the battlefield.
func (f *Face) IsPermanent() bool { return !f.IsInstant() && !f.IsSorcery() }

// KeywordHead strips a keyword's parameters: "Equip:2" is the Equip keyword.
// Keywords whose name contains spaces ("Protection from blue") keep them.
func KeywordHead(k string) string {
	if i := strings.IndexByte(k, ':'); i >= 0 {
		k = k[:i]
	}
	return strings.TrimSpace(k)
}

// SplitKeywordList parses Forge's ampersand-joined keyword-list grammar.
// StaticAbilityContinuous.java splits AddKeyword$ on " & ", and Pump's KW$
// uses the same form: "Vigilance & Lifelink" is two keywords. A comma is NOT
// a list separator. Keyword parameters use commas themselves, for example
// "Protection:Spell.Instant,Spell.Sorcery:..." and
// "OnlyUntapChosen:Artifact,Creature,Land", and must stay one member.
//
// Whitespace around each member is trimmed, empty members are dropped, and an
// absent or empty list yields nil. Every implemented keyword-list reader uses
// this function; type-list parsing is deliberately separate because it has a
// different Forge grammar.
func SplitKeywordList(list string) []string {
	var out []string
	for part := range strings.SplitSeq(list, "&") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (f *Face) HasKeyword(k string) bool { return f.HasKeywordID(k, 0) }

// HasKeywordID is HasKeyword with k's precompiled InternKeywordHead ordinal:
// the same compiled-mask gate, then the keyword-line scan answered from the
// face's interned head bitset when id is nonzero.
func (f *Face) HasKeywordID(k string, id KeywordHeadID) bool {
	if f.compiledCatalog != nil && f.compiledID != 0 && int(f.compiledID) <= len(f.compiledCatalog.Faces) {
		// No keyword lines and no compiled keyword bits: both paths below
		// answer false, so skip the head's mask lookup.
		if len(f.Keywords) == 0 && f.compiledCatalog.Faces[f.compiledID-1].KeywordMask == 0 {
			return false
		}
		if mask := keywordMaskFor(k); mask != 0 {
			return f.compiledCatalog.Faces[f.compiledID-1].KeywordMask&mask != 0
		}
	}
	return f.KeywordLinesHaveHead(k, id)
}

// KeywordParam returns the text after the colon of a parameterised keyword
// ("Kicker:B" -> "B"; "Equip:2" -> "2") and reports whether the keyword is
// printed at all ("Flash" -> "", true; absent -> "", false).
func (f *Face) KeywordParam(head string) (string, bool) {
	// A face with no keyword lines answers "absent" on both paths below (the
	// compiled mask gate and the line scan); deciding it first skips the
	// head's mask lookup, which most calls (every land, most spells) paid.
	if len(f.Keywords) == 0 {
		return "", false
	}
	if mask := keywordMaskFor(head); mask != 0 && f.compiledCatalog != nil && f.compiledID != 0 && int(f.compiledID) <= len(f.compiledCatalog.Faces) && f.compiledCatalog.Faces[f.compiledID-1].KeywordMask&mask == 0 {
		return "", false
	}
	for _, k := range f.Keywords {
		if strings.EqualFold(KeywordHead(k), head) {
			if i := strings.IndexByte(k, ':'); i >= 0 {
				return strings.TrimSpace(k[i+1:]), true
			}
			return "", true
		}
	}
	return "", false
}

// SpellAbility is the SP$ ability a card casts with, if any.
func (f *Face) SpellAbility() *SA {
	if f.compiledCatalog != nil && f.compiledID != 0 && int(f.compiledID) <= len(f.compiledCatalog.Faces) {
		id := f.compiledCatalog.Faces[f.compiledID-1].SpellAbility
		if id == 0 {
			return nil
		}
		if int(id) <= len(f.compiledCatalog.abilityPointers) {
			return f.compiledCatalog.abilityPointers[id-1]
		}
	}
	for _, a := range f.Abilities {
		if a.Kind == "SP" {
			return a
		}
	}
	return nil
}

// ManaAbilities lists every activated ability that produces mana.
func (f *Face) ManaAbilities() []*SA {
	if f.compiledCatalog != nil && f.compiledID != 0 && int(f.compiledID) <= len(f.compiledCatalog.Faces) {
		span := f.compiledCatalog.Faces[f.compiledID-1].ManaAbilities
		end := uint64(span.Start) + uint64(span.Count)
		if end <= uint64(len(f.compiledCatalog.ManaAbilityIDs)) {
			if span.Count == 0 {
				return nil
			}
			// The precomputed pointer span, when every entry resolved, is
			// returned clipped (an append by the caller reallocates, never
			// writing the shared catalog slice).
			if ptrs := f.compiledCatalog.manaAbilityPointers; end <= uint64(len(ptrs)) {
				sp := ptrs[span.Start:uint32(end):uint32(end)]
				if !slices.Contains(sp, nil) {
					return sp
				}
			}
			out := make([]*SA, 0, span.Count)
			for _, id := range f.compiledCatalog.ManaAbilityIDs[span.Start:uint32(end)] {
				if id == 0 || int(id) > len(f.compiledCatalog.abilityPointers) {
					return f.textualManaAbilities()
				}
				out = append(out, f.compiledCatalog.abilityPointers[id-1])
			}
			return out
		}
	}
	return f.textualManaAbilities()
}

func (f *Face) textualManaAbilities() []*SA {
	var out []*SA
	for _, a := range f.Abilities {
		if a.Kind == "AB" && a.API == "Mana" {
			out = append(out, a)
		}
	}
	return out
}

// ManaReflectedAbilities lists every reflected-mana activated ability (AB$
// ManaReflected) on the face, in printed order. It is the ManaAbilities
// sibling the per-face ManaProduction fold needs: a reflected source's
// colours are computed at resolution from the objects its Valid$ selector
// names, so it is not an AB$ Mana ability (ManaAbilities deliberately lists
// only those), but it IS a mana ability the collector must account for
// (ManaProduction.addReflected), or a reflected source projects as producing
// nothing. The compiled span keeps the parity/performance shape ManaAbilities
// uses, with the textual walk as the unbounded fallback.
func (f *Face) ManaReflectedAbilities() []*SA {
	if f.compiledCatalog != nil && f.compiledID != 0 && int(f.compiledID) <= len(f.compiledCatalog.Faces) {
		span := f.compiledCatalog.Faces[f.compiledID-1].Abilities
		end := uint64(span.Start) + uint64(span.Count)
		if end <= uint64(len(f.compiledCatalog.FaceAbilities)) {
			var out []*SA
			for _, id := range f.compiledCatalog.FaceAbilities[span.Start:uint32(end)] {
				if id == 0 || int(id) > len(f.compiledCatalog.abilityPointers) {
					return f.textualManaReflectedAbilities()
				}
				if a := f.compiledCatalog.abilityPointers[id-1]; a != nil && a.Kind == "AB" && a.API == "ManaReflected" {
					out = append(out, a)
				}
			}
			return out
		}
	}
	return f.textualManaReflectedAbilities()
}

func (f *Face) textualManaReflectedAbilities() []*SA {
	var out []*SA
	for _, a := range f.Abilities {
		if a.Kind == "AB" && a.API == "ManaReflected" {
			out = append(out, a)
		}
	}
	return out
}

func (f *Face) Power() int     { return int(f.power) }
func (f *Face) Toughness() int { return int(f.toughness) }

// Cmc returns the face's converted mana cost, derived once at load from the
// printed ManaCost string. It mirrors botpolicy.CmcOf's arithmetic exactly
// (cards cannot import botpolicy or rules, so the few lines are duplicated
// here by design) so a face read the same way anywhere agrees. {X} counts as
// 0 off the stack; ordinary hybrid/Phyrexian/colourless symbols count one,
// while a monocolour hybrid such as {2/W} counts its generic face (two).
func (f *Face) Cmc() int32 { return f.cmc }

// CharacteristicDefining reports whether the face's printed P/T is a
// characteristic-defining value ("*", "1+*"): Power()/Toughness() return 0
// for these and layer 7a (in rules) supplies the real value.
func (f *Face) CharacteristicDefining() bool { return f.characteristicDefining }

// AllCreatureTypesCDA reports whether the face prints its own
// characteristic-defining "is every creature type" ability — the
// AddAllCreatureTypes$ True static on a CharacteristicDefining$ True,
// Affected$ Self line (Mistform Ultimus). This is the intrinsic CDA sibling
// of Changeling's keyword: like Changeling, it is answered by the type
// filter's positive subtype vocabulary (effects' changelingType), never by
// materialising hundreds of subtypes into the derived type list. Derived
// once at load; a granted static (Maskwood Nexus's Affected$ Creature.YouCtrl)
// does not set it — grants stay in the rules layer walk.
func (f *Face) AllCreatureTypesCDA() bool { return f.allCreatureTypesCDA }

// Colour identity is a bitmask over the five colours packed into one byte. A
// bitmask is the natural representation: identity is used as a set-membership
// question ("commander identity must be a superset of this card's identity")
// and a subset test is a single bitwise-and versus zero, it is order
// independent (so scanning fields in any order can never couple identity to
// iteration order, which determinism forbids elsewhere), and one byte is the
// smallest thing a Commander subset check needs. The identity itself is the
// CR 903.5 notion, not the card's colour: a card with no mana cost can still
// have an identity, and a colourless artifact whose activated ability costs
// {R} is red-identity.
const (
	ColourWhite uint8 = 1 << iota
	ColourBlue
	ColourBlack
	ColourRed
	ColourGreen
)

// ColourIdentity returns the face's colour identity, derived once at load
// from its mana cost, colour indicator (Colors:) and the mana symbols in its
// rules text. See deriveColourIdentity for exactly which fields contribute.
func (f *Face) ColourIdentity() uint8 { return f.colourIdentity }

// derive computes the derived fields from the printed text fields. It must
// run after every path that constructs a Face values its printed fields from
// text — after ParseBytes and after the gob decode path — so the two
// construction routes produce identical faces. It is never run into the gob:
// the derived fields stay unexported (gob ignores them) and are recomputed on
// decode, so a stale cache whose gob zero-filled them is repaired with no
// error anywhere.
func (f *Face) derive() {
	f.power, f.toughness, f.characteristicDefining = parsePT(f.PT)
	f.allCreatureTypesCDA = cdaAllCreatureTypes(f.Statics)
	f.cmc = cmcFromManaCost(f.ManaCost)
	f.manaProduction = ManaProduction{}
	for _, a := range f.ManaAbilities() {
		f.manaProduction.add(a)
	}
	for _, a := range f.ManaReflectedAbilities() {
		f.manaProduction.addReflected(a)
	}
	f.colourIdentity = f.deriveColourIdentity()
	f.deriveTypeStatics()
	f.deriveWordSets()
}

// deriveColourIdentity computes the face's colour identity the way CR 903.4
// does and Forge's own algorithm with it: the colours of the mana symbols in
// the mana cost, plus the colour indicator (Forge's Colors:, present only on
// the cards whose colour a viewer cannot otherwise infer — Dryad Arbor is
// green but casts for no cost), plus every brace-delimited mana symbol in the
// Oracle rules text, plus the colour a characteristic-defining ability sets
// (SetColor$ All on Transguild Courier's "CARDNAME is all colors").
//
// Everything else is deliberately NOT scanned, and this is the point of the
// derivation: CR 903.4 reads the printed mana cost and the printed rules text
// and nothing else, so the sources are exactly Oracle (which carries that
// rules text, keyword costs included — Lingering Souls' "Flashback {1}{B}")
// and the two structured colour fields. Raw ability/param/SVar tokens are not
// rules text: scanning them invented identity from SVar names and SVar
// parameters that merely happen to spell a colour letter (Pox's
// "Amount$ G" ⇒ green; First Family's WUBRG SVar names ⇒ five colours) and
// from reminder text quoted in Description$ params (Trinisphere's "a spell
// that would cost {1}{B}" ⇒ black), three classes CR 903.4c excludes with the
// reminder text.
//
// Parenthesised Oracle text is skipped: it is where reminder text lives
// (CR 207.2/903.4c), so a basic land's overarching "({T}: Add {R}.)" — a
// basic land has empty identity by design, its deck legality coming instead
// from the CR 903.5d could-produce rule the deck validator enforces — and
// Trinisphere's example text contribute nothing. Measured across the corpus,
// the only real rules text the skip drops is carried by the mana-ability
// blocks of typed lands (duals, snow duals, Dryad Arbor: every one has a
// basic land type, so 903.5d covers its colours in deck construction) and by
// quoted token/ability costs whose colours the card's own mana cost or colour
// indicator already carries (Zhao, Disa, Gobland); Jasconian Isle — whose
// parenthesised CDA text says "it's blue" but whose script colours it
// colourless — is the one known residual and is named in the derive report.
func (f *Face) deriveColourIdentity() uint8 {
	var m uint8
	m |= manaColours(f.ManaCost)   // the printed mana cost
	m |= colourIndicator(f.Colors) // the colour indicator
	m |= oracleColours(f.Oracle)   // brace-delimited mana symbols in rules text, reminder text skipped
	m |= cdaSetColours(f.Statics)  // a characteristic-defining ability's own SetColor$ ("CARDNAME is all colors")
	return m
}

// cdaSetColours folds in the SetColor$ of every characteristic-defining
// continuous ability that affects the face itself: Transguild Courier and
// Sphinx of the Guildpact's "CARDNAME is all colors" (SetColor$ All) is the
// CR 903.4 "colors defined by its characteristic-defining abilities" class
// with no mana symbol anywhere to scan, so without it both cards would read
// colourless and be legal in every commander deck. "All" is the five colours;
// a named colour contributes itself; "Colorless" and a commander-only
// "ChosenColor" (CR 903.4b's before-the-game choice — Faceless One,
// The Prismatic Piper) contribute nothing here. A static that merely affects
// OTHER permanents (Leyline of the Guildpact makes your permanents all
// colours but is itself green) or that is not characteristic-defining
// (Fallaji Wayfarer's "doesn't affect its color identity") is not the card's
// identity and must be ignored.
// cdaAllCreatureTypes reports whether the face's own statics carry a
// characteristic-defining AddAllCreatureTypes$ True affecting the card
// itself. Mirrors cdaSetColours' gate shape: the static must be a Continuous
// one marked CharacteristicDefining$ True and must name Self in its Affected$
// (an empty Affected$ counts as self-scoped for a printed CDA; a static that
// affects OTHER permanents — Maskwood Nexus's Affected$ Creature.YouCtrl
// grant, handled by the rules layer walk — is not the card's own type and
// must not set the intrinsic flag).
func cdaAllCreatureTypes(sts []Static) bool {
	for _, s := range sts {
		if s.Mode != "Continuous" || !strings.EqualFold(strings.TrimSpace(s.Params["CharacteristicDefining"]), "True") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(s.Params["AddAllCreatureTypes"]), "True") {
			continue
		}
		if aff := strings.TrimSpace(s.Params["Affected"]); aff != "" && !strings.Contains(aff, "Self") {
			continue
		}
		return true
	}
	return false
}

// CommanderColourChoiceCDA reports whether the face carries the CR 903.4b
// "if CARDNAME is your commander, choose a color before the game begins"
// characteristic-defining ability: a CharacteristicDefining$ True continuous
// static affecting Self whose SetColor$ names the chosen colour. It is the
// SAME gate cdaSetColours applies (and the one rules' layer-5 scan resolves
// through resolveChosenColors), exported so the pregame ask in rules/ and the
// identity derivation here cannot drift on what qualifies a commander for the
// choice. cdaSetColours contributes nothing for the value at load time -- the
// choice does not exist yet -- so this predicate is what the pregame round
// keys on.
func (f *Face) CommanderColourChoiceCDA() bool {
	for _, s := range f.Statics {
		if s.Mode != "Continuous" || !strings.EqualFold(strings.TrimSpace(s.Params["CharacteristicDefining"]), "True") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(s.Params["SetColor"]), "ChosenColor") {
			continue
		}
		if aff := strings.TrimSpace(s.Params["Affected"]); aff != "" && !strings.Contains(aff, "Self") {
			continue
		}
		return true
	}
	return false
}

func cdaSetColours(sts []Static) uint8 {
	var m uint8
	for _, s := range sts {
		if s.Mode != "Continuous" || s.Params["CharacteristicDefining"] != "True" {
			continue
		}
		if !strings.Contains(s.Params["Affected"], "Self") {
			continue
		}
		switch strings.ToLower(s.Params["SetColor"]) {
		case "all":
			m |= ColourWhite | ColourBlue | ColourBlack | ColourRed | ColourGreen
		case "white":
			m |= ColourWhite
		case "blue":
			m |= ColourBlue
		case "black":
			m |= ColourBlack
		case "red":
			m |= ColourRed
		case "green":
			m |= ColourGreen
		}
	}
	return m
}

// oracleColours scans Oracle rules text for brace-delimited mana symbols and
// returns their colour bitmask. Only brace content counts — prose whose words
// carry an uppercase colour initial ("a White Spirit token") is not a mana
// symbol — and parenthesised text is skipped outright, because that is where
// reminder text lives (CR 207.2) and CR 903.4c ignores it. Within a brace
// token every colour letter contributes, so hybrid ({W/U}), Phyrexian ({B/P})
// and monocolour-hybrid ({2/B}) forms all land their colour letters exactly
// as manaColours would read them off a printed cost.
func oracleColours(oracle string) uint8 {
	var m uint8
	depth := 0
	start := -1 // index of the '{' of the brace token being collected, -1 if none
	for i := 0; i < len(oracle); i++ {
		switch oracle[i] {
		case '(': // reminder text (and any other parenthesised asides) starts
			depth++
			start = -1
		case ')':
			if depth > 0 {
				depth--
			}
		case '{':
			if depth == 0 { // a brace token's content, not reminder text
				start = i + 1
			}
		case '}':
			if depth == 0 && start >= 0 {
				m |= manaColours(oracle[start:i])
				start = -1
			}
		}
	}
	return m
}

// colourIndicator maps a Colors: value ("black", "white,blue", "colorless")
// to its colour bitmask. Colourless contributes nothing; unknown names are
// ignored.
func colourIndicator(s string) uint8 {
	var m uint8
	for part := range strings.SplitSeq(s, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "white":
			m |= ColourWhite
		case "blue":
			m |= ColourBlue
		case "black":
			m |= ColourBlack
		case "red":
			m |= ColourRed
		case "green":
			m |= ColourGreen
		}
	}
	return m
}

// isManaCh reports whether c can form part of a mana symbol as Forge writes
// them: the colour letters, C/S colourless and snow, P Phyrexian, a slash
// (hybrid "2/B", "W/U") and the digits of a generic cost. Tap is deliberately
// excluded: it is not part of a mana symbol, and Forge's packed comparison
// operators (for example GT0) would otherwise be misread as green mana.
// Because
// lowercase letters are never part of a written pip, matching only these lets
// a prose word keep its uppercase
// initial without it being read as a colour.
func isManaCh(c byte) bool {
	switch c {
	case 'W', 'U', 'B', 'R', 'G', 'C', 'P', 'S', '/',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func isAlphaNum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// manaColours scans s for colour-contributing mana symbols and returns their
// bitmask. A symbol is a run of isManaCh characters bounded on both sides by
// a non-alphanumeric, so neither the tail of a word nor an uppercase initial
// inside one ("White", "Swamp", the "CARD" in CARDNAME) is misread as a pip.
// Only the WUBRG letters contribute: C (colourless), S (snow), P (Phyrexian),
// numerics and slashes never add a colour, matching CR. Hybrid (WU, W/U) and
// Phyrexian (WP, UP, RP) forms contribute their colour letters and nothing
// else. The same scanner runs over the ManaCost and every rules-text field so
// a pip counts wherever it appears — in the cost, in a Cost$, in a Produced$,
// in a trigger, in an SVar.
func manaColours(s string) uint8 {
	var m uint8
	n := len(s)
	i := 0
	for i < n {
		// Advance to a run start: a mana char whose left neighbour is not
		// alphanumeric, so it cannot be the tail of a longer word.
		for i < n && !(isManaCh(s[i]) && (i == 0 || !isAlphaNum(s[i-1]))) {
			i++
		}
		if i >= n {
			break
		}
		var cm uint8
		j := i
		for j < n && isManaCh(s[j]) {
			switch s[j] {
			case 'W':
				cm |= ColourWhite
			case 'U':
				cm |= ColourBlue
			case 'B':
				cm |= ColourBlack
			case 'R':
				cm |= ColourRed
			case 'G':
				cm |= ColourGreen
			}
			j++
		}
		// The run must also be a whole token, not the head of a longer prose
		// word ("White"): require a non-alphanumeric (or end) right after it.
		if j >= n || !isAlphaNum(s[j]) {
			m |= cm
		}
		i = j
	}
	return m
}

// parsePT splits a printed P/T ("2/2") into power and toughness. A face with
// no P/T yields 0,0 and flag false; a face whose P/T carries a
// characteristic-defining value ("*", "1+*") yields 0 for the affected side
// (exactly what pt used to return) and sets the flag, because layer 7a owns
// that value.
func parsePT(pt string) (pow, tgh int32, cd bool) {
	parts := strings.SplitN(pt, "/", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	for i, s := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			cd = true
			continue // 0 for this side; layer 7a owns characteristic-defining values
		}
		if i == 0 {
			pow = int32(n)
		} else {
			tgh = int32(n)
		}
	}
	return pow, tgh, cd
}

// cmcFromManaCost is cards' own conversion of a printed ManaCost string to a
// converted mana cost, an exact mirror of botpolicy.CmcOf. It deliberately
// re-derives rules/mana.go's ParseCost.CMC() by hand here because cards can
// import neither botpolicy nor rules.
func (f *Face) ManaValue() int32 { return cmcFromManaCost(f.ManaCost) }

func cmcFromManaCost(mc string) int32 {
	mc = manaBraceForm.Replace(mc)
	mc = strings.TrimSpace(mc)
	if mc == "" || strings.EqualFold(mc, "no cost") {
		return 0
	}
	var n int32
	for sym := range strings.FieldsSeq(mc) {
		if sym == "X" { // {X} is 0 off the stack
			continue
		}
		if len(sym) == 1 && strings.ContainsRune("WUBRGC", rune(sym[0])) { // a single coloured/colourless pip
			n++
			continue
		}
		if v, err := strconv.Atoi(sym); err == nil && v >= 0 {
			n += int32(v)
			continue
		}
		if v, ok := twobridManaValue(sym); ok {
			// CR 202.4b: a monocolour hybrid's mana value is its generic
			// face. {2/W} is mana value 2, whether it is eventually paid
			// with two mana or one white mana.
			n += v
			continue
		}
		// Hybrid ("W/U"), Phyrexian ("UP"), and any other symbol: one generic.
		n++
	}
	return n
}

// Mentions reports whether any script text on the face contains needle: every
// SVar value body, every reachable ability parameter (abilities, their
// SubAbility$ chains, trigger effects and replacement bodies), every static
// parameter and every keyword string. It is the structural reader for
// provenance gates that ask "does this face read X" -- a value search over
// the whole script, not a keyed parameter read -- so it lives with the IR it
// walks rather than in the key-census-scanned rule packages.
func (f *Face) Mentions(needle string) bool {
	if f == nil || needle == "" {
		return false
	}
	for _, v := range f.SVars {
		if strings.Contains(v, needle) {
			return true
		}
	}
	for _, k := range f.Keywords {
		if strings.Contains(k, needle) {
			return true
		}
	}
	var walkSA func(sa *SA, depth int) bool
	walkSA = func(sa *SA, depth int) bool {
		if sa == nil || depth > 32 {
			return false
		}
		for _, v := range sa.Params {
			if strings.Contains(v, needle) {
				return true
			}
		}
		return walkSA(sa.Sub, depth+1)
	}
	for _, a := range f.Abilities {
		if walkSA(a, 0) {
			return true
		}
	}
	for _, tr := range f.Triggers {
		for _, v := range tr.Params {
			if strings.Contains(v, needle) {
				return true
			}
		}
		if walkSA(tr.Effect, 0) {
			return true
		}
	}
	for _, r := range f.Repls {
		for _, v := range r.Params {
			if strings.Contains(v, needle) {
				return true
			}
		}
		if walkSA(r.With, 0) {
			return true
		}
	}
	for _, s := range f.Statics {
		for _, v := range s.Params {
			if strings.Contains(v, needle) {
				return true
			}
		}
	}
	return false
}

// twobridManaValue recognises Forge's concatenated ("2W") and slash
// ("2/W") monocolour-hybrid spellings. It returns the generic face, which is
// the symbol's mana value. This mirrors rules.ParseCost's twobrid parser
// without importing rules (cards sits to its left in the dependency graph).
func twobridManaValue(sym string) (int32, bool) {
	generic, col := "", ""
	if left, right, ok := strings.Cut(sym, "/"); ok {
		generic, col = left, right
	} else {
		i := 0
		for i < len(sym) && sym[i] >= '0' && sym[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, false
		}
		generic, col = sym[:i], sym[i:]
	}
	if len(col) != 1 || !strings.ContainsRune("WUBRGC", rune(col[0])) {
		return 0, false
	}
	v, err := strconv.ParseInt(generic, 10, 32)
	if err != nil || v < 0 {
		return 0, false
	}
	return int32(v), true
}
