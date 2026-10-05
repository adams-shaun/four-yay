package rules

import (
	"sort"
	"strconv"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file owns the CollectEvidence payment's shared, engine-free mechanics
// and the triggered-cost window's evidence stage. The cast/activation flow
// (rules/cast_targets.go's evidenceAsk) and the trigger-body Cost$ window
// (rules/cumulative.go's triggeredCostComponentsPayable / advanceTriggeredMandatory
// / settleTriggeredMandatory) both pay CR 701.30b's "collect evidence N"
// action, so the candidate gather, the deterministic order (mana value
// DESCENDING, ties by object id), the greedy minimum card count and the
// total read live here ONCE. The evidence stage itself is a method on
// triggeredEffectCost, not a new *Engine method, so internal/codeshape's
// engineMethodCount / engineSurface ceilings stay untouched.

// evidenceGraveCandidates returns the payer's own graveyard cards in zone
// order, excluding any id in reserved (a shared reservation set so one card
// cannot pay two cost parts), skipping objects with no printed face. The
// caller orders and counts.
func evidenceGraveCandidates(g *state.Game, player state.PlayerID, reserved map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range g.Zone(state.ZGraveyard, player) {
		if reserved[id] {
			continue
		}
		if o := g.Obj(id); o != nil && o.Face() != nil {
			out = append(out, id)
		}
	}
	return out
}

// evidenceOrder copies ids and orders them by mana value DESCENDING, ties by
// object id ascending. The order is the option order the ask poses: the
// FIRST minimum-count options then always reach the threshold, so the
// deterministic bot's generic KChoose arm and Clamp's top-up both settle a
// legal evidence payment on the first answer.
func evidenceOrder(g *state.Game, ids []state.ObjID) []state.ObjID {
	ordered := append([]state.ObjID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool {
		mi, mj := g.Obj(ordered[i]).Face().Cmc(), g.Obj(ordered[j]).Face().Cmc()
		if mi != mj {
			return mi > mj
		}
		return ordered[i] < ordered[j]
	})
	return ordered
}

// evidenceManaValue sums the mana values of the named cards the payer owns
// (the Ward evidence payment's wardManaValue read, over a caller-built list).
func evidenceManaValue(g *state.Game, p state.PlayerID, ids []state.ObjID) int32 {
	var n int32
	for _, id := range ids {
		if o := g.Obj(id); o != nil && o.Face() != nil && o.Owner == p {
			n = addClampedGeneric(n, int64(o.Face().Cmc()))
		}
	}
	return n
}

// evidenceGreedyMin returns the smallest prefix length of the ORDERED ids
// whose total reaches need -- the ask's Min, and therefore the count the
// deterministic bot answers with. need <= 0 yields 0; a list that never
// reaches need yields len(ids) (callers check reachability first).
func evidenceGreedyMin(g *state.Game, ids []state.ObjID, need int32) int {
	if need <= 0 {
		return 0
	}
	remaining := need
	for i, id := range ids {
		if remaining <= 0 {
			return i
		}
		remaining -= g.Obj(id).Face().Cmc()
	}
	return len(ids)
}

// evidenceNeed resolves the window's collect-evidence threshold from the
// cost's Evidence parts. A literal part contributes its N; a dynamic part
// (Dyn != "", e.g. Incinerator of the Guilty's CollectEvidence<X>) is NOT
// resolvable in the trigger window -- there is no cast-style chosen-target
// union and no announced X here -- so it returns -1, the loud "unresolvable"
// sentinel. The caller must DECLINE on -1, never free-settle.
func (tc *triggeredEffectCost) evidenceNeed() int32 {
	need := int32(0)
	for _, part := range tc.amount.Evidence {
		if part.Dyn != "" || part.Announced {
			return -1
		}
		need = addClampedGeneric(need, int64(part.N))
	}
	return need
}

// reservedComponents is the set of objects this window's already-settled
// component picks have reserved, so the evidence stage cannot reuse one
// (Lamplight Phoenix's `ExileAnyGrave<1/...> CollectEvidence<4>` must exile
// the triggering card AND then collect evidence from disjoint cards).
func (tc *triggeredEffectCost) reservedComponents() map[state.ObjID]bool {
	if len(tc.sacs)+len(tc.exiles)+len(tc.discards)+len(tc.moveGraves) == 0 {
		return nil
	}
	m := make(map[state.ObjID]bool, len(tc.sacs)+len(tc.exiles)+len(tc.discards)+len(tc.moveGraves))
	for _, id := range tc.sacs {
		m[id] = true
	}
	for _, id := range tc.exiles {
		m[id] = true
	}
	for _, id := range tc.discards {
		m[id] = true
	}
	for _, id := range tc.moveGraves {
		m[id] = true
	}
	return m
}

// evidenceStage advances the window past its component parts into the
// collect-evidence stage: it resolves the threshold, gathers the payer's
// graveyard (minus every component reservation), records the whole list when
// no choice exists, otherwise poses the mana-value-descending KChoose the
// cast flow poses. A dynamic/unresolvable threshold emits a loud Note and
// declines -- it must never free-settle. After a settled evidence stage it
// resumes the parked body (settleTriggeredMandatory).
func (tc *triggeredEffectCost) evidenceStage(e *Engine) {
	if tc.evidenceSettled {
		e.settleTriggeredMandatory(tc)
		return
	}
	need := tc.evidenceNeed()
	if need < 0 {
		e.emit(events.Event{Kind: events.Note, Player: tc.player,
			Text: "collect evidence amount is dynamic and cannot be resolved in a triggered cost; declining"})
		e.triggeredCostDecline(tc)
		return
	}
	ids := evidenceOrder(e.G, evidenceGraveCandidates(e.G, tc.player, tc.reservedComponents()))
	if need <= 0 || evidenceManaValue(e.G, tc.player, ids) < need {
		// The graveyard cannot reach the threshold (or the cost owes
		// nothing): the body never runs, and nothing has moved.
		e.triggeredCostDecline(tc)
		return
	}
	min := evidenceGreedyMin(e.G, ids, need)
	if min == len(ids) {
		// Every candidate is needed to reach the threshold: no choice
		// exists, so no decision is posed (the strict-supersets
		// convention). The whole list is the payment.
		tc.evidence = append([]state.ObjID(nil), ids...)
		tc.evidenceSettled = true
		e.settleTriggeredMandatory(tc)
		return
	}
	name := "triggered ability"
	if o := e.G.Obj(tc.source); o != nil && o.Face() != nil {
		name = o.Face().Name
	}
	d := &decision.Decision{Player: tc.player, Kind: decision.KChoose, Min: min, Max: len(ids),
		Prompt: "Exile evidence with total mana value " + strconv.Itoa(int(need)) + " for " + name,
		Source: tc.source}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "evidence",
			Obj: id, Label: e.targetName(id), Value: int(e.G.Obj(id).Face().Cmc())})
	}
	windowAsk(e, d, chooseTriggeredMandatory)
}

