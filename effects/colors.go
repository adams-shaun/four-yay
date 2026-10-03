package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

type ColorMask uint8

var colorMaskStrings = [32]string{
	"", "W", "U", "WU", "B", "WB", "UB", "WUB",
	"R", "WR", "UR", "WUR", "BR", "WBR", "UBR", "WUBR",
	"G", "WG", "UG", "WUG", "BG", "WBG", "UBG", "WUBG",
	"RG", "WRG", "URG", "WURG", "BRG", "WBRG", "UBRG", "WUBRG",
}

func colorBit(c byte) ColorMask {
	switch c {
	case 'W':
		return 1 << 0
	case 'U':
		return 1 << 1
	case 'B':
		return 1 << 2
	case 'R':
		return 1 << 3
	case 'G':
		return 1 << 4
	default:
		return 0
	}
}

func (m ColorMask) String() string {
	return colorMaskStrings[m&31]
}

// ColorsOf is an object's colours as WUBRG letters in that fixed order:
// the colours of its mana cost, or an explicit Colors: line for a card
// whose cost does not show them (a token, an artifact "that is green").
// Devoid makes a card colourless regardless (CR 702.114). A Face-less
// object (an ability, a copy of nothing) is colourless. Protection (rules)
// and the colour predicates read this rather than the face directly.
func ColorsOf(o *state.Object) string {
	return ColorMaskOf(o).String()
}

// ColorMaskOf is ColorsOf's compact representation for rules hot paths that
// combine printed colours with continuous effects before rendering WUBRG.
func ColorMaskOf(o *state.Object) ColorMask {
	if o == nil {
		return 0
	}
	f := o.Face()
	if f == nil || f.HasKeyword("Devoid") {
		return 0
	}
	var mask ColorMask
	for i := 0; i < len(f.ManaCost); i++ {
		mask |= colorBit(f.ManaCost[i])
	}
	if mask == 0 && f.Colors != "" {
		for rest := f.Colors; ; {
			word := rest
			if comma := strings.IndexByte(rest, ','); comma >= 0 {
				word, rest = rest[:comma], rest[comma+1:]
			} else {
				rest = ""
			}
			word = strings.TrimSpace(word)
			switch {
			case strings.EqualFold(word, "white"):
				mask |= 1 << 0
			case strings.EqualFold(word, "blue"):
				mask |= 1 << 1
			case strings.EqualFold(word, "black"):
				mask |= 1 << 2
			case strings.EqualFold(word, "red"):
				mask |= 1 << 3
			case strings.EqualFold(word, "green"):
				mask |= 1 << 4
			}
			if rest == "" {
				break
			}
		}
	}
	// CR 604.3/208.2: a characteristic-defining ability works in EVERY zone,
	// so a resolvable self SetColor$ CDA overwrites the printed colours here
	// -- the same read that governs the battlefield layer-5 base (rules'
	// derivedWith) and every off-battlefield consumer of this function.
	// SetColor$ overwrites, it never extends (CR 613.1e), so the claim
	// REPLACES the mask. rules' staticEffects withholds exactly the claim
	// this helper resolves from its layer-5 scan emission (the P/T CDA skip
	// discipline), so the battlefield read applies it once, and the
	// off-battlefield reads (hand, stack, graveyard, library) get it too --
	// Transguild Courier is all colours wherever it is, and Ghostfire is
	// colourless ("" is a real overwrite, not a no-claim). The Devoid
	// early-return above stays first: no corpus card carries both (measured
	// over the corpus's CharacteristicDefining SetColor$ carriers), so their
	// relative order is unmeasured and a combined card would need its own
	// reading.
	if claim, ok := CDASetColourClaim(f); ok {
		mask = claim
	}
	return mask
}

