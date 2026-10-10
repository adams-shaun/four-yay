package pay

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// SacrificeCostCandidates returns, in battlefield scan order, the permanents
// that can pay one Sac cost part for a cast (ability=false) or an activation
// (ability=true) of source by p. Every Sac stage derives its candidate list
// from this one helper -- the X announcement's upper bound (xAsk), the
// sacrifice settle (sacAsk) and the offer gate's announced-X affordability
// sweep (offerCastableUsing) -- so the count an offer is priced on, the count
// the payer may announce, and the count the payment can settle cannot
// disagree about whether a self-reference or a CantSacrifice block is
// payable.
func SacrificeCostCandidates(e Engine, p state.PlayerID, source state.ObjID, part costvocab.CostPart, ability bool) []state.ObjID {
	matchSpec := SacrificeMatchSpec(part.Spec)
	cause := CostCauseForAbility(ability)
	var out []state.ObjID
	if part.Referent != 0 {
		// A granted ability's bound OriginalHost (bindGrantedCostReferents):
		// exactly the grantor, and only while the payer controls it on the
		// battlefield (CR 701.21a: only a permanent you control can be
		// sacrificed).
		r := part.Referent
		if slices.Contains(e.Game().Zone(state.ZBattlefield, p), r) && ExistsOnBattlefield(e.Game().Obj(r)) &&
			!e.CostBlocked(BlockSacrifice, r, cause) {
			out = append(out, r)
		}
		return out
	}
	if matchSpec == "CARDNAME" {
		// A bare self-reference matches exactly the source (CR 201.5; the
		// filter's CARDNAME base rejects every object whose ID is not
		// sc.Source, and a zero source matches nothing), so the scan below
		// can admit at most the source itself, at its battlefield position.
		// Test it alone instead of matching the whole battlefield: a mass of
		// Sac<1/CARDNAME> mana tokens (Eldrazi Spawn) otherwise makes every
		// payability check O(board) and the priority walk O(board^2).
		if source != 0 && slices.Contains(e.Game().Zone(state.ZBattlefield, p), source) &&
			ExistsOnBattlefield(e.Game().Obj(source)) && !e.CostBlocked(BlockSacrifice, source, cause) &&
			e.MatchesSpecFrom(matchSpec, source, p, source) {
			out = append(out, source)
		}
		if e.Verify() {
			if want := sacrificeCostScan(e, p, source, matchSpec, cause); !slices.Equal(out, want) {
				panic(fmt.Sprintf("rules: CARDNAME sacrifice fast path %v, full scan %v (source %d)", out, want, source))
			}
		}
		return out
	}
	return sacrificeCostScan(e, p, source, matchSpec, cause)
}

// sacrificeCostScan is sacrificeCostCandidates' full battlefield scan.
func sacrificeCostScan(e Engine, p state.PlayerID, source state.ObjID, matchSpec string, cause CostCause) []state.ObjID {
	var out []state.ObjID
	for _, oid := range e.Game().Zone(state.ZBattlefield, p) {
		if !ExistsOnBattlefield(e.Game().Obj(oid)) || e.CostBlocked(BlockSacrifice, oid, cause) {
			continue
		}
		if e.MatchesSpecFrom(matchSpec, oid, p, source) {
			out = append(out, oid)
		}
	}
	return out
}

// SacrificeCostAssignable tests whether all Sac parts can be paid with
// distinct permanents for an announced X. A per-part candidate count is
// insufficient: two parts can each have X candidates but share every one.
// Match each required sacrifice to an object, rerouting earlier matches when
// a later, narrower part needs one of their objects. This is an existence
// check, not a payment choice; sacAsk still lets the player choose the
// actual sacrifices in cost-part order.
func SacrificeCostAssignable(e Engine, p state.PlayerID, source state.ObjID, parts []costvocab.CostPart, ability bool, x int32) bool {
	candidates := make([][]state.ObjID, len(parts))
	for i, part := range parts {
		candidates[i] = SacrificeCostCandidates(e, p, source, part, ability)
		need := part.N
		if part.Announced {
			need = x
		}
		if need > int32(len(candidates[i])) {
			return false
		}
	}
	assigned := make(map[state.ObjID]int)
	var claim func(int, map[state.ObjID]bool) bool
	claim = func(i int, seen map[state.ObjID]bool) bool {
		for _, oid := range candidates[i] {
			if seen[oid] {
				continue
			}
			seen[oid] = true
			prev, used := assigned[oid]
			if !used || claim(prev, seen) {
				assigned[oid] = i
				return true
			}
		}
		return false
	}
	for i, part := range parts {
		need := part.N
		if part.Announced {
			need = x
		}
		for n := int32(0); n < need; n++ {
			if !claim(i, make(map[state.ObjID]bool)) {
				return false
			}
		}
	}
	return true
}

