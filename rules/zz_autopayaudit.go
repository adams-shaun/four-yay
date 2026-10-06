//go:build autopayaudit

package rules

// Diagnostic-only helpers for the goal-1 auto-pay bot audit
// (cmd/autopayaudit). This file is compiled ONLY with -tags autopayaudit, so
// the production engine, its tests and every golden are untouched. Every
// helper is a pure read: no event, no state write, no RNG.

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// auditNodeBudget bounds one audit query. A query that exhausts it reports
// Known=false rather than guessing.
const auditNodeBudget = 400000

// AuditMaxUnits skips (Known=false) a pair audit whose board carries more V1
// source units than this; 0 = no cap. Big landfall/token boards make the
// exhaustive walk the harness's own bottleneck.
var AuditMaxUnits = 0

// AuditPair is the hand-pair feasibility verdict for two ordinary hand casts
// A (the one being cast) and B (another card the hand holds).
type AuditPair struct {
	// Known is false when either cost is outside the V1 subset or the node
	// budget ran out; the booleans are then meaningless.
	Known bool
	// Joint: some disjoint assignment of V1 sources (plus the current pool)
	// pays A and then B.
	Joint bool
	// AfterChosen: B is still payable from the pool left after A and the V1
	// sources A's actual payment did not use.
	AfterChosen bool
}

type auditUnit struct {
	id   state.ObjID
	alts []state.Mana
	max  int32
}

// auditUnits is the V1 planner's own source census (paymentPlanManaUnits and
// paymentPlanUnitAlternatives), minus any excluded source.
func (e *Engine) auditUnits(p state.PlayerID, exclude map[state.ObjID]bool) []auditUnit {
	units := e.paymentPlanManaUnits(p)
	out := make([]auditUnit, 0, len(units))
	for _, u := range units {
		if exclude[u.ID] {
			continue
		}
		var au auditUnit
		au.id = u.ID
		for _, a := range pay.PaymentPlanQueryAlternatives(asPayer(e), u) {
			au.alts = append(au.alts, a.Mana)
			if t := a.Mana.Total(); t > au.max {
				au.max = t
			}
		}
		if len(au.alts) > 0 {
			out = append(out, au)
		}
	}
	return out
}

// AuditV1SourceIDs returns the untapped sources the V1 planner can use.
func (e *Engine) AuditV1SourceIDs(p state.PlayerID) map[state.ObjID]bool {
	out := map[state.ObjID]bool{}
	for _, u := range e.auditUnits(p, nil) {
		out[u.id] = true
	}
	return out
}

// auditCost is the composed cost a V1 plan pays for an ordinary hand cast,
// and whether it is inside the V1 cost subset.
func (e *Engine) auditCost(p state.PlayerID, id state.ObjID) (Cost, bool) {
	c := e.offerCostFor(p, id, pay.RawBaseCost(asPayer(e), p, id), spellScope(""))
	return c, pay.PlanCostOK(c)
}

func auditNeed(c Cost) int32 { return c.Generic + c.Colored.Total() }

// auditPayable reports whether c is payable from pool plus some subset of
// units (each at most once, one alternative each). budget is shared.
func (e *Engine) auditPayable(p state.PlayerID, c Cost, pool state.Mana, units []auditUnit, budget *int) (bool, bool) {
	life := e.G.Players[p].Life
	need := auditNeed(c)
	suffix := make([]int32, len(units)+1)
	for i := len(units) - 1; i >= 0; i-- {
		suffix[i] = suffix[i+1] + units[i].max
	}
	exhausted := false
	var walk func(at int, produced state.Mana) bool
	walk = func(at int, produced state.Mana) bool {
		if *budget <= 0 {
			exhausted = true
			return false
		}
		*budget--
		have := pay.ManaAdd(pool, produced)
		if have.Total() >= need {
			if _, ok := resolveManaWith(c, have, state.Mana{}, [7]state.Mana{}, life, false, pipRider{}, nil); ok {
				return true
			}
		}
		if at == len(units) || have.Total()+suffix[at] < need {
			return false
		}
		for _, m := range units[at].alts {
			if walk(at+1, pay.ManaAdd(produced, m)) {
				return true
			}
		}
		return walk(at+1, produced)
	}
	ok := walk(0, state.Mana{})
	return ok, !exhausted || ok
}

