package rules

import (
	"regexp"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// targetBoundReadsPromisedGift limits the alternate-branch offer check to
// target bounds whose SVar table can read Count$PromisedGift. It is a
// conservative over-approximation: a SVar that reads PromisedGift for a
// non-bound purpose also trips it. That is the safe direction for an
// OFFER-feasibility union (it can only widen the offered set, never narrow
// it, and the post-election ask still reads the elected branch), so the
// false-positive cost is an offered cast whose only feasible branch the
// player then does not elect -- the ordinary CR 601.2c reversal.
func targetBoundReadsPromisedGift(o *state.Object, sa *cards.SA) bool {
	if o == nil || o.Face() == nil || sa == nil {
		return false
	}
	if strings.Contains(sa.ParamStr(cards.PKTargetMin), "Count$PromisedGift") ||
		strings.Contains(sa.ParamStr(cards.PKTargetMax), "Count$PromisedGift") {
		return true
	}
	for _, body := range o.Face().SVars {
		if strings.Contains(body, "Count$PromisedGift") {
			return true
		}
	}
	return false
}

// targetSAAvailable reports whether a target declaration admits at least one
// legal announcement at offer time: enough legal candidates for its resolved
// mandatory minimum AND the cross-target set constraints the post-push ask
// enforces (per-controller exclusivity/OneEach, same-controller group capacity
// and set-property capacity). It is still not a full cast/payment check:
// target-dependent cost modifiers and the final target announcement belong to
// the post-push ask. CR 601.2c is the reason the offer must not admit a
// declaration the ask will reverse (CR 733.1) the instant it is submitted.
func (e *Engine) targetSAAvailable(p state.PlayerID, id, excludeSelf state.ObjID, sa *cards.SA, x int32, xPending bool) bool {
	if sa == nil || strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)) == "" {
		return true
	}
	// A pending {X} is announced before targets, so a bare X bound cannot be
	// judged at offer time. Keep that shape offerable and let targetAsk use the
	// settled value. SVar-backed bounds remain readable now and are checked.
	// Read by literal key: the param census's rot guard rejects a dynamic
	// Params key that is not a function parameter.
	if xPending && (strings.EqualFold(strings.TrimSpace(sa.Params["TargetMin"]), "X") ||
		strings.EqualFold(strings.TrimSpace(sa.Params["TargetMax"]), "X")) {
		return true
	}
	min, _ := e.resolvedTargetBounds(p, id, sa, x)
	// CR 702.168a: the Gift election has not been made yet when this census
	// runs, so a bound that reads Count$PromisedGift is feasible if EITHER
	// branch of the promise is satisfiable. The union is a min of the two
	// branches' minimums: the census compares candidates against min, and a
	// smaller min is satisfiable exactly when at least one branch is. The
	// post-election ask (and every other reader) still resolves the ELECTED
	// branch through the nil-override resolvedTargetBounds, so promising
	// remains what switches the actual bound.
	if targetBoundReadsPromisedGift(e.G.Obj(id), sa) {
		promised := true
		if pmin, _ := e.resolvedTargetBoundsWithGift(p, id, sa, x, &promised); pmin < min {
			min = pmin
		}
	}
	// The census is a pure read; the two answers that never look at the
	// population return before walking it.
	if min <= 0 {
		return true
	}
	if xPending && specNamesXBound(sa.Params["ValidTgts"]) {
		return true
	}
	if capCMC, capped := e.maxTotalTargetCMC(p, id, sa, x); capped {
		candidates := e.legalTargetCandidates(p, id, excludeSelf, sa)
		candidates, _, _ = e.totalCMCCappedCandidates(candidates, p, id, sa, x)
		if !e.targetChoiceFeasible(sa, candidates, min) {
			return false
		}
		// A subset-sum cap binds even when enough individually legal
		// candidates exist: the cheapest `min` of them is the minimal-sum
		// subset of that size (mana values are nonnegative), so if it busts
		// the cap no legal subset does. Every current corpus carrier has
		// TargetMin$ 0, so this arm is prophylactic. Derive the effective
		// minimum the same way targetChoiceFeasible does (the OneEach/
		// same-controller/set-property bounds can lower min) so both readers
		// reason about the same count.
		eff := min
		eff, _, _, _ = e.oneEachTargetBounds(sa, candidates, eff, 0)
		eff, _, _, _ = e.sameControllerTargetBounds(sa, candidates, eff, 0)
		eff, _, _, _, _ = e.setPropTargetBounds(sa, candidates, eff, 0)
		if eff > 0 {
			vms := make([]int, 0, len(candidates))
			for _, c := range candidates {
				if c.kind == "player" {
					// Players are valid members of a target set and contribute
					// zero to its total mana value.
					vms = append(vms, 0)
					continue
				}
				if o := e.G.Obj(c.obj); o != nil && o.Face() != nil {
					vms = append(vms, int(o.Face().ManaValue()))
				}
			}
			if len(vms) < eff {
				return false
			}
			slices.Sort(vms)
			total := 0
			for _, mv := range vms[:eff] {
				total += mv
			}
			return total <= capCMC
		}
		return true
	}
	if !targetCrossConstrained(sa) {
		// No cross-target constraint: targetChoiceFeasible reduces to
		// len(candidates) >= min, which the limited census answers exactly
		// (candidatesForLimit stops at min only when no post-filter can drop
		// a candidate) without matching the rest of the population -- an
		// any-target spell stops at the player seats.
		ok := e.candidatesCountForLimit(p, id, excludeSelf, sa, true, min) >= min
		if walkCacheVerify && ok != e.targetChoiceFeasible(sa, e.legalTargetCandidates(p, id, excludeSelf, sa), min) {
			panic("rules: limited target census disagrees with the full census")
		}
		return ok
	}
	candidates := e.legalTargetCandidates(p, id, excludeSelf, sa)
	return e.targetChoiceFeasible(sa, candidates, min)
}

