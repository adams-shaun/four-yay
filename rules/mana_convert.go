package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// stat:ManaConvert (CR 608.2i, "spend mana as though it were mana of any
// color"): the S:Mode$ ManaConvert statics. This file owns the grammar and
// the one per-payment conversion set; rules/mana.go's resolveMana consults
// it through the *manaConv parameter on the payment call sites, so an
// offered cost (castable / manaAbilityPayable / the mana window / the X ask)
// and the cost actually charged can never disagree about what the payer may
// spend.
//
// The conversion is a fact about the PAYER'S mana for ONE payment, never
// about the cost: "you may spend white mana as though it were red mana"
// leaves the {R} pip exactly what it was and lets the pool's white mana pay
// it. That is why the model below annotates pool colours, not pips.

// manaColourFrom maps a ManaConversion$ "from" word to the pool-colour
// indexes it names: a colour name or letter is that colour; "AnyType" is all
// six (any type includes colourless); "non<name>" is every colour except the
// named one. An unknown word names nothing -- a conversion this build cannot
// parse is silently inert rather than guessed wide.
func manaColourFrom(word string) []int {
	name := strings.TrimSpace(word)
	complement := false
	if rest, ok := strings.CutPrefix(name, "non"); ok && rest != "" {
		name, complement = rest, true
	}
	var base []int
	switch manaColourFromcf21Codes.Code(string(strings.ToLower(name))) {
	case manaColourFromcf21Anytype:
		base = []int{state.MW, state.MU, state.MB, state.MR, state.MG, state.MC}
	case manaColourFromcf21W:
		base = []int{state.MW}
	case manaColourFromcf21U:
		base = []int{state.MU}
	case manaColourFromcf21B:
		base = []int{state.MB}
	case manaColourFromcf21R:
		base = []int{state.MR}
	case manaColourFromcf21G:
		base = []int{state.MG}
	case manaColourFromcf21C:
		base = []int{state.MC}
	default:
		return nil
	}
	if !complement {
		return base
	}
	excl := map[int]bool{}
	for _, i := range base {
		excl[i] = true
	}
	var out []int
	for i := range manaLetters {
		if !excl[i] {
			out = append(out, i)
		}
	}
	return out
}

// manaColourTo maps a ManaConversion$ "to" word to the pip it makes
// acceptable: a colour name or letter is that pip; "AnyColor" is the wild
// (any colour, not {C}) grant; "AnyType" is wild plus the {C} pips. The
// second return reports whether the word resolved at all, so an unknown
// conversion target leaves the static inert rather than granting everything.
func applyManaConversionTo(conv *manaConv, from []int, word string) bool {
	name := strings.ToLower(strings.TrimSpace(word))
	switch applyManaConversionTocf22Codes.Code(string(name)) {
	case applyManaConversionTocf22Anycolor:
		for _, i := range from {
			conv.Wild[i] = true
		}
	case applyManaConversionTocf22Anytype:
		for _, i := range from {
			conv.Wild[i] = true
		}
		conv.WildC = true
	case applyManaConversionTocf22C:
		// "as though it were colorless mana" as a GRANT: mana of the "from"
		// colours may additionally pay {C} pips. Not a corpus shape today,
		// but the same grammar the restriction side uses; kept for symmetry.
		for _, i := range from {
			conv.To[i][state.MC] = true
		}
	case applyManaConversionTocf22W:
		for _, i := range from {
			conv.To[i][state.MW] = true
		}
	case applyManaConversionTocf22U:
		for _, i := range from {
			conv.To[i][state.MU] = true
		}
	case applyManaConversionTocf22B:
		for _, i := range from {
			conv.To[i][state.MB] = true
		}
	case applyManaConversionTocf22R:
		for _, i := range from {
			conv.To[i][state.MR] = true
		}
	case applyManaConversionTocf22G:
		for _, i := range from {
			conv.To[i][state.MG] = true
		}
	default:
		return false
	}
	return true
}

