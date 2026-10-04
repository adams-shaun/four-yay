package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// mana_convert.go reads the board's stat:ManaConvert statics (CR 608.2i)
// into the conversion sets one payment resolves under; the ManaConversion$
// grammar itself lives in rules/pay (conv.go).

// manaConversionParts is the mandatory and Optional$ conversion sets the
// active ManaConvert statics grant p paying for id.
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
		if vsa, ok := sv.Param(cards.PKValidSA); ok && !pay.StaticSAKindMatches(vsa, ability) {
			return
		}
		dst := &mandatory
		if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKOptional)), "True") {
			dst = &optional
		}
		for tok := range strings.FieldsSeq(sv.ParamStr(cards.PKManaConversion)) {
			if from, to, ok := strings.Cut(tok, "->"); ok {
				if froms := pay.ManaColourFrom(from); froms != nil {
					pay.ApplyConversionTo(dst, froms, to)
				}
				continue
			}
			if from, ok := strings.CutSuffix(tok, "<-C"); ok {
				if froms := pay.ManaColourFrom(from); froms != nil {
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
		sc.Remembered = pay.RememberedTargets(remembered)
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