// CostCause names what a COST-path sacrifice is being paid for, the cost-side
// counterpart of causeSpecAdmits' actionCause(). A cost has no resolving
// object to attribute: an activated ability's costs are paid BEFORE its stack
// object exists (cast.go pushCast's pc.isAbility() early return), so the
// stack top would name whatever unrelated object was already there -- the
// exact misattribution trigmatch.DiscardCauseAdmits guards against. The pending act of
// casting/activating is therefore the only honest cause where one is pending,
// and the defined semantics per cost site (cantsac1 r2) are:
//
//	spell-cast cost component -> costCauseSpell
//	activated-ability cost component, mana abilities included -> costCauseActivated
//	ward cost (CR 702.22: the ward trigger demands the payment) -> costCauseTriggered
//	cumulative-upkeep payment and the Cost$ Mandatory trigger-cost window
//	(CR 702.25a: the upkeep/resolving trigger demands the payment) -> costCauseTriggered
//	unless payment (paid during a resolving ability to elect its outcome --
//	a resolution-election payment, never a cast or activation cost) -> costCauseResolution
//
// costCauseNone is no cost context at all: the effect path (the effects.Host
// method, forCost false) and a caller with nothing pending. causeCostAdmits
// reads Spell, Activated and Triggered; Resolution is inadmissible by every
// readable base, so a ValidCause$ line fails closed at an unless site (the
// permissive direction) instead of blocking a payment the resolving ability
// did not demand as its cast/activation cost. Every corpus ForCost$ True
// carrier is `ValidCause$ Spell,Activated` (angel_of_jubilation,
// yasharn_implacable_earth), so a ward, unless or upkeep payment is correctly
// OUTSIDE its scope: Angel stops sacrificing "to cast spells or activate
// abilities", and none of those three payments is one.
type CostCause uint8

const (
	CostCauseNone       CostCause = iota // no cost context (the effect path)
	CostCauseSpell                       // a component of casting a spell
	CostCauseActivated                   // a component of activating an ability
	CostCauseTriggered                   // a payment a triggered ability demands (ward, upkeep)
	CostCauseResolution                  // an unless payment made during a resolving ability
)

// CostCauseForAbility is the offer gate's variant (cast.go nonManaCastable):
// castable prices a HYPOTHETICAL cast with no pendingCast, so the caller's
// own ability bit is the provenance.
func CostCauseForAbility(ability bool) CostCause {
	if ability {
		return CostCauseActivated
	}
	return CostCauseSpell
}