// cdaSetColourClaimStatic reports one static's colour claim: (mask, isCDA,
// ok). isCDA means the static passes the exact cdaSetColours gate cards'
// colour-identity derivation uses -- Mode$ Continuous,
// CharacteristicDefining$ True (exact case, the spelling every corpus CDA
// carrier prints), Affected$ naming Self -- and carries a SetColor$ value;
// ok then reports whether effects.ColorLetters parsed the value whole. The
// two results are separate because the callers want different things:
// ColorMaskOf acts only on (isCDA && ok) and keeps the printed colours
// otherwise, while rules' staticEffects must distinguish "a resolvable CDA I
// withhold from the scan" (isCDA && ok) from "a CDA static I keep emitting
// like any other static" (isCDA && !ok -- the ChosenColor family's fail-closed
// arm, where today's scan behaviour is no emission either way, but the shape
// stays honest) from "not a CDA at all" (!isCDA).
func cdaSetColourClaimStatic(s cards.Static) (ColorMask, bool, bool) {
	if s.Mode != "Continuous" || s.ParamStr(cards.PKCharacteristicDefining) != "True" {
		return 0, false, false
	}
	if !strings.Contains(s.Params["Affected"], "Self") {
		return 0, false, false
	}
	raw, isSet := s.Params["SetColor"]
	if !isSet {
		return 0, false, false
	}
	letters, ok := ColorLetters(raw)
	if !ok {
		return 0, true, false
	}
	var mask ColorMask
	for _, l := range letters {
		mask |= colorBit(l[0])
	}
	return mask, true, true
}

// CDASetColourClaimStatic is cdaSetColourClaimStatic for rules' static scan,
// so the scan's withholding gate and this package's ColorMaskOf arm share
// ONE classifier (the shared UnknownPredicates pattern) and the two colour
// paths can never disagree about what is a CDA.
func CDASetColourClaimStatic(s cards.Static) (ColorMask, bool, bool) {
	return cdaSetColourClaimStatic(s)
}

// CDASetColourClaim folds the CDA SetColor$ statics of one face into the
// single overwrite claim they make (Transguild Courier / Sphinx of the
// Guildpact "CARDNAME is all colors", Ghostfire "CARDNAME is colorless"):
// every static passing the cdaSetColours gate (Mode$ Continuous,
// CharacteristicDefining$ True exact case, Affected$ naming Self) contributes
// its parsed set OR-ed in, and ok reports whether EVERY such static parsed --
// one the parser cannot read (ChosenColor) spoils the whole claim, the same
// fail-closed direction the scan's own ok gate takes, so the caller keeps the
// printed colours rather than applying a partial prefix. A face with no CDA
// SetColor$ static is ok=false (no claim). "Colorless" parses to the empty
// set with ok=true -- a real overwrite to colourless (Ghostfire) -- which is
// why cards.cdaSetColours' uint8 return cannot be reused for this read: it
// cannot distinguish an empty claim from no claim. ColorLetters' vocabulary
// (comma lists, " & " lists, All) is a superset of cdaSetColours' single-word
// switch; the corpus's CDA carriers print single words only, so on every real
// carrier the two parsers agree.
func CDASetColourClaim(f *cards.Face) (ColorMask, bool) {
	if f == nil {
		return 0, false
	}
	var mask ColorMask
	ok := true
	any := false
	for _, s := range f.Statics {
		m, isCDA, parsed := cdaSetColourClaimStatic(s)
		if !isCDA {
			continue
		}
		any = true
		mask |= m
		ok = ok && parsed
	}
	if !any || !ok {
		return 0, false
	}
	return mask, true
}