// answerEvidenceStage validates one answered evidence selection against the
// current board and either settles it or re-poses the ask (the cast flow's
// evidenceAsk settle-validate shape, shared through the same free helpers).
// A selection whose total falls short, or that names an unavailable card, is
// rejected and re-posed; a dynamic threshold neither the ask nor the answer
// can resolve is a loud decline.
func (tc *triggeredEffectCost) answerEvidenceStage(e *Engine, chosen []decision.Option) {
	need := tc.evidenceNeed()
	if need < 0 {
		e.emit(events.Event{Kind: events.Note, Player: tc.player,
			Text: "collect evidence amount is dynamic and cannot be resolved in a triggered cost; declining"})
		e.triggeredCostDecline(tc)
		return
	}
	ids := make([]state.ObjID, 0, len(chosen))
	for _, o := range chosen {
		if o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	eligible := evidenceGraveCandidates(e.G, tc.player, tc.reservedComponents())
	allowed := make(map[state.ObjID]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	reason := ""
	switch {
	case len(ids) == 0:
		reason = "no cards chosen as evidence; choose again"
	case !evidenceAllAllowed(ids, allowed):
		reason = "evidence selection includes an unavailable card; choose again"
	case evidenceManaValue(e.G, tc.player, ids) < need:
		reason = "evidence selection's total mana value is too low; choose again"
	}
	if reason != "" {
		e.emit(events.Event{Kind: events.Note, Player: tc.player, Text: reason})
		tc.evidenceStage(e)
		return
	}
	tc.evidence = ids
	tc.evidenceSettled = true
	e.advanceTriggeredMandatory(tc)
}

// evidenceAllAllowed reports whether every id is in the allowed set.
func evidenceAllAllowed(ids []state.ObjID, allowed map[state.ObjID]bool) bool {
	for _, id := range ids {
		if !allowed[id] {
			return false
		}
	}
	return true
}
