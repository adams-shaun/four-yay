package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// emitProposalFlip records the FlipFace an alternate-face cast proposal
// (room_alt, adventure_alt, adventure_recast, aftermath, split_alt,
// defeat_cast) makes before its ordinary cast transaction, WITHOUT letting
// that flip count as game progress. emit clears the held-out no-progress
// state (suppressedCast/castAborts) on every state-changing event; the flip
// is one, but it is net no progress when the proposal is then reversed
// (abortCast flips the card back). Left cleared, pushCast captured the
// already-emptied maps as the proposal's pre-push state, so abortCast's
// F05-2 count restarted at zero on every attempt and the SECOND identical
// no-progress abort never held the option out: an Adventure cast the offer
// priced as payable but the flipped face could not pay was reversed and
// re-offered forever (the botbench flip_face livelock). Restoring the maps
// the proposal began with keeps the count across attempts; a cast that goes
// on to reach the stack clears them at its PutOnStack as before.
func (e *Engine) emitProposalFlip(id state.ObjID, before uint8, preSuppress map[state.ObjID]bool, preAborts map[state.ObjID]int32) {
	e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: int32(1 - int(before))})
	e.suppressedCast, e.castAborts = preSuppress, preAborts
}

// dropProposalTriggers removes the pending triggers a reversed spell
// proposal's own CR 601.2c target choice queued (pc.proposalTriggers). CR
// 733.1: "No abilities trigger and no effects apply as a result of an undone
// action." Only the recorded ranges go -- a trigger an unreversed mana
// ability produced during the payment window (Manabarbs) stays queued, since
// the engine never reverses those activations. Ranges are removed back to
// front so an earlier range's indices are unaffected; a range the queue no
// longer covers (it was drained, which a live proposal never does) is skipped
// rather than trusted, and an ordered prefix is never touched.
func (e *Engine) dropProposalTriggers(pc *pendingCast) {
	ranges := pc.proposalTriggers
	pc.proposalTriggers = nil
	for i := len(ranges) - 1; i >= 0; i-- {
		start, end := ranges[i][0], ranges[i][1]
		if start < e.orderedTriggers || start >= end || end > len(e.pendingTriggers) {
			continue
		}
		e.noteTrigShrink()
		e.pendingTriggers = append(e.pendingTriggers[:start], e.pendingTriggers[end:]...)
	}
}

// castWindowUnits is the CR 601.2g cast-payment window's provable mana reach:
// the shared fixed-production census (windowManaUnits) plus each
// choice-shaped source (Any, Combo, Chosen) as single-colour alternatives of
// the SAME permanent, never as another tap, plus the cast-only paid/dynamic
// layer (castWindowPaidUnits) for the deterministic activation shapes the
// shared census cannot price. The shared census omits those because an
// unless-pay window cannot pose their colour or payment sub-ask; cast
// payment can. The result is filtered to the sources manaWindowAsk will
// actually offer (a permanent this cast already committed to Convoke or
// Conspire is withheld).
func (e *Engine) castWindowUnits(pc *pendingCast) []windowManaUnit {
	p := pc.player
	windowUnits := e.windowManaUnits(p)
	windowUnits = e.castWindowProbeUnits(pc, windowUnits)
	// SUPERSET GUARD (the anti-abort invariant): a source this cast already
	// committed to Convoke/Harmonize/Improvise (pc.convoke) or Conspire
	// (pc.taps) is NOT offered by manaWindowAsk (cast.go's convokeCommitted
	// filter), so the probe must not promise its tap either -- the cost fold
	// already credits its contribution, and counting the permanent a second
	// time would let the probe claim reach the window cannot complete.
	out := windowUnits[:0]
	for _, u := range windowUnits {
		if e.convokeCommitted(pc, u.id) {
			continue
		}
		out = append(out, u)
	}
	return out
}