// staticSAKindMatches reports whether a ValidSA$ value on a ManaConvert
// static admits this kind of payment. The value is Forge's comma-separated
// OR list of "<kind>[.<qualifier>]" ("Spell", "Activated", "Spell.MayPlaySource",
// "Spell,Activated"); an absent value admits both. An alternative whose kind
// is neither Spell nor Activated (a Loyalty/other qualifier this build
// cannot evaluate) is skipped rather than guessed: a conversion the engine
// cannot scope never grants, the fail-closed direction every other
// restriction-class reader here uses.
func staticSAKindMatches(validSA string, ability bool) bool {
	v := strings.TrimSpace(validSA)
	if v == "" {
		return true
	}
	for alt := range strings.SplitSeq(v, ",") {
		kind := strings.TrimSpace(alt)
		if i := strings.IndexByte(kind, '.'); i >= 0 {
			kind = kind[:i]
		}
		switch staticSAKindMatchescf23Codes.Code(string(kind)) {
		case staticSAKindMatchescf23Spell:
			if !ability {
				return true
			}
		case staticSAKindMatchescf23Activated:
			if ability {
				return true
			}
		}
	}
	return false
}

func (e *Engine) manaConversionParts(p state.PlayerID, id state.ObjID, ability bool) (manaConv, manaConv) {
	var mandatory, optional manaConv
	// The printed sources are a board-only list, cached for a legal-actions
	// walk (rules/walkcache.go); the per-payment filter below still runs.
	srcs := e.manaConvPrintedSources()
	// remembered is the Effect-delivered static's own Remembered set (nil for
	// a printed static). It is threaded into the ValidCard$ SpecContext so a
	// `ValidCard$ Card.IsRemembered` conversion (Abstruse Appropriation's
	// exiled card) resolves against the effect that created it.
	apply := func(sv staticView, remembered []state.ObjID) {
		if vp, ok := sv.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, vp, p, sv.Controller) {
			return
		}
		if !e.manaConvAffectedZoneAdmits(sv.ParamStr(cards.PKAffectedZone), id) {
			return
		}
		if vc, ok := sv.Param(cards.PKValidCard); ok && vc != "" &&
			!e.matchesSpec(vc, id, e.manaConvSpecCtx(sv, p, remembered)) {
			return
		}
		if vsa, ok := sv.Param(cards.PKValidSA); ok && !staticSAKindMatches(vsa, ability) {
			return
		}
		dst := &mandatory
		if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKOptional)), "True") {
			dst = &optional
		}
		for tok := range strings.FieldsSeq(sv.ParamStr(cards.PKManaConversion)) {
			if from, to, ok := strings.Cut(tok, "->"); ok {
				if froms := manaColourFrom(from); froms != nil {
					applyManaConversionTo(dst, froms, to)
				}
				continue
			}
			if from, ok := strings.CutSuffix(tok, "<-C"); ok {
				if froms := manaColourFrom(from); froms != nil {
					for _, i := range froms {
						dst.OnlyC[i] = true
					}
				}
			}
		}
	}
	for i := range srcs {
		apply(srcs[i].sv, nil)
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.CostStaticMode == "ManaConvert" {
			remembered := ce.Remembered
			// A may-play cast's own ForgetOnMoved$ clears the binding the
			// instant the card reaches the stack (CR 601.2a), but the paired
			// ManaConvert's ValidCard$ Card.IsRemembered must keep resolving
			// through the cost payment (CR 601.2h). Fall back to the binding
			// recorded at beginCast, while the card was still in the granted
			// zone; a still-live binding always wins.
			if len(remembered) == 0 && e.cast != nil && e.cast.card == id {
				if captured := e.cast.mayPlayRemembered[ce.Source]; len(captured) > 0 {
					remembered = captured
				}
			}
			apply(staticView{Source: ce.Source, Controller: ce.Controller,
				Params: ce.CostStaticParams, SVars: ce.CostStaticSVars}, remembered)
		}
	}
	return mandatory, optional
}

