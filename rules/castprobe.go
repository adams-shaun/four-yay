package rules

import (
	"slices"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// castprobe.go: judging a spell's CR 601.2c target census the way the cast
// will, with the card already on the stack.
//
// CR 601.2a moves the card from its zone to the stack BEFORE CR 601.2c asks
// for its targets, so a target whose legality reads the caster's hand is
// judged with the hand one card smaller. The offer census (castTargetsAvailable)
// runs with the card still in hand. Measured (round-9 cardfuzz explore seed
// 9606575608234985872): Empyrial Armor ("+1/+1 for each card in your hand")
// on an opponent's 1/3 made it a 4/6 with three cards in hand, so Guiding
// Bolt ("destroy target creature with power 4 or greater") passed the census;
// on the stack the hand held two and the creature was a 3/5, so the cast
// aborted with no legal target. The payment-plan offer (paymentActionsForPriority)
// offered that cast as a one-click plan, which then reversed (CR 733.1).
//
// offerAsSpellOnStack answers fn with id moved from its hand to the top of
// the stack and puts it back before returning. Like offerAsFace
// (faceprobe.go) it is a scoped READ: no event is emitted, the hand list is
// restored to the identical slice (the probe writes only fresh copies), the
// stack to its own slice, and the object's zone to its value -- so the log,
// the hash chain and replay are untouched. The log-head-keyed layer caches
// are brought up to date BEFORE the move, so they are hits throughout (none
// of them holds a hand count: a Count$ValidHand amount is evaluated when the
// characteristic is derived), the walk's Derived memo is bypassed for the
// probe, and the cross-walk memo is retired at both edges, exactly as the
// face probe does. The static-walk zone summaries are keyed on the live id
// list (static_zoneskip.go), so the probed hand list can never be served a
// summary of the other.
func (e *Engine) offerAsSpellOnStack(id state.ObjID, fn func() bool) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand {
		return fn()
	}
	hand := e.G.Zone(state.ZHand, o.Owner)
	i := slices.Index(hand, id)
	if i < 0 {
		return fn()
	}
	_ = e.active()
	e.refreshDerivedTypes()
	probeHand := make([]state.ObjID, 0, len(hand)-1)
	probeHand = append(append(probeHand, hand[:i]...), hand[i+1:]...)
	prevStack, prevZone := e.G.Stack, o.Zone
	prevDepth, prevGen := e.derivedMemoDepth, e.derivedMemoGen
	e.G.SetZone(state.ZHand, o.Owner, probeHand)
	e.G.Stack = append(slices.Clip(prevStack), id)
	o.Zone = state.ZStack
	e.derivedMemoDepth = 0
	// The object classes (walk_objclass.go) cache nothing read under the
	// move, whose fingerprint would outlive it.
	e.offerProbeDepth++
	e.retireCrossWalkMemo()
	defer func() {
		e.offerProbeDepth--
		o.Zone = prevZone
		e.G.Stack = prevStack
		e.G.SetZone(state.ZHand, o.Owner, hand)
		e.derivedMemoDepth = prevDepth
		e.retireCrossWalkMemo()
		if e.derivedMemoGen != prevGen {
			e.derivedMemoGen++
		}
	}()
	return fn()
}

// castTargetsAvailableOnStack is castTargetsAvailable judged by
// offerAsSpellOnStack: the census the CR 601.2c target ask will run once the
// card has moved to the stack. Only a spell with a ValidTgts$ census is
// probed; every other spell is answered by the ordinary census unchanged.
func (e *Engine) castTargetsAvailableOnStack(p state.PlayerID, id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return true
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.Params["ValidTgts"] == "" && sa.API != "Charm" {
		return true
	}
	return e.offerAsSpellOnStack(id, func() bool { return e.castTargetsAvailable(p, id, sa) })
}

// paymentPlanHoldsOnStack reports whether every activation of plan still
// resolves to its exact witnessed alternative (paymentPlanStepAlternative:
// the same ability identity, production and consequence) with id on the
// stack -- the board the plan executes on. The executor revalidates the plan
// in the spell's CR 601.2g mana-ability window, which opens only after CR
// 601.2a has moved the card from the hand to the stack, so a mana ability
// whose activation restriction or amount reads the caster's hand must be
// judged with the hand one card smaller. Measured (round-10 paymirror random2
// seed 11828 seq 4243): Fanatic of Rhonas's "{T}: Add {G}{G}{G}{G}. Activate
// only if you control a creature with power 4 or greater" held only through
// Syr Elenora (power = cards in hand) at 4 power while Vorinclex was in hand;
// in the window she was a 3/4, the step fell back source_changed and the cast
// reversed with seven of its eight mana.
//
// Only a plan with a printed (non-intrinsic) step is probed: an intrinsic
// basic-land ability carries no activation restriction and a literal
// production, and the plans that tap only basic land types are the common
// case the offer builds on every priority decision. The census runs outside
// the offer's query scope, whose cached units describe the in-hand board.
func (e *Engine) paymentPlanHoldsOnStack(p state.PlayerID, id state.ObjID, plan decision.PaymentPlan) bool {
	probe := false
	for _, pa := range plan.Activations {
		if pa.Ability.Kind != decision.PaymentAbilityIntrinsic {
			probe = true
			break
		}
	}
	if !probe {
		return true
	}
	return e.offerAsSpellOnStack(id, func() bool {
		prevQuery := e.paymentPlanQuery
		e.paymentPlanQuery = nil
		defer func() { e.paymentPlanQuery = prevQuery }()
		// Only the plan's own sources are resolved, so the census is taken
		// for those alone (exactly the full census's units for them).
		only := make([]state.ObjID, 0, len(plan.Activations))
		for _, pa := range plan.Activations {
			only = append(only, pa.Source)
		}
		units := e.paymentPlanManaUnitsOnly(p, only)
		for _, pa := range plan.Activations {
			if _, ok := e.paymentPlanStepAlternative(units, pa); !ok {
				return false
			}
		}
		return true
	})
}
