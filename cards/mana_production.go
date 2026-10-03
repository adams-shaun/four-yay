package cards

import (
	"strconv"
	"strings"
)

// ManaProduction is a face's mana-production capability summary, expressed
// as plain data so any later package (view, botpolicy, the seat adapter) can
// carry it without importing a package to its right. It is derived from the
// face's own abilities -- never from land subtypes. A face with several mana
// abilities lists their possible production here; rules selects exactly one
// when a shared tap cost is paid (rules/mana_activation.go).
//
// Colour is indexed the same way state.Mana is (W, U, B, R, G, then
// colourless), so a caller that also imports state can translate an index
// with state.ManaIndex. A dual land may list both possible colours here, but
// selecting one of its distinct mana abilities on activation adds only that
// selected ability's mana.
//
// Any reports that at least one mana ability's Produced$ was not a plain
// colour string: "Any"/"Combo Any", a listed "Combo X Y" choice, or a
// "Chosen"/"Special" word (any token the symbol grammar cannot read). Such a
// source is conditional in the card script, so a policy must not treat it as
// a dependable colour fixer. Colour carries what a plain token names (one each
// for "R G", two for "RR") and the real alternatives of a choice token. It
// never counts letters of script words as phantom mana: Chosen is represented
// by all five possible colours because its source-specific choice is not
// available to this source-free parser.
type ManaProduction struct {
	Colour [6]int32 `json:"colour"`
	Any    bool     `json:"any"`
	// Indeterminate reports that at least one mana ability on the face
	// carries a non-literal Amount$ ("X", "Y", "UrzaAmount", a Count$
	// expression, "Sacrificed$...") whose value the projection cannot
	// statically price. Such an ability yields no amount the pool is
	// guaranteed to receive -- the executor's Num (effects/count.go)
	// resolves it to an SVar/count/X that is zero on a plain tap-for-mana
	// activation or unknown at projection time -- so the collector claims
	// no colour slot from it (every contribution is zero). The flag is what
	// lets a policy distinguish "this source produces mana I can count on"
	// from "this source might produce something"; chooseTap must never
	// PREFER an indeterminate source over one that demonstrably produces.
	// Indeterminate is a statement about the AMOUNT, orthogonal to Any,
	// which is a statement about the PRODUCED colour choice.
	//
	// It is a server-only field: the bot policy reads it, but it must not
	// ride the human wire (CardView) -- no web component consumes
	// CardView.produces at all, so surfacing it as `indeterminate?: boolean`
	// in protocol.ts is dead payload on every card view. json:"-" keeps it
	// out of tsgen's jsonName (internal/tsgen/tsgen.go) while the Go field
	// stays for the two adapters and the policy that reads it.
	Indeterminate bool `json:"-"`
	// Reflected reports that at least one mana ability on the face is an
	// AB$ ManaReflected ability, whose produced colour is computed at
	// RESOLUTION from the objects its Valid$ selector names
	// (effects.ManaReflectedCandidates) and is therefore never statically
	// knowable. Any is also set (the colour is a script-level choice), but
	// Reflected is the narrower fact a consumer needs when it must
	// distinguish a reflected source from the corpus's 500+ ordinary "Any"
	// sources (Cavern of Souls and the rest): a reflected source resolves to
	// the colour its controller asks for, so a tap gate may aim a needed
	// coloured pip at it, while an ordinary Any source's behaviour must not
	// be widened by the same change (ticket cli-20260922T225137Z). It is a
	// server-only field, like Indeterminate: the bot policy reads it, it
	// must not ride the human wire (CardView).
	Reflected bool `json:"-"`
}

// manaAbilityAmount is the Amount$ a mana ability produces. It returns the
// amount and whether the amount is known at projection time:
//
//   - a blank Amount is the executor's own default of 1 (Num's def), known;
//   - a literal integer is used directly, known (negative clamped to 0,
//     effMana's T14-f clamp);
//   - anything else ("X", "Y", "UrzaAmount", a Count$ expression,
//     "Sacrificed$...") is a value the projection cannot price -- the
//     executor's Num resolves it through the SVar/count/$X machinery to a
//     count the collector has no game context for -- so it is returned
//     as (0, false): NOT a guaranteed amount. The collector must not
//     record a source as producing mana the pool will never be promised.
//
// The contract mirrors effMana exactly in the cases that are statically
// decidable (blank -> 1, literal -> literal) and diverges only where effMana
// would need a live game, where claiming a count would be a lie.
func manaAbilityAmount(a *SA) (int32, bool) {
	raw := strings.TrimSpace(a.ParamStr(PKAmount))
	if raw == "" {
		return 1, true
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v < 0 {
			v = 0
		}
		return int32(v), true
	}
	return 0, false
}