// mayPlayManaConvertRemembered records, at beginCast, every active
// Effect-delivered ManaConvert static's remembered-object binding keyed by
// its source. The map is deliberately keyed by source and read only by exact
// key (never ranged), so it introduces no iteration-order dependence. A
// static with an empty remembered set is omitted; the live read stays
// authoritative whenever it is non-empty.
func (e *Engine) mayPlayManaConvertRemembered(p state.PlayerID, id state.ObjID) map[state.ObjID][]state.ObjID {
	var out map[state.ObjID][]state.ObjID
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.CostStaticMode != "ManaConvert" || len(ce.Remembered) == 0 {
			continue
		}
		if out == nil {
			out = map[state.ObjID][]state.ObjID{}
		}
		out[ce.Source] = append([]state.ObjID(nil), ce.Remembered...)
	}
	return out
}

// manaConvSpecCtx is the filter context a ManaConvert static's ValidCard$
// resolves against: the staticView's own SVar table (an under-card static),
// plus the Effect's Remembered set when the static was Effect-delivered, so
// `Card.IsRemembered` (Abstruse Appropriation) sees the card the effect
// captured. `you` is the payer, matching the pre-existing specCtx read.
func (e *Engine) manaConvSpecCtx(sv staticView, you state.PlayerID, remembered []state.ObjID) effects.SpecContext {
	sc := e.specCtxSVars(sv.Source, you, sv.SVars)
	if len(remembered) > 0 {
		sc.Remembered = rememberedTargets(remembered)
	}
	return sc
}

// manaConvAffectedZoneAdmits reports whether a ManaConvert static carrying
// AffectedZone$ applies to the payment subject id. An absent AffectedZone$
// applies everywhere (the pre-existing behaviour). The subject's EFFECTIVE
// zone is used: for the spell a live cast is paying for, that is the cast's
// ORIGIN zone -- by the time payment runs (CR 601.2h) the spell has already
// moved to the stack, but `AffectedZone$ Exile` on a MayPlay-scoped
// conversion (Abstruse Appropriation) names the zone the card is cast FROM,
// exactly as the paired MayPlay$ static's own AffectedZone$ does. Every other
// subject (an ability's source, a cost paid by a permanent) uses the
// object's current zone. An AffectedZone$ this build cannot parse fails
// closed, the same fail-closed direction an unevaluable ValidCard$ takes --
// an unparseable scope must never grant a conversion.
func (e *Engine) manaConvAffectedZoneAdmits(spec string, id state.ObjID) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	zones, all, ok := effects.ParseZones(spec)
	if !ok {
		return false
	}
	if all {
		return true
	}
	z, known := e.manaConvSubjectZone(id)
	if !known {
		return false
	}
	for _, cand := range zones {
		if cand == z {
			return true
		}
	}
	return false
}

// manaConvSubjectZone is the effective zone the AffectedZone$ scope is
// checked against. A live cast (or ability activation) for this subject
// reports its ORIGIN zone: the object has moved to the stack by the time the
// payment gate runs, but the conversion's scope names where the card is cast
// from. Any other read falls back to the object's current zone; a subject
// the game no longer knows is unknown (fail-closed).
func (e *Engine) manaConvSubjectZone(id state.ObjID) (state.Zone, bool) {
	if e.cast != nil && e.cast.card == id {
		return e.cast.from, true
	}
	o := e.G.Obj(id)
	if o == nil {
		return 0, false
	}
	return o.Zone, true
}

func mergeManaConv(dst *manaConv, src manaConv) {
	for i := range dst.Wild {
		dst.Wild[i] = dst.Wild[i] || src.Wild[i]
		dst.OnlyC[i] = dst.OnlyC[i] || src.OnlyC[i]
		for j := range dst.To[i] {
			dst.To[i][j] = dst.To[i][j] || src.To[i][j]
		}
	}
	dst.WildC = dst.WildC || src.WildC
}