// ManaCostPayableFull is the full cost walk of manaCostPayable.
func ManaCostPayableFull(e Engine, p state.PlayerID, o *state.Object, source state.ObjID, cost costvocab.Cost, hyp *state.Mana) bool {
	av := AvailableFor(e, p, DescriptorFor(source, true, cost))
	pool := av.Pool
	typed := av.Typed
	if hyp != nil {
		pool = *hyp
		// A hypothetical bound is a pure mana bound that may include
		// restricted units, so its typed partition is the raw tally (the
		// typed counts never affect payability anyway).
		typed = e.Game().Players[p].ManaUnits()
	}
	if cost.X != 0 || len(cost.Reveal) > 0 || len(cost.RevealOrChoose) > 0 || len(cost.RevealChosen) > 0 || len(cost.Behold) > 0 ||
		len(cost.Blight) > 0 || ActivationTapCostUnavailable(o, &cost) || !CostPayablePool(e, p, source, true, cost, pool, typed) {
		return false
	}
	// A Forage cost is payable when the payer's graveyard holds three cards OR
	// they control a Food; the settle poses the two-arm election (the cast
	// path's nonManaCastable read).
	if cost.Forage && !manaForagePayable(e, p, source) {
		return false
	}
	// untapYType<N/Spec> parts (Benthic Explorers): enough distinct TAPPED
	// matching permanents exist (an untap cost needs a tapped permanent).
	if !ManaUntapPayable(e, p, source, cost) {
		return false
	}
	// The mana-activation path announces its own X for an announced
	// SubCounter<X/...> part and settles it off the continuation; a PayLife<X>
	// part still has no ask here, so the ability is not offered rather than
	// paid for free.
	if len(cost.LifeX) > 0 {
		return false
	}
	if !ManaCostPartsSettleable(cost) {
		return false
	}
	// Announced SubCounter<X/...> parts: the mana path announces X (the cast
	// path's xAsk shape) and settles the removal, so the offer gate prices
	// the announcement instead of refusing it. XMin floors X (Rasputin and
	// Jetfire carry XMin$ 1), so a board that cannot settle at least XMin is
	// unpayable -- the fail-closed direction. A fixed part still needs its
	// counters: source-anchored from the source, anchored from any candidate.
	if bound, any := ManaSubCounterXBound(e, p, o, source, cost); any && bound < cost.XMin {
		return false
	}
	for _, part := range cost.SubCounter {
		if part.Announced {
			continue
		}
		if costvocab.SubCounterTargetsSource(part.Target) {
			if o.Counter(part.Spec) < part.N {
				return false
			}
			continue
		}
		if len(SubCounterRemovalCandidates(e, p, source, part, part.N, nil)) == 0 {
			return false
		}
	}
	// PayEnergy<N> (Aether Hub, Servant of the Conduit): the payer's energy
	// total must cover every fixed part (CR 107.14); the settle spends it
	// through chargeEnergyCost, the cast path's one energy-charging site.
	energy := int32(0)
	for _, part := range cost.Energy {
		energy += part.N
	}
	if energy > e.Game().Players[p].Counter("ENERGY") {
		return false
	}
	// AddCounter<N/KIND> and Exert<1/CARDNAME> are source-anchored: the
	// source must still be the permanent that receives the counter or the
	// exert (a mana ability is activated from the battlefield).
	if (len(cost.AddCounter) > 0 || len(cost.Exert) > 0) && o.Zone != state.ZBattlefield {
		return false
	}
	// Return<N/Spec> parts: the mana path pays only the self-return
	// (Spec CARDNAME, Forge's payCostFromSource -- Grinning Ignus's
	// "{R}, Return this creature to its owner's hand"), which needs no
	// choice. Any other Return spec, or a Return beside a part the
	// discard/sacrifice continuation owns, is refused rather than activated
	// with the return silently unpaid.
	if !ManaReturnCostSupported(cost) {
		return false
	}
	for range cost.Return {
		if o.Zone != state.ZBattlefield {
			return false
		}
	}
	sacs, ok := ManaSacrifices(e, p, source, cost)
	if !ok {
		return false
	}
	discards, ok := ManaDiscards(e, p, source, cost)
	if !ok {
		return false
	}
	exiles, ok := ManaExiles(e, p, source, cost)
	if !ok {
		return false
	}
	// A CollectEvidence<N> part (Cryptex): the graveyard, less the cards the
	// exile parts above reserve, must reach N (the stage elects the cards).
	if !manaEvidencePayable(e, p, cost, exiles) {
		return false
	}
	// tapXType<N/Spec> parts (Springleaf Drum, Heritage Druid): the payer must
	// have enough untapped matching permanents, reserving the source when the
	// same cost also taps it and the sacrifice/discard/exile picks above (one
	// permanent cannot pay two parts of one cost). The candidates here are the
	// same battlefield-order walk the payment election uses, so the count that
	// offered the activation and the objects the payer may tap cannot
	// disagree.
	reserved := make(map[state.ObjID]bool, len(sacs)+len(discards)+len(exiles))
	for _, id := range sacs {
		reserved[id] = true
	}
	for _, id := range discards {
		reserved[id] = true
	}
	for _, id := range exiles {
		reserved[id] = true
	}
	if !ManaTapsPayable(e, p, source, cost, reserved) {
		return false
	}
	return true
}

// ManaSacrifices finds enough candidates for each sacrifice cost part. The
// activation continuation chooses which candidates pay when there is a choice.
// Candidates are still returned in deterministic battlefield order for the
// no-choice path.
func ManaSacrifices(e Engine, p state.PlayerID, source state.ObjID, cost costvocab.Cost) ([]state.ObjID, bool) {
	candidates := make([][]state.ObjID, len(cost.Sac))
	needs := make([]int, len(cost.Sac))
	for i, part := range cost.Sac {
		needs[i] = int(part.N)
		if needs[i] <= 0 {
			return nil, false
		}
		candidates[i] = SacrificeCostCandidates(e, p, source, part, true)
	}
	used := make(map[state.ObjID]bool)
	var chosen []state.ObjID
	var assign func(int, int) bool
	assign = func(part, unit int) bool {
		for part < len(needs) && unit >= needs[part] {
			part++
			unit = 0
		}
		if part == len(needs) {
			return true
		}
		for _, id := range candidates[part] {
			if used[id] {
				continue
			}
			used[id] = true
			chosen = append(chosen, id)
			if assign(part, unit+1) {
				return true
			}
			chosen = chosen[:len(chosen)-1]
			delete(used, id)
		}
		return false
	}
	if !assign(0, 0) {
		return nil, false
	}
	return chosen, true
}