// castWindowProbeUnits is castWindowUnits' cast-only activation-cost layer.
// It adds the shapes the shared windowManaUnits withholds from EVERY payment
// window: choice-shaped productions (a colour chosen when the source is
// tapped), free abilities whose Amount$ is not a literal, and non-free
// activation costs (a literal generic <N>, a PayLife<N>, a deterministic
// self-sacrifice, or a choice-shaped production behind any of them). The
// conservatism of windowManaUnits is load-bearing for the attack-cost and
// unless-cost windows, which cannot pose a sub-ask while tapping; the CR
// 601.2g cast window CAN: manaWindowAsk offers any currently payable
// non-InstantSpeed mana source, including a tapped source with a {Q} ability,
// and activation runs through resolveManaAbilityRef, which pays the full
// activation cost and evaluates the Amount$ body. Every source added here
// comes from the same availableManaAbilitiesForWindow walk manaWindowAsk uses,
// so the probe can never promise an activation the window will not offer.
//
// A literal generic <N> activation cost is carried on the alt as costGeneric
// rather than netted into the production: the cast-only eligibility search
// (castWindowReachable) pays it from the mana this window has already
// accumulated, so the fee can be funded by an earlier same-window activation
// and a multi-colour production stays expressible without choosing which
// colour the generic consumed. A PayLife<N> activation carries its life on
// the alt so the search debits it. The CHOICE-SHAPED production is split
// into one alt per producible colour, exactly the shared walk's
// one-alt-per-alternative shape, so the payer's colour choice is made by
// picking an alt and never needs a nested sub-ask.
//
// Deliberately EXCLUDED (fail closed), each for a named reason:
//
//   - InstantSpeed$ True abilities: already withheld by the shared walk.
//   - RestrictValid$-governed abilities: the produced batch may not pay the
//     priced cost, and the dotted matcher only admits a subset of the
//     grammar, so no restriction is priced here (AGENTS.md row 1's
//     direction).
//   - tapXType, SubCounter, Mill, UnlessCost$, Return<>, coloured activation
//     pips, multi-part or overlapping Sac costs, loyalty-ability mana
//     producers (never exposed by availableManaAbilities), and every
//     Amount$ body the count evaluator does not understand.
func (e *Engine) castWindowProbeUnits(pc *pendingCast, windowUnits []windowManaUnit) []windowManaUnit {
	p := pc.player
	pl := e.G.Players[p]
	for _, id := range e.battlefieldManaSourceIDs(p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, ma := range e.castWindowProbeAbilities(p, id) {
			if strings.TrimSpace(ma.Params["RestrictValid"]) != "" {
				continue
			}
			cost := e.parseCost(ma.ParamStr(cards.PKCost))
			if activationTapCostUnavailable(o, &cost) {
				continue
			}
			lifeCost := int32(0)
			genericCost := int32(0)
			free := manaFreeCost(cost)
			switch {
			case free:
				// windowManaUnits already counted a free PLAIN literal
				// production; only a choice-shaped or dynamic amount it
				// withholds is added here (the skip test runs below, after
				// the production is parsed).
			case castWindowPayLifeCost(cost):
				lifeCost = cost.Life
				if pl.Life <= lifeCost {
					continue
				}
			case castWindowGenericCostShape(cost):
				genericCost = cost.Generic
			case e.castWindowSelfSacCost(p, id, cost):
			default:
				continue
			}
			amt, ok := e.castWindowAmount(p, id, o, ma)
			if !ok {
				continue
			}
			// Chosen is an as-enters read: substitute the recorded colour
			// BEFORE the parse so a "Combo R Chosen" land recorded as G
			// offers R/G only, never the raw parser's WUBRG superset. Without
			// a record it produces no colour and stays out of this window
			// (the same fail-closed direction the shared walk takes).
			produced := strings.TrimSpace(ma.ParamStr(cards.PKProduced))
			if producedNeedsChosen(produced) {
				chosen := e.chosenProducedColour(id)
				if chosen == "" {
					continue
				}
				produced = substituteChosenProduced(produced, chosen)
			}
			counts, any := cards.ProducedCounts(produced)
			total := int32(0)
			for _, n := range counts {
				total += n
			}
			if total <= 0 {
				continue
			}
			if free {
				// A free plain literal production is windowManaUnits' domain
				// (already counted); a free choice-shaped one is not, and a
				// free dynamic amount is the shape this layer adds.
				if !any && availableAmount(ma) > 0 {
					continue
				}
			}
			if any {
				for colour, n := range counts {
					if n == 0 {
						continue
					}
					var single [6]int32
					single[colour] = 1
					windowUnits = appendCastWindowAlt(windowUnits, id, ma, single, amt, lifeCost, genericCost)
				}
				continue
			}
			windowUnits = appendCastWindowAlt(windowUnits, id, ma, counts, amt, lifeCost, genericCost)
		}
	}
	return windowUnits
}

// castWindowProbeAbilities is the cast-window probe's own member walk: the
// same gates as availableManaAbilitiesForWindow, EXCEPT the live-pool
// payability gate, which a CR 601.2g window can satisfy later in the same
// window (appendAvailableManaAbilitiesGate's ignorePayable). InstantSpeed$
// True abilities stay withheld exactly as the shared window walk withholds
// them, so no ability the live payment window cannot activate is ever priced.
func (e *Engine) castWindowProbeAbilities(p state.PlayerID, id state.ObjID) []*cards.SA {
	var out []*cards.SA
	for _, ma := range e.appendAvailableManaAbilitiesGate(nil, nil, p, id, true) {
		if e.instantSpeedOnly(ma) {
			continue
		}
		out = append(out, ma)
	}
	return out
}