const (
	manaColourFromcf21Anytype uint16 = 1 // "anytype"
	manaColourFromcf21W       uint16 = 2 // "w", "white"
	manaColourFromcf21U       uint16 = 3 // "u", "blue"
	manaColourFromcf21B       uint16 = 4 // "b", "black"
	manaColourFromcf21R       uint16 = 5 // "r", "red"
	manaColourFromcf21G       uint16 = 6 // "g", "green"
	manaColourFromcf21C       uint16 = 7 // "c", "colorless"
)

var manaColourFromcf21Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "anytype", Val: manaColourFromcf21Anytype},
	state.StrEntry[uint16]{Key: "w", Val: manaColourFromcf21W},
	state.StrEntry[uint16]{Key: "white", Val: manaColourFromcf21W},
	state.StrEntry[uint16]{Key: "u", Val: manaColourFromcf21U},
	state.StrEntry[uint16]{Key: "blue", Val: manaColourFromcf21U},
	state.StrEntry[uint16]{Key: "b", Val: manaColourFromcf21B},
	state.StrEntry[uint16]{Key: "black", Val: manaColourFromcf21B},
	state.StrEntry[uint16]{Key: "r", Val: manaColourFromcf21R},
	state.StrEntry[uint16]{Key: "red", Val: manaColourFromcf21R},
	state.StrEntry[uint16]{Key: "g", Val: manaColourFromcf21G},
	state.StrEntry[uint16]{Key: "green", Val: manaColourFromcf21G},
	state.StrEntry[uint16]{Key: "c", Val: manaColourFromcf21C},
	state.StrEntry[uint16]{Key: "colorless", Val: manaColourFromcf21C},
)

const (
	applyManaConversionTocf22Anycolor uint16 = 1 // "anycolor"
	applyManaConversionTocf22Anytype  uint16 = 2 // "anytype"
	applyManaConversionTocf22C        uint16 = 3 // "c", "colorless"
	applyManaConversionTocf22W        uint16 = 4 // "w", "white"
	applyManaConversionTocf22U        uint16 = 5 // "u", "blue"
	applyManaConversionTocf22B        uint16 = 6 // "b", "black"
	applyManaConversionTocf22R        uint16 = 7 // "r", "red"
	applyManaConversionTocf22G        uint16 = 8 // "g", "green"
)

var applyManaConversionTocf22Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "anycolor", Val: applyManaConversionTocf22Anycolor},
	state.StrEntry[uint16]{Key: "anytype", Val: applyManaConversionTocf22Anytype},
	state.StrEntry[uint16]{Key: "c", Val: applyManaConversionTocf22C},
	state.StrEntry[uint16]{Key: "colorless", Val: applyManaConversionTocf22C},
	state.StrEntry[uint16]{Key: "w", Val: applyManaConversionTocf22W},
	state.StrEntry[uint16]{Key: "white", Val: applyManaConversionTocf22W},
	state.StrEntry[uint16]{Key: "u", Val: applyManaConversionTocf22U},
	state.StrEntry[uint16]{Key: "blue", Val: applyManaConversionTocf22U},
	state.StrEntry[uint16]{Key: "b", Val: applyManaConversionTocf22B},
	state.StrEntry[uint16]{Key: "black", Val: applyManaConversionTocf22B},
	state.StrEntry[uint16]{Key: "r", Val: applyManaConversionTocf22R},
	state.StrEntry[uint16]{Key: "red", Val: applyManaConversionTocf22R},
	state.StrEntry[uint16]{Key: "g", Val: applyManaConversionTocf22G},
	state.StrEntry[uint16]{Key: "green", Val: applyManaConversionTocf22G},
)

const (
	staticSAKindMatchescf23Spell     uint16 = 1 // "Spell"
	staticSAKindMatchescf23Activated uint16 = 2 // "Activated"
)

var staticSAKindMatchescf23Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Spell", Val: staticSAKindMatchescf23Spell},
	state.StrEntry[uint16]{Key: "Activated", Val: staticSAKindMatchescf23Activated},
)