// targetCrossConstrained reports whether sa carries a cross-target
// constraint targetChoiceFeasible reads beyond the candidate count: per-
// controller exclusivity (oneEachTargetBounds), same-controller grouping
// (sameControllerTargetBounds) or a set-property constraint
// (setPropTargetBounds). Each of those helpers is the identity on min when
// its predicate here is false.
func targetCrossConstrained(sa *cards.SA) bool {
	if targetControllerExclusive(sa) || strings.EqualFold(sa.ParamStr(cards.PKTargetsWithSameController), "True") {
		return true
	}
	mode, _ := targetSetPropMode(sa)
	return mode != decision.SetPropNone
}

// targetChoiceFeasible is the ONE home for whether a target declaration with
// the given candidate population admits at least one legal selection under
// every cross-target constraint the ask enforces: the resolved mandatory
// minimum (with TargetMin$/TargetMax$ OneEach respelled to the
// distinct-controller count), TargetsForEachPlayer$/TargetsWithDifferentControllers$
// exclusivity, TargetsWithSameController$ group capacity, and the
// set-property capacity. Both the offer census (targetSAAvailable) and the
// post-push cast ask (cast.go's targetAsk) call it against their OWN candidate
// list, so an offered cast can always announce a legal target and the two sites
// cannot drift. Cost-driven candidate pruning stays the ask's alone: only it
// can re-price a target-dependent cost modifier.
func (e *Engine) targetChoiceFeasible(sa *cards.SA, candidates []targetCandidate, min int) bool {
	if min <= 0 {
		return true
	}
	min, _, exclusive, distinct := e.oneEachTargetBounds(sa, candidates, min, 0)
	min, _, sameCapacity, sameController := e.sameControllerTargetBounds(sa, candidates, min, 0)
	min, _, setCapacity, setMode, _ := e.setPropTargetBounds(sa, candidates, min, 0)
	if len(candidates) < min {
		return false
	}
	if exclusive && min > distinct {
		return false
	}
	if sameController && min > sameCapacity {
		return false
	}
	if setMode != decision.SetPropNone && min > setCapacity {
		return false
	}
	return true
}

