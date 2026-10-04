package rules

import (
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The potential walk, shared across one posed priority decision.
//
// Three pure readers of a posed priority decision each ran the SAME
// PotentialMana fixpoint and the SAME legal-offer walk priced against it, for
// the same seat at the same board:
//
//   - EnsurePaymentActions' offer builder (paymentActionsForPriority: the
//     PotentialMana candidate walk, casts only);
//   - the seat's view projection (PotentialActions: the full walk);
//   - the builtin seat's planner (PotentialPaymentPlans: the full walk, run
//     with paymentPlanPotentialPool set).
//
// Measured on the 4096-game random SpellBench leg (tip12): those three walks
// plus their PotentialMana fixpoints were ~26% of all CPU, each computed from
// scratch. potentialWalk computes the pair once per decision and serves the
// rest.
//
// Why the answer is exact. Every input of PotentialMana and of the walk is
// either event-backed state (e.G, moved only by events.Apply, so
// len(e.L.Events) keys it), the continuous registry (continuousVersion), the
// object arena (len(e.G.Objs)), or a non-event engine runtime field. The
// entry is keyed on all three counters AND on the posed decision itself: the
// pending pointer plus potentialAskSerial, a counter bumped by every ask and
// by every Submit once its validation passes (validation is a pure read: a
// rejected intent leaves the decision posed and the engine untouched).
// Between an ask and the next committing Submit the engine is at rest --
// the readers above are documented pure reads (no event, no state write),
// and every non-event runtime input the walk contract lists (rename/type
// tables, observer bindings, goad probes, the derivation depth guards, the
// face probe) is at rest outside a derivation, which potentialWalkUsable
// checks. paymentPlanPotentialPool, the one flag the PotentialPaymentPlans
// reader sets around its walk, is read only by the cast planner
// (paymentPlanPoolAccepted), never by PotentialMana or the offer walk. A
// direct test write to e.G between an ask and a read is the one input the
// key cannot see exactly. The entry also records the seat's own pools and
// turn bookkeeping (potentialPlayerStamp) and active()'s rebuild count
// (potentialRebuilds), so the usual probes -- floating mana into Pool,
// writing e.G and then retiring the layer caches (activeEpoch = -1) -- miss;
// and verify mode (walkCacheVerify, on in the rules test binary) recomputes
// every hit and panics on any difference.
//
// The offer builder's cast plans for the decision are memoised alongside
// (planCastPaymentMemo), so PotentialPaymentPlans and ValidateCastPayment
// (under Submit's validation) re-plan nothing the builder already planned.
//
// Why serving the FULL walk to the casts-only reader is exact: the builder
// reads only Kind "cast" options and never their Index, and the casts-only
// walk's cast options are exactly the full walk's, in the same order with
// the same fields (castsOnlyWalkVerify). The builder runs the full walk
// instead of the casts-only one only once a full-walk reader has asked on
// this engine (potentialFullDemand), so a host that never projects
// PotentialActions keeps the cheaper walk.
//
// Ownership: the cached Options are a fresh slice the walk returned
// (forAsk false: never the decision arena), read-only to every reader --
// each ranges it by value and none retains it past its call.
type potentialWalkCache struct {
	stamp potentialStamp
	full  bool
	mana  state.Mana
	opts  []decision.Option
	// plans memoises the cast planner's verdicts at this entry's state
	// (planCastPaymentMemo); it lives and dies with the entry.
	plans []castPlanMemo
	// spare is the cache's own walk-result buffer: the next computed walk
	// is built into it (the walk's temp mode), so a replaced entry's array is
	// reused instead of reallocated. opts aliases it when the entry's walk
	// was computed here (never when it is the decision's own Options).
	spare []decision.Option
}

// potentialStamp is a cache entry's validity key: the posed decision (the
// ask serial and the pending pointer), the board counters, active()'s
// rebuild count, and the seat with its own pool stamp.
type potentialStamp struct {
	serial  uint64
	pending *decision.Decision
	ep      int
	ver     int
	objs    int
	seq     uint64
	p       state.PlayerID
	pl      potentialPlayerStamp
}

// potentialStampNow is p's key at the current state (pending non-nil and
// activeDepth 0: potentialWalkUsable).
func (e *Engine) potentialStampNow(p state.PlayerID) potentialStamp {
	return potentialStamp{serial: e.potentialAskSerial, pending: e.pending,
		ep: len(e.L.Events), ver: e.continuousVersion, objs: len(e.G.Objs),
		seq: e.potentialRebuilds(), p: p, pl: e.potentialPlayerStampOf(p)}
}

// castPlanMemo is one memoised planCastPaymentChecked verdict.
type castPlanMemo struct {
	cast decision.PlannedCast
	out  PaymentPlanOutcome
}

// potentialWalkUsable reports whether a potential walk may be served from,
// or stored into, the cache: a top-level read at a posed priority decision,
// outside any derivation, active() build, probe or nested potential walk.
func (e *Engine) potentialWalkUsable() bool {
	return e.pending != nil && e.pending.Kind == decision.KPriority && e.potentialWalkDepth == 0 &&
		e.derivedMemoUsable()
}

// potentialWalkHit reports whether the cache holds p's walk at the current
// state (full: the full walk is required).
func (e *Engine) potentialWalkHit(p state.PlayerID, full bool) bool {
	c := &e.potentialWalk
	return c.opts != nil && (c.full || !full) && c.stamp.p == p && c.stamp == e.potentialStampNow(p)
}

// potentialPlayerStamp is the seat's own mana and turn bookkeeping plus the
// turn position: the fields a probe most often writes directly between an
// ask and a read (a test floating mana into Pool). Engine flow moves each of
// them only through an event, which the log length already keys; the stamp
// makes such a direct write miss instead of tripping verify mode.
type potentialPlayerStamp struct {
	pool, snow, persistent, combat state.Mana
	typed                          [4]state.Mana
	artifact                       [3]state.Mana
	restricted                     int
	life, lands                    int32
	active                         state.PlayerID
	step                           state.Step
	stack                          int
}

func (e *Engine) potentialPlayerStampOf(p state.PlayerID) potentialPlayerStamp {
	pl := &e.G.Players[p]
	return potentialPlayerStamp{
		pool: pl.Pool, snow: pl.Snow, persistent: pl.PersistentMana, combat: pl.CombatMana,
		typed: pl.TypedMana, artifact: pl.ArtifactTyped, restricted: len(pl.RestrictedMana),
		life: pl.Life, lands: pl.LandsPlayed, active: e.G.Active, step: e.G.Step, stack: len(e.G.Stack),
	}
}

// potentialWalkOf returns PotentialMana(p) and the legal-offer walk priced
// against it. full false asks only for the walk's "cast" options (the
// casts-only walk; the full walk may be returned instead, see above); full
// true asks for the whole walk. The returned Options are read-only.
func (e *Engine) potentialWalkOf(p state.PlayerID, full bool) (state.Mana, []decision.Option) {
	if !e.potentialWalkUsable() {
		return e.potentialWalkCompute(p, full)
	}
	if full {
		e.potentialFullDemand = true
	}
	if e.potentialWalkHit(p, full) {
		c := &e.potentialWalk
		if walkCacheVerify {
			e.verifyPotentialWalk(p, full, c.mana, c.opts)
		}
		return c.mana, c.opts
	}
	walkFull := full || e.potentialFullDemand
	var mana state.Mana
	var opts []decision.Option
	servedTail := false
	if tail := e.priorityWalkTailFor(p); tail != nil {
		// The decision's own offer walk is the potential walk whenever
		// PotentialMana adds nothing to the floating pool (see
		// priorityWalkTail): only the bound needs computing.
		e.potentialWalkDepth++
		if e.priorityWalk.blocks {
			// The recorded priority walk's own-battlefield membership
			// lists serve PotentialMana's (walk_block_reuse.go).
			e.potentialManaRec = &e.walkRec
		}
		mana = e.PotentialMana(p)
		e.potentialManaRec = nil
		e.potentialWalkDepth--
		if mana == e.G.Players[p].Pool {
			opts, walkFull, servedTail = tail, true, true
			if walkCacheVerify {
				e.potentialWalkDepth++
				want := e.legalActionsWalk(p, &mana, false)
				e.potentialWalkDepth--
				if !reflect.DeepEqual(want, opts) {
					panic(fmt.Sprintf("rules: priority walk served as the potential walk %+v, the priced walk is %+v", opts, want))
				}
			}
		} else {
			// The full walk serves the recorded priority walk's
			// pool-independent blocks (walk_block_reuse.go).
			reuse := walkFull && e.priorityWalk.blocks
			if reuse {
				e.walkReuse = &e.walkRec
			}
			e.potentialWalkDepth++
			opts = e.legalActionsWalkWithWindow(p, &mana, !walkFull, nil, false, e.potentialWalk.spare, true)
			e.potentialWalkDepth--
			e.walkReuse = nil
			if reuse && walkCacheVerify {
				e.potentialWalkDepth++
				want := e.legalActionsWalk(p, &mana, false)
				e.potentialWalkDepth--
				if !reflect.DeepEqual(want, opts) {
					panic(fmt.Sprintf("rules: potential walk with reused blocks %+v, the walk is %+v", opts, want))
				}
			}
		}
	} else {
		// The replaced entry's own array is reused for the new walk: no
		// reader holds it (each reader ranges its opts only within its own
		// call, and no reader runs inside another on one engine).
		mana, opts = e.potentialWalkComputeInto(p, walkFull, e.potentialWalk.spare)
	}
	spare := e.potentialWalk.spare
	if !servedTail {
		// The walk returned in spare when it fit, else in a fresh array
		// that now becomes the cache's own.
		spare = opts[:0]
	}
	plans := e.potentialWalk.plans
	clear(plans)
	e.potentialWalk = potentialWalkCache{
		plans: plans[:0], stamp: e.potentialStampNow(p), full: walkFull, mana: mana, opts: opts, spare: spare,
	}
	if walkFull && !full && castsOnlyWalkVerify {
		e.potentialWalkDepth++
		verifyCastsOnlyWalk(e.legalActionsWalk(p, &mana, true), opts)
		e.potentialWalkDepth--
	}
	return mana, opts
}

// potentialRebuilds brings active() up to date and returns its rebuild count:
// activeBuildSeq less the explicit retirements (retireCrossWalkMemo), which
// mark a reader's own transient no-event probes (a face flip, the
// cost-composition exclusion) rather than any change of the board. Only read
// at activeDepth 0 (potentialWalkUsable).
func (e *Engine) potentialRebuilds() uint64 {
	e.active()
	return e.activeBuildSeq - e.crossWalkRetires
}

func (e *Engine) potentialWalkCompute(p state.PlayerID, full bool) (state.Mana, []decision.Option) {
	return e.potentialWalkComputeInto(p, full, nil)
}

// potentialWalkComputeInto is potentialWalkCompute returning the walk's
// options built into dst (legalActionsWalkWithWindow's temp mode); nil dst
// returns a fresh exactly-sized list.
func (e *Engine) potentialWalkComputeInto(p state.PlayerID, full bool, dst []decision.Option) (state.Mana, []decision.Option) {
	e.potentialWalkDepth++
	defer func() { e.potentialWalkDepth-- }()
	mana := e.PotentialMana(p)
	if dst == nil {
		return mana, e.legalActionsWalk(p, &mana, !full)
	}
	return mana, e.legalActionsWalkWithWindow(p, &mana, !full, nil, false, dst, true)
}

func (e *Engine) verifyPotentialWalk(p state.PlayerID, full bool, mana state.Mana, opts []decision.Option) {
	wantMana, want := e.potentialWalkCompute(p, true)
	if wantMana != mana {
		panic(fmt.Sprintf("rules: potential walk cache served PotentialMana %v, recomputed %v", mana, wantMana))
	}
	if e.potentialWalk.full {
		if !reflect.DeepEqual(opts, want) {
			panic(fmt.Sprintf("rules: potential walk cache served %+v, recomputed %+v", opts, want))
		}
		return
	}
	verifyCastsOnlyWalk(opts, want)
}

// planCastPaymentAtDecision is PlanCastPayment for a cast at the posed
// priority decision (ValidateCastPayment, under Submit's validation, still a
// pure read). When the decision's shared PotentialMana walk is still exact
// and lists cast's object as a plain cast, the candidate set is that walk's
// priced set -- exactly the offer builder's (paymentCastCandidates' priced
// argument) -- so the planner's per-cast huge-pool walk is skipped;
// pricedCandidatesVerify runs it and panics on a miss. Otherwise it is
// PlanCastPayment itself.
func (e *Engine) planCastPaymentAtDecision(p state.PlayerID, cast decision.PlannedCast) PaymentPlanOutcome {
	if e.potentialWalkUsable() && e.potentialWalkHit(p, false) {
		for _, o := range e.potentialWalk.opts {
			if o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 && o.Obj == cast.Object {
				statics := costStaticSource{e: e}
				candidates := paymentCastCandidates{e: e, p: p, priced: true}
				return e.planCastPaymentMemo(p, cast, &statics, &candidates)
			}
		}
	}
	return e.PlanCastPayment(p, cast)
}

// planCastPaymentMemo is planCastPaymentChecked, memoised at the posed
// decision's state alongside its potential walk. The offer builder plans
// every plain cast the PotentialMana walk lists; PotentialPaymentPlans plans
// the same casts again for the seat, and ValidateCastPayment re-plans the
// submitted one -- each at the same state, so each answer is the builder's.
//
// Exact because planCastPaymentChecked is a pure read whose inputs are the
// board (keyed by the walk entry), p, the cast, and three engine fields:
// paymentPlanPotentialPool (read only through paymentPlanPoolAccepted, which
// answers the same either way while the pool is plain -- the memo is only
// used then), the relaxed-proof alternatives (paymentPlanRelaxed/Fee: the
// memo is bypassed while either is set), and the per-call statics and
// candidates, whose answers are fixed by the board (the candidate set's
// priced membership equals the huge-pool walk's: pricedCandidatesVerify).
// The served outcome's Plan is shared and read-only: every caller copies
// the struct, clones the plan, or only reads it. Verify mode
// (walkCacheVerify) re-plans every hit with fresh inputs and panics on any
// difference.
func (e *Engine) planCastPaymentMemo(p state.PlayerID, cast decision.PlannedCast, statics *costStaticSource, candidates *paymentCastCandidates) PaymentPlanOutcome {
	if e.PaymentPlanRelaxed != nil || e.PaymentPlanRelaxedFee != 0 || !pay.PlanPoolOK(&e.G.Players[p]) ||
		!e.potentialWalkUsable() || !e.potentialWalkHit(p, false) {
		return e.planCastPaymentChecked(p, cast, statics, candidates)
	}
	c := &e.potentialWalk
	for i := range c.plans {
		if c.plans[i].cast == cast {
			if walkCacheVerify {
				fs, fc := costStaticSource{e: e}, paymentCastCandidates{e: e, p: p}
				if want := e.planCastPaymentChecked(p, cast, &fs, &fc); !reflect.DeepEqual(want, c.plans[i].out) {
					panic(fmt.Sprintf("rules: cast plan memo for %+v served %+v, re-planned %+v", cast, c.plans[i].out, want))
				}
			}
			return c.plans[i].out
		}
	}
	out := e.planCastPaymentChecked(p, cast, statics, candidates)
	if e.potentialWalkHit(p, false) {
		c.plans = append(c.plans, castPlanMemo{cast: cast, out: out})
	}
	return out
}

// priorityWalkTail is the posed priority decision's own offer walk
// (askPriority's legalActionsWithWindow, priced against the floating pool),
// recorded right after its ask so the potential readers can use it.
//
// When PotentialMana(p) equals p's floating pool (no untapped source adds
// anything: measured 60% of the potential walks on the random SpellBench
// leg), the potential walk priced against it IS that walk: every place the
// walk reads its hypothetical bound is a mana pricing whose nil-pool form
// prices exactly (Pool, ManaUnits) while p holds no RestrictedMana --
// manaFeasiblePricedP's two arms, castable/castablePriced and
// costPayable/costPayablePool (the same resolveManaWith; the pip-free fast
// path is verify-checked against it), and specializeLegalPriced's
// costPayableOther/costPayablePool. The one structural difference,
// morphTurnUpPayablePriced's smallest-X pricing against the nil form's X
// loop, only runs for a face-down permanent of p's, so the tail is not used
// while p controls one. Verify mode (walkCacheVerify) runs the priced walk
// on every use and panics unless the two option lists are identical.
//
// The tail is recorded only when nothing but ask's own DecisionAsk marker
// (whose Apply writes nothing) was logged since the walk, and the registry
// and arena did not move, so the walk read the state the stamp names.
type priorityWalkTail struct {
	stamp potentialStamp
	opts  []decision.Option
	// blocks: the walk recorded its pool-independent blocks
	// (walk_block_reuse.go, e.walkRec).
	blocks bool
}

// notePriorityWalk records askPriority's walk for p (taken at log length ep,
// registry version ver and arena size objs) once its ask posed d.
func (e *Engine) notePriorityWalk(p state.PlayerID, d *decision.Decision, ep, ver, objs int) {
	e.priorityWalk = priorityWalkTail{}
	// A block record is this walk's only if the walk just completed one
	// (walk_block_reuse.go); it is promoted with the tail or dropped.
	r := &e.walkRec
	recorded := r.owner == e && r.done && r.p == p
	r.done = false
	if e.pending != d || ver != e.continuousVersion || objs != len(e.G.Objs) || ep > len(e.L.Events) ||
		!e.potentialWalkUsable() {
		return
	}
	for _, ev := range e.L.Events[ep:] {
		if ev.Kind != events.DecisionAsk {
			return
		}
	}
	e.priorityWalk = priorityWalkTail{stamp: e.potentialStampNow(p), opts: d.Options, blocks: recorded}
}

// priorityWalkTailFor returns the recorded priority walk's options when they
// are p's walk at the current state and stand for the potential walk's
// pricing (no RestrictedMana, no face-down permanent of p's); the caller
// still checks the PotentialMana bound against the pool.
func (e *Engine) priorityWalkTailFor(p state.PlayerID) []decision.Option {
	t := &e.priorityWalk
	if t.opts == nil || t.stamp.p != p || len(e.G.Players[p].RestrictedMana) != 0 || t.stamp != e.potentialStampNow(p) {
		return nil
	}
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.FaceDown {
			return nil
		}
	}
	return t.opts
}