// manaSymbols is the set of single-letter mana symbols a Produced$ token may
// consist of: the five colours plus colourless. A token whose runes all lie
// in this set is a plain literal (each rune one unit of its slot); a token
// carrying any other rune names a script-level choice ("Chosen",
// "ColorIdentity", a "Special ..." word) and must not be walked at all.
const manaSymbols = "WUBRGC"

// ManaSymbol is the single-letter mana symbol that occupies slot i of a
// ManaProduction.Colour vector (0 W, 1 U, 2 B, 3 R, 4 G, 5 colourless).
// cards cannot import state, so a caller that also owns a state.Mana
// translates the letter with state.ManaIndex -- the two slot layouts are
// the same by convention.
func ManaSymbol(i int) byte {
	if i < 0 || i >= len(manaSymbols) {
		return 0
	}
	return manaSymbols[i]
}

// ProducedCounts parses one mana ability's Produced$ value into the
// per-symbol mana counts (indexed W, U, B, R, G, colourless -- the same
// layout state.Mana uses) that its plain tokens state, plus the any flag for
// a production whose colour is a script-level choice. It is THE one Produced$
// parse the projection collectors share: cards.ManaProduction.add (the
// per-face capability summary that rides CardView.produces) and rules'
// addAvailable (the engine's available-by-tapping aggregate) both fold
// through it, so the two cannot drift the way the pre-fb-windgrace rune walk
// let them (walking "Combo B R" one rune at a time counted the letters of
// the word "Combo" as five phantom colourless).
//
// The grammar, deliberately narrow and fail-closed (the effMana convention):
//
//   - blank is one colourless; exactly "Any" or exactly "Combo Any" is one
//     possible unit in each of the five colours plus any (the executor asks
//     for that colour when a decision-capable host is present);
//   - braces are stripped, the value is split on whitespace, and a leading
//     literal "Combo" token is dropped (it names a choice, not a symbol);
//     a Combo-prefixed value is a script-level CHOICE among its tokens, so
//     it is flagged any however well-formed its tokens are;
//   - every remaining token either consists solely of the symbols WUBRGC
//     -- each of its runes then adds one unit of its slot, so "RR" is two
//     red and "R G" is one red and one green -- or it names a script-level
//     choice ("Chosen", "ColorIdentity", a "Special ..." word). Chosen
//     exposes all five possible colours; the other unmodelled words claim no
//     mana while setting any. A token is rejected whole: a word
//     that merely CONTAINS a symbol letter ("ColorIdentity" contains "C")
//     is not a symbol and must not be walked, or the phantom is back.
//
// The caller multiplies the counts by the ability's Amount$ (or applies its
// own default); the flag is independent of the amount -- an unpriceable
// Amount$ still sets any, because the colour choice is unmodelled even when
// the count is not the collector's problem.
func ProducedCounts(produced string) (counts [6]int32, any bool) {
	raw := strings.TrimSpace(produced)
	if raw == "" {
		counts[5] = 1
		return counts, true
	}
	if raw == "Any" || raw == "Combo Any" || raw == "Chosen" || raw == "ComboChosen" {
		for i := 0; i < 5; i++ {
			counts[i] = 1
		}
		return counts, true
	}
	tokens := strings.Fields(producedBraces.Replace(raw))
	if len(tokens) > 0 && tokens[0] == "Combo" {
		tokens = tokens[1:]
		any = true // a Combo choice is never a plain colour string
	}
	if len(tokens) == 0 {
		// A bare "Combo" (or a value that is only braces): a choice-shaped
		// production that names no symbol at all. Nothing is claimed.
		return counts, true
	}
	for _, tok := range tokens {
		if tok == "Chosen" || tok == "ChosenColor" {
			// ProducedCounts has no source object from which to read the
			// already-recorded choice.  The real possibilities are therefore
			// the five colours; the activation/effect paths substitute the
			// source's ChosenColor before resolving a concrete unit.
			for i := 0; i < 5; i++ {
				counts[i] = 1
			}
			any = true
			continue
		}
		if strings.Trim(tok, manaSymbols) != "" {
			any = true
			continue
		}
		for _, r := range tok {
			switch r {
			case 'W':
				counts[0]++
			case 'U':
				counts[1]++
			case 'B':
				counts[2]++
			case 'R':
				counts[3]++
			case 'G':
				counts[4]++
			default:
				counts[5]++
			}
		}
	}
	return counts, any
}

