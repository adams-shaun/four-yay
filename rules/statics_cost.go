package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func spellScope(mode string) costScope    { return costScope{Kind: "Spell", Mode: mode} }
func foretellScope() costScope            { return costScope{Kind: "Foretell", Mode: "foretell"} }
func abilityScope(ab *cards.SA) costScope { return costScope{Kind: "Ability", Ab: ab} }

// castSaMayPlaySource is the card-level MayPlaySource token a cost static's
// ValidCard$ may carry (castSaTokens' flag entry of the same spelling).
const castSaMayPlaySource = "CastSa Spell.MayPlaySource"

// specialActionScope prices a CR 116.2 special action that is neither a
// spell nor an activated ability but still has a cost the CR 601.2f
// modifiers reach -- Forge prices these as static abilities (ValidSpell$
// Static.<X>). The modes: "unlock" (CR 309.5 Room unlock), and the CR 708.6
// turn-face-up actions "morphup" (Morph and Megamorph -- Forge's isMorphUp)
// and "disguiseup" (Disguise). Kind "Static" is never Type$ Spell or Type$
// Ability (Forge's Type$ gate requires isSpell / isActivatedAbility), carries
// no commander tax and no targets.
func specialActionScope(mode string) costScope { return costScope{Kind: "Static", Mode: mode} }

// modeIsCastFaceDown reports whether a cast mode puts the spell on the stack
// face down (CR 708.4): the morph family's {3} cast. It is the one reading of
// Forge's SpellAbility.isCastFaceDown the ValidSpell$ Spell.isCastFaceDown
// constraint shares with the cast flow's own faceDown mark (beginCast sets
// pendingCast.faceDown for exactly these modes).
func modeIsCastFaceDown(mode string) bool {
	if v, ok := modeIsCastFaceDownTab.Get(mode); ok {
		return v
	}
	return false
}

// manaFeasibleGrant is manaFeasible with the may-play ignore-colour rider
// passed explicitly (a pendingCast's pc.mayPlayIgnore, or the offer-side
// MayPlayRider derivation), and the payer's PayLifeInsteadOf:B
// grant derived here. Both widen the leaf payable check the same way the
// payment (resolveManaWith) widens it, so an offered cast, an offered
// announcement face and the charged total can never disagree on a
// K'rrik-shaped or MayPlayIgnoreColor$-shaped cost either. The descriptor is
// the caller's: post-announcement costs (pc.resolvedMana and friends) have
// WithX folded their X into Generic, so they must arrive through
// paymentForCast — a paymentFor-built descriptor from a folded cost would
// hide every CostContainsX batch from the target-repricing gates exactly
// where the offer and the payment both admit it.
func (e *Engine) manaFeasibleDescriptor(p state.PlayerID, d paymentDescriptor, c Cost, mods costMods, taxGeneric, delve int32, rider pipRider) bool {
	pl := e.G.Players[p]
	av := pay.AvailableFor(asPayer(e), p, d)
	return mods.FeasibleAny(c, av.Pool, pl.Snow, av.Typed, pl.Life, taxGeneric, delve,
		asPayer(e).PayLifeInsteadOfB(p), rider, asPayer(e).Conv(p, d.ID, d.Class == paymentActivated))
}

// manaFeasiblePool is manaFeasible priced against an EXPLICIT pool instead of
// the seat's restriction-adjusted floating one. The ordinary gate
// (manaFeasible above) is exactly this with the real manaAvailableFor pool;
// the potential-action walk (rules/legal.go legalActionsPriced) passes the
// hypothetical bound the seat would hold after floating every untapped
// source. The payer grants and conversion shaping are the same reads in both
// modes, so a potential action and the offer the walk mirrors can never
// disagree about what the pool may satisfy.
func (e *Engine) manaFeasiblePool(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, pool state.Mana, typed [7]state.Mana) bool {
	return e.manaFeasiblePoolP(p, id, ability, &c, &mods, taxGeneric, delve, pool, typed)
}

// manaFeasiblePoolP is manaFeasiblePool over pointers: *c and *mods are only
// read, and the common pool-floor refusal never copies either.
func (e *Engine) manaFeasiblePoolP(p state.PlayerID, id state.ObjID, ability bool, c *Cost, mods *costMods, taxGeneric, delve int32, pool state.Mana, typed [7]state.Mana) bool {
	// Without an announcement walk feasibleAny is exactly its composed leaf,
	// whose first answer is the pool-unit floor (poolUnitsFloor): decide that
	// here, before the payer's grant and conversion reads the search needs.
	if !mods.HasFloorP() || c.AnnPipCountP() == 0 {
		if int64(pool.Total()) < composedPoolFloor(mods, c, taxGeneric, delve) {
			return false
		}
	}
	pl := &e.G.Players[p]
	return mods.FeasibleAny(*c, pool, pl.Snow, typed, pl.Life, taxGeneric, delve,
		asPayer(e).PayLifeInsteadOfB(p),
		asPayer(e).MayPlayRider(p, id),
		asPayer(e).Conv(p, id, ability))
}