// charmTargetsAvailable evaluates the possible CR 601.2b mode announcement
// before a cast is offered. Target-bearing modes with an unsatisfiable
// mandatory minimum are removed from the possible announcement set, using
// the same target census castModeAsk uses after the spell reaches the stack.
// A mode whose target count depends on an announcement remains offerable.
func (e *Engine) charmTargetsAvailable(p state.PlayerID, id state.ObjID, sa *cards.SA, xPending bool) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return true
	}
	choices := strings.Split(sa.Params["Choices"], ",")
	ctx := &effects.Ctx{Source: id, Controller: p}
	effects.SetSVars(ctx, o.Face().SVars)
	legal := make([]string, 0, len(choices))
	for _, name := range choices {
		name = strings.TrimSpace(name)
		sub := cards.ResolveSVar(o.Face().SVars, name)
		if sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" &&
			!e.targetSAAvailable(p, id, id, sub, 0, xPending) {
			continue
		}
		legal = append(legal, name)
	}
	legal = effects.CharmEligibleModes(e, id, sa, legal)
	min, _, repeat := effects.CharmModeBounds(e, ctx, sa, len(legal))
	return repeat || min <= len(legal)
}

// targetsAvailable reports whether the target requirement that can be proved
// before offering a spell or ability is satisfiable. Dynamic announcements
// remain offerable until the post-announcement targetAsk backstop evaluates
// them. excludeSelf is the CR 115.5 object for a spell, or zero for an
// activated ability whose Source permanent may target itself.
// targetsReadXPending reports whether targetsAvailable(sa) reads its
// xPending argument at all: only the Choices$ charm census and a
// ValidTgts$ census behind no Announce$ do. Every other shape answers from
// sa alone, so an offer gate may skip pricing the X announcement (two parsed
// costs per card) for it.
func targetsReadXPending(sa *cards.SA) bool {
	if sa == nil {
		return false
	}
	if sa.API == "Charm" && strings.TrimSpace(sa.Params["Choices"]) != "" {
		return true
	}
	return strings.TrimSpace(sa.ParamStr(cards.PKAnnounce)) == "" && strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)) != ""
}

func (e *Engine) targetsAvailable(p state.PlayerID, id, excludeSelf state.ObjID, sa *cards.SA, xPending bool) bool {
	if sa == nil {
		return true
	}
	if sa.API == "Charm" && strings.TrimSpace(sa.Params["Choices"]) != "" {
		return e.charmTargetsAvailable(p, id, sa, xPending)
	}
	// An Announce$ value can change the target restriction itself; its value
	// is not available until the cast transaction reaches the announcement
	// stage, so retain the post-announcement backstop for that shape.
	if strings.TrimSpace(sa.ParamStr(cards.PKAnnounce)) != "" {
		return true
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)) == "" {
		return true
	}
	return e.targetSAAvailable(p, id, excludeSelf, sa, 0, xPending)
}

// costAnnouncesX reports whether paying this cost announces a value for {X}
// before targets are chosen: a printed {X} mana symbol or a PayEnergy<X>
// energy part (Forge announces both through the same ability X). A cost that
// announces X makes every ValidTgts$ bound that reads that X (cmcEQX and its
// siblings) a DYNAMIC bound -- the same carve-out TargetMin$/TargetMax$/
// Announce$/Choices$ already have -- so the offer gate does not withhold the
// action on a bound whose value does not exist yet; the post-announcement
// askTarget backstop evaluates it once the X is fixed (CR 601.2b before
// 601.2c).
// costAnnouncesX reports whether the cost announces an X the cast or
// activation chooses (CR 601.2b/107.3i): a printed {X} mana symbol, a
// PayEnergy<X> part, an announced PayLife<X> payment, an announced
// SubCounter<X/Kind> removal, or a tapXType<X/Spec> part whose tap election
// announces it (the dynTapCost head). The tap-election clause does not add
// the announced-Sac clause: this gate's callers treat a true answer as "the
// X-bound targets are dynamic -- offer and evaluate at the ask", and the
// tap election is announced BEFORE the target ask (the tap stage precedes
// targetAsk in continueCast), so the bound is already fixed when targets
// are chosen either way; the clause only stops the offer gate from
// withholding the action on a bound whose value the tap election will
// supply (Aryel's powerLEX).
func costAnnouncesX(c Cost) bool { return costAnnouncesXRef(&c) }

