package rules

import (
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// legalWalk carries the state the priority-option walk shares across its
// section methods (legal_walk_hand.go, legal_walk_alt.go,
// legal_walk_battlefield.go). costStatics/actionStatics are the ONE
// lazily-filled statics memo the whole walk reads -- the closures and the
// sections must hit the same instance, so a face probe can never leave a
// later reader with a stale probed-face collection (faceprobe.go).
type legalWalk struct {
	e             *Engine
	p             state.PlayerID
	hyp           *state.Mana
	castsOnly     bool
	sorcery       bool
	out           []decision.Option
	costStatics   costStaticSource
	actionStatics actionStaticSource
}

// add appends one option to the walk scratch list with the running Index.
func (w *legalWalk) add(kind, label string, obj state.ObjID) {
	w.out = append(w.out, decision.Option{Index: len(w.out), Kind: kind, Label: label, Obj: obj})
}

// hyp is the pricing mode the walk runs in: nil is the ordinary real-pool
// offer walk; non-nil is the potential-action walk's hypothetical bound.
// The two affordability gates below are the ONLY pricing difference:
// every non-mana part (Sac/Discard/SubCounter/Tap) is checked against the
// REAL state in both modes -- floating mana never satisfies a sacrifice --
// and the cost composition (offerCostFor) is pool-independent, so the
// two walks cannot drift inside the body they share.
func (w *legalWalk) castRestricted(p state.PlayerID, id state.ObjID) bool {
	return w.e.castRestrictedUsing(w.actionStatics.get().cantCast, p, id)
}

func (w *legalWalk) abilityRestricted(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	return w.e.abilityRestrictedUsing(w.actionStatics.get().cantActivate, p, id, ab)
}

func (w *legalWalk) offerCastable(p state.PlayerID, id state.ObjID, base Cost, scope costScope, ability bool) bool {
	statics := w.costStatics.get()
	if w.offerFloorRefuses(&statics, p, &base, scope) {
		if walkSkipVerify && w.e.offerCastableUsing(statics, p, id, base, scope, ability, w.hyp) {
			panic(fmt.Sprintf("rules: offer floor refusal for obj %d (%+v) disagrees with offerCastableUsing", id, base))
		}
		// The skipped composition is the empty one, which clears the
		// provenance capture and sets nothing else.
		w.e.costProvenanceSeen = false
		return false
	}
	return w.e.offerCastableUsing(statics, p, id, base, scope, ability, w.hyp)
}

// offerFloorRefuses reports, without composing a modifier set, that
// offerCastableUsing refuses base on its first mana test: the pool-unit
// floor manaFeasiblePoolP checks before any payer read.
//
// It answers only where that composition is provably the zero one and
// every later chance is closed:
//
//   - the floating pool prices the offer (hyp nil, no RestrictedMana), so
//     the priced pool is p's Pool;
//   - no RaiseCost/ReduceCost/SetCost static is in the walk's snapshot (so
//     no ValidTarget$ member either), and an ability scope carries no own
//     ReduceCost$ (ownManaReduction's only source): costModifiersCompose
//     returns costMods{}, which has no floor (hasFloorP), so the floor test
//     runs, and the potential-target retry, the Sac<X> sweep (no announced
//     Sac part) and the named-count sweep (no extra cost) all decline;
//   - no PayLife<X>, Waterbend or XMin part, no negative Life/Snow, and for
//     an ability scope no announced X (its XMin$ would raise XMin), so the
//     cost the floor reads is base and composedPoolFloor takes its
//     zero-composition branch: max(0, Generic+tax-delve) plus the coloured
//     pips. That is non-decreasing in the commander tax (>= 0, and 0 for an ability) and
//     non-increasing in delve (0, or the graveyard's size with Delve), so
//     pricing tax 0 against the whole graveyard bounds the floor from below.
//
// walkSkipVerify runs offerCastableUsing on every refusal.
func (w *legalWalk) offerFloorRefuses(statics *costStaticViews, p state.PlayerID, base *Cost, scope costScope) bool {
	if w.hyp != nil || len(statics.raise) != 0 || len(statics.reduce) != 0 || len(statics.set) != 0 || statics.validTarget ||
		len(base.LifeX) != 0 || base.Waterbend != 0 || base.WaterbendX || base.XMin != 0 || base.Life < 0 || base.Snow < 0 {
		return false
	}
	if ab := scope.ab; ab != nil {
		if _, own := ab.Params["ReduceCost"]; own || costAnnouncesX(*base) {
			return false
		}
	}
	for i := range base.Sac {
		if base.Sac[i].Announced {
			return false
		}
	}
	pl := &w.e.G.Players[p]
	if len(pl.RestrictedMana) != 0 {
		return false
	}
	var zero costMods
	return int64(pl.Pool.Total()) < composedPoolFloor(&zero, base, 0, int32(len(w.e.G.Zone(state.ZGraveyard, p))))
}

// offerCastableAsFace is offerCastable for an alternate-face cast route
// (adventure_alt, adventure_recast, room_alt, split_alt, aftermath):
// beginCast flips the object to `face` before pricing, so the gate prices
// with the object showing that face too (faceprobe.go) -- otherwise a
// modifier reading the card's characteristics (Thalia's
// Card.nonCreature) matches the wrong face and an unpayable cast is
// offered, reversed and re-offered forever. The walk's cached statics are
// fetched BEFORE the probe (a lazy first collection inside it would cache
// the probed face for the rest of the walk) and re-collected inside it
// only when either face carries a cost-modifier static of its own.
func (w *legalWalk) offerCastableAsFace(p state.PlayerID, id state.ObjID, face *cards.Face, base Cost, scope costScope) bool {
	statics := w.costStatics.get()
	var cur *cards.Face
	if o := w.e.G.Obj(id); o != nil {
		cur = o.Face()
	}
	return w.e.offerAsFace(id, face, func() bool {
		if faceHasCostStatics(cur) || faceHasCostStatics(face) {
			statics = w.e.collectCostStatics()
		}
		return w.e.offerCastableUsing(statics, p, id, base, scope, false, w.hyp)
	})
}

// castRestrictedAsFace is castRestricted for an alternate-face cast route
// (modal_spell): the CantBeCast prohibition must be evaluated against the
// face the cast flips to -- the same probe offerCastableAsFace prices
// under, and the same face recheckIllegal (CR 601.2e) re-checks against
// after beginCast's real FlipFace. CR 712.4d: while a Modal DFC's spell
// is on the stack its characteristics are the chosen face's, so a
// restriction that matches only one face decides only that face's offer
// (a Card.nonCreature lockout withholds the sorcery back face but must
// not touch the creature front, and ValidCard$ Creature the reverse).
// The walk's cached CantBeCast statics are fetched BEFORE the probe: a
// CantBeCast source is battlefield-only in collectActionStatics' walk and
// the probed card sits in hand, so the collection is face-independent,
// and a lazy first collection inside the probe would cache the probed
// face for the rest of the walk (faceprobe.go). castRestrictionSources
// re-reads the restricted card's OWN statics from o.Face() live, so a
// back-face self-restriction is seen under the probe exactly as
// recheckIllegal sees it after the real flip.
func (w *legalWalk) castRestrictedAsFace(p state.PlayerID, id state.ObjID, face *cards.Face) bool {
	statics := w.actionStatics.get().cantCast
	return w.e.offerAsFace(id, face, func() bool {
		return w.e.castRestrictedUsing(statics, p, id)
	})
}

// affordable is the composed-cost gate the two direct e.castable sites of
// the walk use (the may-play and escape walks price an already-composed
// cost, so they cannot re-run the modifier composition offerCastable
// owns); hyp==nil is the ordinary castable, hyp!=nil prices the same
// composed cost against the hypothetical pool.
func (w *legalWalk) affordable(q state.PlayerID, id state.ObjID, c Cost, ability bool) bool {
	if w.hyp == nil {
		return w.e.castable(q, id, c, ability)
	}
	return w.e.castablePriced(q, id, c, ability, *w.hyp)
}

func (w *legalWalk) offerCostFor(p state.PlayerID, id state.ObjID, base Cost, scope costScope) Cost {
	return w.e.offerCostForUsing(w.costStatics.get(), p, id, base, scope)
}

// castsOnlyWalkVerify makes every castsOnly walk also run the full walk and
// panic unless its options are exactly the full walk's "cast" options
// (Index aside). Set by the rules test binary (derivedmemo_verify_test.go),
// or at link time with derivedMemoVerifyFlag.
var castsOnlyWalkVerify = derivedMemoVerifyFlag != ""

func verifyCastsOnlyWalk(got, full []decision.Option) {
	var want []decision.Option
	for _, o := range full {
		if o.Kind == "cast" {
			want = append(want, o)
		}
	}
	var casts []decision.Option
	for _, o := range got {
		if o.Kind == "cast" {
			casts = append(casts, o)
		}
	}
	same := len(casts) == len(want)
	for i := 0; same && i < len(want); i++ {
		a, b := casts[i], want[i]
		a.Index, b.Index = 0, 0
		same = reflect.DeepEqual(a, b)
	}
	if !same {
		panic(fmt.Sprintf("rules: castsOnly walk casts %+v, full walk casts %+v", casts, want))
	}
}