// castWindowAmount resolves a window mana ability's Amount$ the way the
// activation's own manaEffectAmount does -- the source face's SVar table and
// effects.Num's grammar -- but with the resolvability verdict the count
// ratchet demands: effects.NumResolvedStrict rejects a named SVar whose
// Count$ body the evaluator does not model, and the inline `Amount$ Count$...`
// form (which Strict does not itself re-check) is verified with EvalCountOK.
// A body that does not resolve deterministically, or resolves to zero or
// less, is not priced.
func (e *Engine) castWindowAmount(p state.PlayerID, source state.ObjID, o *state.Object, ma *cards.SA) (int32, bool) {
	raw := strings.TrimSpace(ma.ParamStr(cards.PKAmount))
	if raw == "" {
		return 1, true
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v <= 0 {
			return 0, false
		}
		return int32(v), true
	}
	ctx := e.manaAmountCtx(p, source)
	effects.SetSVars(ctx, o.Face().SVars)
	n, ok := effects.NumResolvedStrict(e, ctx, ma, "Amount", 1)
	if !ok || n <= 0 {
		return 0, false
	}
	ref := raw
	if len(ref) > 1 && (ref[0] == '+' || ref[0] == '-') {
		ref = ref[1:]
	}
	if strings.HasPrefix(ref, "Count$") {
		if _, evaluated := effects.EvalCountOK(e, ctx, ref); !evaluated {
			return 0, false
		}
	}
	return n, true
}

// castWindowPayLifeCost reports whether c is a PayLife<N> activation cost
// this probe can price: the fixed life part and nothing else (bar the tap).
// An announced PayLife<X> (LifeX), a life-halving token and every other cost
// component are refused.
func castWindowPayLifeCost(c Cost) bool {
	return c.Life > 0 && c.Generic == 0 && c.Colored == (state.Mana{}) &&
		len(c.Sac) == 0 && castWindowOtherPartsAbsent(c)
}

// castWindowGenericCostShape reports whether c is a literal generic <N>
// activation cost this probe can price: exactly one literal generic mana and
// nothing else (bar the tap). A coloured pip, an {X} component or any other
// part is refused.
//
// The pool is deliberately NOT consulted here: the fee may be funded by an
// earlier same-window activation, which the cast-only eligibility search
// (castWindowReachable) proves. The live window offers this source once it is
// untapped and re-checks payability at each activation
// (manaAbilityPayablePool), so promising only a source the search can actually
// fund stays sound.
func castWindowGenericCostShape(c Cost) bool {
	return c.Generic > 0 && c.Colored == (state.Mana{}) &&
		len(c.Sac) == 0 && castWindowOtherPartsAbsent(c)
}

// castWindowSelfSacCost reports whether c is a self-sacrifice activation cost
// ("Sac<1/CARDNAME>") whose batch is deterministic: the only matching
// permanent is the source itself, so the interactive continuation
// (manaDiscardActivation) sacrifices it without a further ask. Any other Sac
// shape (multi-part, overlapping, multiple candidates) is refused.
func (e *Engine) castWindowSelfSacCost(p state.PlayerID, source state.ObjID, c Cost) bool {
	if len(c.Sac) != 1 || c.Sac[0].N != 1 || !strings.EqualFold(sacrificeMatchSpec(c.Sac[0].Spec), "CARDNAME") {
		return false
	}
	if c.Generic != 0 || c.Life != 0 || c.Colored != (state.Mana{}) || !castWindowOtherPartsAbsent(c) {
		return false
	}
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.sacrificeBlockedForCost(id, costCauseActivated) {
			continue
		}
		if e.matchesSpecFrom(c.Sac[0].Spec, id, p, source) {
			n++
		}
	}
	return n == 1
}