// costAnnouncesXRef is costAnnouncesX reading c in place.
func costAnnouncesXRef(c *Cost) bool {
	if c.X > 0 {
		return true
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return true
		}
	}
	if len(c.LifeX) > 0 {
		return true
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.TapPermanent {
		if part.Dyn == "X" {
			return true
		}
	}
	// An announced Blight<X> part binds the cast's X exactly like Sac<X/Spec>
	// (Blighted Nightmare's `Blight<X> Return<1/CARDNAME>` ability): the
	// payment settles the announced count, so the X was announced even when
	// its value is 0.
	for _, part := range c.Blight {
		if part.Announced {
			return true
		}
	}
	return false
}

// specNamesXBound reports whether a ValidTgts$ spec carries a numeric bound
// whose right-hand side is the paid {X}: the <field><CMP>X family numericPred
// resolves through SpecContext.Resolve (powerGEX, cmcEQX, toughnessLTX,
// counters_GTX_<KIND>). Only these shapes are dynamic in X; a literal bound
// (cmcGE3) is static and stays gated at offer time.
var xBoundRe = regexp.MustCompile(`(?i)(power|toughness|cmc)(LE|GE|EQ|LT|GT)X|counters_(?:LE|GE|EQ|LT|GT)X_`)

func specNamesXBound(spec string) bool {
	return xBoundRe.MatchString(spec)
}

// castTargetsAvailable is the cast-offer guard: the spell card may not target
// itself (CR 115.5), so excludeSelf is the card id. A cost that announces an
// X (printed {X} or a SpellAbility Cost$ PayEnergy<X>) relaxes an X-bound
// spec to the post-announcement backstop (costAnnouncesX above).
func (e *Engine) castTargetsAvailable(p state.PlayerID, id state.ObjID, sa *cards.SA) bool {
	xPending := false
	if !targetsReadXPending(sa) {
		// xPending is never read for this shape (targetsReadXPending).
		return e.targetsAvailable(p, id, id, sa, false)
	}
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		xPending = costAnnouncesXRef(&e.faceCompiledCost(o.Face()).Cost)
		if ab := o.Face().SpellAbility(); ab != nil {
			xPending = xPending || costAnnouncesXRef(e.costRef(ab.Params["Cost"]))
		}
	}
	return e.targetsAvailable(p, id, id, sa, xPending)
}

// abilityTargetsAvailable is the activated-ability offer guard. It is what
// stops an ability with no legal target from being re-offered in a loop after
// the transaction aborts it (CR 602.2b / 601.2c: such an ability cannot be
// activated at all). Unlike a cast, an activated ability CAN target its own
// Source permanent (Mother of Runes targeting itself), so no self-exclusion
// applies -- EXCEPT for an attach ability (API$ Attach, the Equip/Reconfigure
// expansion): CR 701.3a attaches to ANOTHER permanent, and targetAsk
// excludes the source from an attach SA's pool, so the offer must exclude it
// too. Without that, a creature that GAINED an Equip ability (Trazyn the
// Infinite with an Equipment in its owner's graveyard) as the only creature
// its controller had was offered an Equip whose ask then found no legal
// target and aborted, forever (cardfuzz batch5 line 1). An ability cost that
// announces an X (a {X} mana symbol or PayEnergy<X>) relaxes an X-bound spec
// to the post-announcement backstop.
func (e *Engine) abilityTargetsAvailable(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	var excludeSelf state.ObjID
	if ab != nil && ab.API == "Attach" {
		excludeSelf = id
	}
	if !targetsReadXPending(ab) {
		// xPending is never read for this shape (targetsReadXPending).
		return e.targetsAvailable(p, id, excludeSelf, ab, false)
	}
	return e.targetsAvailable(p, id, excludeSelf, ab, costAnnouncesX(e.parseCost(ab.Params["Cost"])))
}