// AuditPlanPair judges a planned cast: A is paid by the named chosen sources
// leaving poolAfterA floating; is B still payable, and could some other
// assignment have paid both?
func (e *Engine) AuditPlanPair(p state.PlayerID, a, b state.ObjID, chosen []state.ObjID, poolAfterA state.Mana) AuditPair {
	ca, okA := e.auditCost(p, a)
	cb, okB := e.auditCost(p, b)
	if !okA || !okB {
		return AuditPair{}
	}
	if AuditMaxUnits > 0 && len(e.paymentPlanManaUnits(p)) > AuditMaxUnits {
		return AuditPair{}
	}
	budget := auditNodeBudget
	ex := make(map[state.ObjID]bool, len(chosen))
	for _, id := range chosen {
		ex[id] = true
	}
	after, known1 := e.auditPayable(p, cb, poolAfterA, e.auditUnits(p, ex), &budget)
	joint, known2 := e.auditJoint(p, ca, cb, e.G.Players[p].Pool, &budget)
	return AuditPair{Known: known1 && known2, Joint: joint, AfterChosen: after}
}

// AuditJointNow reports whether the current pool plus V1 sources can pay A
// then B disjointly (used on a snapshot taken before the manual bot tapped).
func (e *Engine) AuditJointNow(p state.PlayerID, a, b state.ObjID) (joint, known bool) {
	if AuditMaxUnits > 0 && len(e.paymentPlanManaUnits(p)) > AuditMaxUnits {
		return false, false
	}
	ca, okA := e.auditCost(p, a)
	cb, okB := e.auditCost(p, b)
	if !okA || !okB {
		return false, false
	}
	budget := auditNodeBudget
	return e.auditJoint(p, ca, cb, e.G.Players[p].Pool, &budget)
}

// AuditAfterPoolCast reports whether B is payable after A is paid out of the
// CURRENT floating pool (the manual bot's legacy cast) from the leftover pool
// plus the still-untapped V1 sources.
func (e *Engine) AuditAfterPoolCast(p state.PlayerID, a, b state.ObjID) (after, known bool) {
	ca, okA := e.auditCost(p, a)
	cb, okB := e.auditCost(p, b)
	if !okA || !okB {
		return false, false
	}
	pay, ok := resolveManaWith(ca, e.G.Players[p].Pool, state.Mana{}, [7]state.Mana{}, e.G.Players[p].Life, false, pipRider{}, nil)
	if !ok {
		return false, false
	}
	budget := auditNodeBudget
	return e.auditPayable(p, cb, pay.Pool, e.auditUnits(p, nil), &budget)
}

// auditJoint asks whether A and B, cast in the same step, can both be paid:
// mana from any source may go to either spell and surplus floats until the
// step ends, so this is exactly "is ca+cb payable from the pool plus some
// subset of V1 sources" (V1 costs are generic+coloured only, so Plus is
// exact).
func (e *Engine) auditJoint(p state.PlayerID, ca, cb Cost, pool state.Mana, budget *int) (bool, bool) {
	return e.auditPayable(p, ca.Plus(cb), pool, e.auditUnits(p, nil), budget)
}

// AuditManaAbilityAPIs returns the API of every ability the priority offer
// walk's "activate" option for id would resolve through (the exact
// appendAvailableManaAbilities call rules/legal.go makes), with "intrinsic"
// for a basic land's intrinsic ability, and the parsed Cost$ of each. It lets
// the audit check that every "activate" option the auto-pay adapter drops is
// a CR 605 mana ability, and whether it costs more than a tap.
func (e *Engine) AuditManaAbilityAPIs(p state.PlayerID, id state.ObjID) (apis []string, costs []string) {
	statics := actionStaticSource{e: e}
	for _, ma := range e.appendAvailableManaAbilities(nil, &statics, p, id) {
		api := ma.API
		if len(ma.Line) >= 10 && ma.Line[:10] == "intrinsic:" {
			api = "intrinsic"
		}
		apis = append(apis, api)
		costs = append(costs, ma.ParamStr(cards.PKCost))
	}
	return apis, costs
}