// castWindowOtherPartsAbsent reports whether c carries none of the cost
// components the paid-cost layer does not price. It is deliberately broader
// than manaFreeCost (which only needs a bare tap): every part whose payment
// needs a choice, an event or a resource this probe does not model is
// refused, as is any token the parser did not understand.
func castWindowOtherPartsAbsent(c Cost) bool {
	return len(c.Discard) == 0 && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.ExileFromTop) == 0 && len(c.Reveal) == 0 && len(c.RevealOrChoose) == 0 && len(c.RevealChosen) == 0 &&
		len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.Blight) == 0 &&
		len(c.Exert) == 0 && !c.Forage && !c.LifeHalfUp && len(c.Draw) == 0 &&
		len(c.Energy) == 0 && len(c.LifeX) == 0 && len(c.DamageYou) == 0 &&
		len(c.GainLife) == 0 &&
		len(c.Return) == 0 && len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 &&
		len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 &&
		len(c.Unknown) == 0 && len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 &&
		len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 && c.Snow == 0 && c.X == 0
}

// appendCastWindowAlt merges one production alternative into the unit for id
// (creating it if absent), so the affordability search can never tap the same
// permanent twice through two separate unit entries.
func appendCastWindowAlt(units []windowManaUnit, id state.ObjID, ma *cards.SA, counts [6]int32, amt, life, costGeneric int32) []windowManaUnit {
	idx := -1
	for i := range units {
		if units[i].id == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		units = append(units, windowManaUnit{id: id})
		idx = len(units) - 1
	}
	units[idx].alts = append(units[idx].alts, windowManaAlt{ma: ma, counts: counts, amt: amt, life: life, costGeneric: costGeneric})
	return units
}