var producedBraces = strings.NewReplacer("{", "", "}", "")

// add folds one mana ability's production into the collector. It mirrors
// effMana's conventions: blank becomes one C, Any/Combo Any and Chosen expose
// their possible WUBRG colours, a plain symbol token adds its listed colours,
// and an unrecognised token (ColorIdentity or a Special word) claims no mana
// while flagging Any. The counts come from ProducedCounts, the one parse the
// available-mana projection in rules reuses.
func (mp *ManaProduction) add(a *SA) {
	amt, known := manaAbilityAmount(a)
	if !known {
		// The amount this ability would add is not statically known, so it
		// contributes nothing to the guaranteed-production counts the policy
		// relies on; the Indeterminate flag carries that it might produce
		// something. Keep off the wire-claim path: an unknown-amplitude
		// source must never make ProducesColour true.
		mp.Indeterminate = true
	}
	counts, any := ProducedCounts(a.ParamStr(PKProduced))
	if any {
		mp.Any = true
	}
	for i := range counts {
		mp.Colour[i] += counts[i] * amt // amt is 0 for an indeterminate amount
	}
}

// ManaProduction returns the production capabilities of every mana ability
// on the face, folded together. It is derived once at load from the face's
// abilities (ManaAbilities), refreshed after ApplyIntrinsics adds abilities,
// so a card the intrinsic layer granted its mana from subtypes (a Plains, a
// dual with a Plains and an Island half) is covered by the same path as a card
// whose script spells the ability out.
func (f *Face) ManaProduction() ManaProduction { return f.manaProduction }

// addReflected folds one AB$ ManaReflected ability into the collector. Such
// an ability's colours are computed at RESOLUTION from the objects its Valid$
// selector names (effects.ManaReflectedCandidates), so they are not knowable
// at load time; the honest per-face summary is the same shape ProducedCounts
// gives an "Any"/"Combo Any" ability -- a CONDITIONAL colour choice, flagged
// Any, whose guaranteed amount is folded as one colourless (the executor's
// own unmodelled-colour convention). Before this the reflected ability was
// invisible to the collector altogether (Face.ManaAbilities lists only AB$
// Mana), so a reflected source projected as producing nothing and a bot's
// tap gate never aimed a coloured pip at it. The amount is read the same way
// every other ability's is (a blank Amount is 1, an unpriced one contributes
// nothing and sets Indeterminate).
func (mp *ManaProduction) addReflected(a *SA) {
	amt, known := manaAbilityAmount(a)
	if !known {
		mp.Indeterminate = true
	}
	mp.Any = true
	mp.Reflected = true
	mp.Colour[5] += amt
}

// Distinguishes the five coloured pool slots from colourless: the index of a
// colour the policy can spend on a coloured pip. Index 5 (colourless) is not a
// colour.
const manaColourStart, manaColourEnd = 0, 5

// DistinctColours is the number of distinct coloured (WUBRG) mana kinds the
// production yields. An Any production reports all five: it is conditional in
// script, but still must sort after a basic rather than masquerading as a
// zero-flexibility source. Otherwise a source that produces exactly one colour
// (a basic Plains) reports 1 and is spent before a dual that reports 2.
func (mp ManaProduction) DistinctColours() int {
	if mp.Any {
		return manaColourEnd - manaColourStart
	}
	n := 0
	for i := manaColourStart; i < manaColourEnd; i++ {
		if mp.Colour[i] > 0 {
			n++
		}
	}
	return n
}

// ProducesColour reports whether the production yields at least one mana of
// colour i (a WUBRG index; index 5, colourless, reports false -- a colourless
// source never pays a coloured pip).
func (mp ManaProduction) ProducesColour(i int) bool {
	return i >= manaColourStart && i < manaColourEnd && mp.Colour[i] > 0
}

// Colourless reports whether the production yields any colourless mana.
func (mp ManaProduction) Colourless() bool { return mp.Colour[5] > 0 }

// IsZero reports whether the production is empty -- a card with no mana
// ability, or one whose abilities produce nothing this build can count. It is
// what lets a caller keep a zero production off the wire (omitempty) rather
// than pay for a six-entry array on every card.
func (mp ManaProduction) IsZero() bool { return mp == ManaProduction{} }