// colorLetters turns a Forge colour-list parameter value (Animate's Colors$,
// the "White,Blue" / "White" / "All" / "Colorless" vocabulary measured over
// the corpus's AB$ Animate lines, and the "Green & White" static spelling)
// into the WUBRG letters of the colour set it names, in WUBRG order and
// deduplicated. A list may separate its entries with commas OR the " & "
// Forge uses on its static list parameters (Witness Protection's
// `SetColor$ Green & White`, Ludevic, Necrogenius's `AddColors$ Blue & Black`)
// -- the SAME grammar rules' statList applies to AddType$, so the two list
// parameters on one static line cannot disagree. "All" is every colour,
// "Colorless" is the empty set. The second return reports whether EVERY
// non-blank word was recognised: a value this parser does not know (the
// corpus's "ChosenColor" family, which asks its controller for a colour) is
// a fail closed "ok=false" with whatever prefix parsed, never a guess -- the
// caller gates the effect registration on it so an unparseable value keeps
// the object's printed colours rather than silently overwriting them away.
// Blank entries (an absent parameter splits to one) neither contribute nor
// spoil ok. The letters are what state.ContinuousEffect's colour fields
// carry, so a caller never re-parses the words.
func colorLetters(list string) ([]string, bool) {
	var set [5]bool
	ok := true
	for entry := range strings.SplitSeq(list, ",") {
		for word := range strings.SplitSeq(strings.TrimSpace(entry), " & ") {
			switch strings.ToLower(strings.TrimSpace(word)) {
			case "":
			case "all":
				set = [5]bool{true, true, true, true, true}
			case "colorless":
				// The empty set; ok stays true. Whether that grant does anything
				// (an overwrite to colourless) or nothing (an add of the empty
				// set) is the CALLER's decision -- effAnimate notes the no-op arm.
			case "white":
				set[0] = true
			case "blue":
				set[1] = true
			case "black":
				set[2] = true
			case "red":
				set[3] = true
			case "green":
				set[4] = true
			default:
				ok = false
			}
		}
	}
	var out []string
	for i, c := range "WUBRG" {
		if set[i] {
			out = append(out, string(c))
		}
	}
	return out, ok
}

// ColorLetters is colorLetters for callers outside this package (rules'
// static scan reading a card's SetColor$). It is the same colour-word
// vocabulary -- commas or " & " between entries -- and the same fail-closed
// contract: ok=false means at least one word was not recognised, so the
// caller must not overwrite the target's colours with the partial prefix.
// "Colorless" parses to an empty set with ok=true, which is a real
// overwrite-to-colourless for a SetColor$ grant (Imprisoned in the Moon),
// distinct from the fail-closed arm.
func ColorLetters(list string) ([]string, bool) {
	return colorLetters(list)
}

// ObjectColors binds one object to its layer-5 derived colour set (CR
// 613.1e). rules builds the table for the objects whose derived colours
// differ from their printed ones and binds it on SpecContext.Layers.DerivedColors,
// so a colour predicate agrees with the layer walk.
type ObjectColors struct {
	ID   state.ObjID
	Mask ColorMask
}

// ColorMaskFromLetters is the mask of a WUBRG letter string (Derived.Colors).
func ColorMaskFromLetters(letters string) ColorMask {
	var m ColorMask
	for i := 0; i < len(letters); i++ {
		m |= colorBit(letters[i])
	}
	return m
}

// colorMaskCtx is the colour read every filter predicate takes: the bound
// layer-5 derived colours when the context carries an entry for o, else the
// printed read (ColorMaskOf). It returns a plain mask, never data sourced
// from the context, so the context does not leak (see hasEffectiveName).
func colorMaskCtx(o *state.Object, sc *SpecContext) ColorMask {
	if o != nil {
		for i := range sc.Layers.DerivedColors {
			if sc.Layers.DerivedColors[i].ID == o.ID {
				return sc.Layers.DerivedColors[i].Mask
			}
		}
	}
	return ColorMaskOf(o)
}

// colorsCtx is colorMaskCtx as WUBRG letters.
func colorsCtx(o *state.Object, sc *SpecContext) string {
	return colorMaskCtx(o, sc).String()
}

// hasDerivedColorEntryPtr reports whether the context binds derived colours
// for o (the compiled predicate programs read the printed face, so such an
// object takes the textual path, as a renamed or retyped one does).
func hasDerivedColorEntryPtr(o *state.Object, sc *SpecContext) bool {
	for i := range sc.Layers.DerivedColors {
		if sc.Layers.DerivedColors[i].ID == o.ID {
			return true
		}
	}
	return false
}

// colourMapPredicate reports whether p is one of the legacy predicate-map
// colour entries (the five colour names and nonBlack), whose map functions
// read the printed colours and carry no context. With derived colours bound
// the matcher skips the map for them and evaluates the same word through
// wordMatches, which reads the context.
func colourMapPredicate(p string) bool {
	if p == "nonBlack" {
		return true
	}
	_, is := colorLetter[p]
	return is
}