// manaForagePayable is the offer gate's read: Forage is payable when the
// payer's graveyard holds three cards OR a Food they control is on the
// battlefield (the cast path's nonManaCastable read; CR 701.16a).
func manaForagePayable(e Engine, p state.PlayerID, source state.ObjID) bool {
	if len(e.Game().Zone(state.ZGraveyard, p)) >= 3 {
		return true
	}
	return len(CostCandidates(e, p, source, state.ZBattlefield, "Food.YouCtrl", false, false)) > 0
}

// ManaUntapCandidates returns, in deterministic seat/zone order, the TAPPED
// permanents matching spec that can pay one untapYType<N/Spec> part. Unlike
// costCandidates (which walks only the payer's own zone), this walks every
// seat's battlefield because the head's spec can name an opponent's permanent
// (Benthic Explorers' Land.OppCtrl); the spec matcher owns the control
// relation.
func ManaUntapCandidates(e Engine, p state.PlayerID, source state.ObjID, spec string, claimed map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for _, q := range e.Game().Players {
		for _, id := range e.Game().Zone(state.ZBattlefield, q.ID) {
			if claimed != nil && claimed[id] {
				continue
			}
			o := e.Game().Obj(id)
			if o == nil || !o.Tapped {
				continue
			}
			if e.MatchesSpecFrom(spec, id, p, source) {
				out = append(out, id)
			}
		}
	}
	return out
}

// ManaUntapPayable is the offer gate's read for the untapYType parts: enough
// distinct TAPPED matching permanents exist, reserving the source when the
// same cost also taps it.
func ManaUntapPayable(e Engine, p state.PlayerID, source state.ObjID, cost costvocab.Cost) bool {
	if len(cost.UntapPermanent) == 0 {
		return true
	}
	claimed := map[state.ObjID]bool{}
	if cost.Tap {
		claimed[source] = true
	}
	for _, part := range cost.UntapPermanent {
		cands := ManaUntapCandidates(e, p, source, part.Spec, claimed)
		if int32(len(cands)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			claimed[cands[i]] = true
		}
	}
	return true
}

// PaymentPlanProducedExactly reports whether the mana p's pool gained from
// event index from onward is exactly want, with nothing taken out: one
// planned activation's actual production against its witness step. Every
// ManaAdd counter form (plain, snow, typed) is read into its pool slot.
func PaymentPlanProducedExactly(e Engine, p state.PlayerID, from int, want decision.ManaAmount) bool {
	var added state.Mana
	for _, ev := range e.Log().Events[from:] {
		if ev.Kind != events.ManaAdd || ev.Player != p {
			continue
		}
		if ev.Amount < 0 {
			return false
		}
		added[state.ManaSlot(ev.Counter)] += ev.Amount
	}
	return ManaAmount(added) == want
}

// CastWindowSelfSacCost reports whether c is a self-sacrifice activation cost
// ("Sac<1/CARDNAME>") whose batch is deterministic: the only matching
// permanent is the source itself, so the interactive continuation
// (manaDiscardActivation) sacrifices it without a further ask. Any other Sac
// shape (multi-part, overlapping, multiple candidates) is refused.
func CastWindowSelfSacCost(e Engine, p state.PlayerID, source state.ObjID, c costvocab.Cost) bool {
	if len(c.Sac) != 1 || c.Sac[0].N != 1 || !strings.EqualFold(SacrificeMatchSpec(c.Sac[0].Spec), "CARDNAME") {
		return false
	}
	if c.Generic != 0 || c.Life != 0 || c.Colored != (state.Mana{}) || !CastWindowOtherPartsAbsent(c) {
		return false
	}
	n := 0
	for _, id := range e.Game().Zone(state.ZBattlefield, p) {
		if e.CostBlocked(BlockSacrifice, id, CostCauseActivated) {
			continue
		}
		if e.MatchesSpecFrom(c.Sac[0].Spec, id, p, source) {
			n++
		}
	}
	return n == 1
}