// manaFeasiblePriced is manaFeasible's priced-mode entry: hyp nil keeps the
// ordinary real-pool gate, hyp non-nil prices the feasibility against the
// potential walk's hypothetical bound (rules/legal.go legalActionsPriced).
func (e *Engine) manaFeasiblePriced(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, hyp *state.Mana) bool {
	return e.manaFeasiblePricedP(p, id, ability, &c, &mods, taxGeneric, delve, hyp)
}

// manaFeasiblePricedP is manaFeasiblePriced over pointers (*c and *mods are
// only read). The priced pool is manaAvailableFor's, whose descriptor matters
// only to a RestrictedMana batch: without one it is the floating pool and the
// raw tally verbatim, and under hyp both are replaced outright, so neither
// case builds the descriptor (a Cost copy).
func (e *Engine) manaFeasiblePricedP(p state.PlayerID, id state.ObjID, ability bool, c *Cost, mods *costMods, taxGeneric, delve int32, hyp *state.Mana) bool {
	pl := &e.G.Players[p]
	var pool state.Mana
	var typed [7]state.Mana
	switch {
	case hyp != nil:
		// A hypothetical bound is a pure mana bound (see costPayablePool),
		// so its typed partition is the raw tally.
		pool, typed = *hyp, pl.ManaUnits()
	case len(pl.RestrictedMana) == 0:
		pool, typed = pl.Pool, pl.ManaUnits()
	default:
		av := pay.AvailableFor(asPayer(e), p, paymentFor(id, ability, *c))
		pool, typed = av.Pool, av.Typed
	}
	return e.manaFeasiblePoolP(p, id, ability, c, mods, taxGeneric, delve, pool, typed)
}

// effectZoneOK reports whether a static whose EffectZone$ reads v applies
// while its source sits in zone z. Forge's default is the battlefield, and
// the corpus names zones with Forge's comma-separated All/Battlefield/Stack/
// Graveyard/Hand/Library/Exile/Command words; anything unrecognised denies
// (the same fail-closed direction every unparseable static qualifier takes),
// because wrongly applying a hand-or-library static is exactly the class of
// over-reach the zone gate exists to prevent.
func effectZoneOK(v string, z state.Zone) bool { return chars.EffectZoneOK(v, z) }

// composedPoolFloor is composeFeasibleP(m, c, taxGeneric, delve)
// .PoolUnitsFloor(). Under the zero composition (costModsZero) on a cost
// with no XMin and no negative Life/Snow, apply only clamps Generic and the
// coloured pips at zero, so the floor is computed from *c directly instead
// of materialising the composed Cost; walkSkipVerify checks it against the
// composition.
func composedPoolFloor(m *costMods, c *Cost, taxGeneric, delve int32) int64 {
	if !pay.CostModsZero(m) || c.XMin != 0 || c.Life < 0 || c.Snow < 0 {
		cc := pay.ComposeFeasibleP(m, c, taxGeneric, delve)
		return cc.PoolUnitsFloor()
	}
	g := addClampedGeneric(addClampedGeneric(c.Generic, 0), int64(taxGeneric))
	if g > delve {
		g -= delve
	} else {
		g = 0
	}
	n := int64(g)
	for _, letter := range pipLetters {
		if letter == 'B' {
			continue
		}
		if k := c.Colored[state.ManaIndex(letter)]; k > 0 {
			n += int64(k)
		}
	}
	if walkSkipVerify {
		if cc := pay.ComposeFeasibleP(m, c, taxGeneric, delve); cc.PoolUnitsFloor() != n {
			panic(fmt.Sprintf("rules: zero-composition pool floor %d disagrees with the composed %d", n, cc.PoolUnitsFloor()))
		}
	}
	return n
}

var modeIsCastFaceDownTab = state.NewStrTable[bool](
	state.StrEntry[bool]{Key: "morphed", Val: true},
	state.StrEntry[bool]{Key: "megamorphed", Val: true},
	state.StrEntry[bool]{Key: "disguised", Val: true},
)