// castWindowReachable is the cast-only CR 601.2g eligibility search. It is
// unlessManaReachable plus the one transition the cast window has and the
// attack/unless windows do not: an activation may PAY a literal generic fee
// (windowManaAlt.costGeneric) and a life fee before its production is added,
// and that fee may be funded by mana an earlier same-window activation
// already produced. The live window offers one source at a time and
// re-enters after each activation, so the search explores executable source
// orders and never claims a paid source it cannot fund. Units are visited in
// stable fee/id order, but every remaining source can be selected next;
// every shared-window alt carries costGeneric 0 so the attack/unless callers
// of unlessManaReachable are untouched.
//
// spellPool is the payer's restriction-adjusted pool for the SPELL (the same
// projection unlessManaReachable receives); activation fees are funded from
// the payer's real pool minus every restricted batch a non-matching
// descriptor would hide (unrestrictedWindowPool), never from spell-restricted
// mana, so the sequence is executable under the live activation gate. Every
// produced unit is unrestricted (both probe walks exclude RestrictValid$
// abilities).
func (e *Engine) castWindowReachable(p state.PlayerID, cost Cost, spellPool, snow state.Mana,
	typed [7]state.Mana, life int32, conv *manaConv, units []windowManaUnit) bool {
	payable := func(pool, snowPool state.Mana, lifeNow int32) bool {
		_, ok := resolveManaWith(cost, pool, snowPool, typed, lifeNow,
			e.payerGrantsPayLifeInsteadOfB(p), pipRider{}, conv)

		return ok
	}
	if payable(spellPool, snow, life) {
		return true
	}
	free := e.unrestrictedWindowPool(p)
	// Stable free-first ordering: a free alt sorts before a paid one; ties
	// retain the zone walk's order, with the id as a determinism guard.
	ordered := append([]windowManaUnit(nil), units...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0; j-- {
			if !castWindowUnitLess(ordered[j], ordered[j-1]) {
				break
			}
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	nodes := 0
	used := make([]bool, len(ordered))
	var rec func(pool, spellSnow, activationPool, activationSnow state.Mana, lifeLeft int32) bool
	rec = func(pool, spellSnow, activationPool, activationSnow state.Mana, lifeLeft int32) bool {
		if payable(pool, spellSnow, lifeLeft) {
			return true
		}
		nodes++
		if nodes > 1<<18 {
			return false
		}
		for i := range ordered {
			if used[i] {
				continue
			}
			for _, a := range ordered[i].alts {
				if a.life > lifeLeft {
					continue
				}
				// Pay a generic activation fee from mana the live activation
				// gate can spend. Track the same spent colours in the spell
				// pool; fees reduce that pool, they are not extra spell pips.
				activationFee, feeOK := resolveMana((Cost{Generic: a.costGeneric}),
					activationPool, activationSnow, [7]state.Mana{}, lifeLeft, nil)

				if !feeOK {
					continue
				}
				spent := manaSub(activationPool, activationFee.pool)
				snowSpent := manaSub(activationSnow, activationFee.snow)
				nextPool := manaSub(pool, spent)
				nextSpellSnow := manaSub(spellSnow, snowSpent)
				nextActivation := activationFee.pool
				nextActivationSnow := activationFee.snow
				produced := a.mana()
				used[i] = true
				found := rec(manaAdd(nextPool, produced), nextSpellSnow,
					manaAdd(nextActivation, produced), nextActivationSnow, lifeLeft-a.life)
				used[i] = false
				if found {
					return true
				}
			}
		}
		return false
	}
	return rec(spellPool, snow, free, snow, life)
}

func manaSub(a, b state.Mana) state.Mana {
	var m state.Mana
	for i := range m {
		m[i] = a[i] - b[i]
		if m[i] < 0 {
			m[i] = 0
		}
	}
	return m
}

// castWindowUnitLess orders a unit before another when its cheapest
// alternative is cheaper in generic activation cost, tie-broken by zone id,
// giving castWindowReachable a stable traversal order.
func castWindowUnitLess(a, b windowManaUnit) bool {
	ka, kb := int32(-1), int32(-1)
	for _, x := range a.alts {
		if ka < 0 || x.costGeneric < ka {
			ka = x.costGeneric
		}
	}
	for _, x := range b.alts {
		if kb < 0 || x.costGeneric < kb {
			kb = x.costGeneric
		}
	}
	if ka != kb {
		return ka < kb
	}
	return a.id < b.id
}

// unrestrictedWindowPool is the payer's real floating pool minus every
// restricted batch (a non-empty Valid) a non-matching descriptor would hide.
// Only these units are guaranteed spendable on a mana-ability activation
// regardless of the activation's own restriction terms, so castWindowReachable
// funds activation fees from them and never from spell-restricted mana.
// Empty-Valid batches are unrestricted (Boseiju's AddsNoCounter$ provenance)
// and stay counted, matching manaAvailableFor's own rule.
func (e *Engine) unrestrictedWindowPool(p state.PlayerID) state.Mana {
	pl := e.G.Players[p]
	free := pl.Pool
	for _, r := range pl.RestrictedMana {
		if r.Valid == "" {
			continue
		}
		idx := state.ManaSlot(r.Color)
		if free[idx] < r.Amount {
			free[idx] = 0
		} else {
			free[idx] -= r.Amount
		}
	}
	return free
}

// striveAffordableTargets is the Decision.AffordableTargets hint for a Strive
// spell's target ask: the largest n in [1, max] whose total cost -- the
// resolved cost, n-1 Strive payments (CR 702.52a), the proposal's modifier
// snapshot, commander tax, Delve credit and announced Convoke -- the caster
// can provably pay from the pool plus the window's fixed productions
// (castWindowUnits, the same probe the target-discount gate trusts). It
// returns 0 (no hint) when the proposal carries no priceable Strive or max is
// at most one; it returns at least 1 otherwise, since one target adds no
// Strive charge and the ask's own gate already admitted the base cost. A pure
// read: nothing is emitted.
func (e *Engine) striveAffordableTargets(pc *pendingCast, max int) int {
	if pc.isAbility() || !pc.striveSet || max <= 1 {
		return 0
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return 0
	}
	if _, ok := o.Face().KeywordParam("Strive"); !ok {
		return 0
	}
	sc := ParseCost(pc.striveParam)
	if len(sc.Unknown) > 0 {
		return 0
	}
	pl := e.G.Players[pc.player]
	var units []windowManaUnit
	unitsBuilt := false
	affordable := func(n int) bool {
		cost := pc.resolvedMana()
		for i := int(pc.striveUnits); i < n-1; i++ {
			cost = cost.Plus(sc)
		}
		cost = pc.mods.apply(cost)
		cost.Generic = addClampedGeneric(cost.Generic, int64(pc.taxGeneric))
		cost.Generic -= int32(len(pc.delve))
		if cost.Generic < 0 {
			cost.Generic = 0
		}
		convoked := e.applyConvoke(pc, cost)
		pay := paymentForCast(pc, convoked)
		rider := pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}
		if e.manaFeasibleDescriptor(pc.player, pay, convoked, costMods{}, 0, 0, rider) {
			return true
		}
		if !convoked.HasManaPayment() {
			return false
		}
		if !unitsBuilt {
			units, unitsBuilt = e.castWindowUnits(pc), true
		}
		av := e.manaAvailableFor(pc.player, pay)
		return e.castWindowReachable(pc.player, convoked, av.pool, pl.Snow, av.typed, pl.Life,
			e.paymentConv(pc.player, pay.id, pay.class == paymentActivated), units)
	}
	// Ascending: the price is monotone in n, so the first unaffordable count
	// ends the walk and at most one exhaustive (failing) window search runs.
	best := 1
	for n := 2; n <= max; n++ {
		if !affordable(n) {
			break
		}
		best = n
	}
	return best
}
