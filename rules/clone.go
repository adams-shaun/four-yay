package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Clone deep-copies the engine: game, log, RNG position, the pending
// decision, continuous effects, the pending-trigger queue and the trigger
// bookkeeping maps. The copy and the original then evolve independently —
// the same intents submitted to both produce the same events, chain head
// and RNG draw count (clone_test.go pins that), and nothing submitted to
// one is visible to the other. Card data (*cards.Card, *cards.SA) is
// shared: the compiled corpus is immutable once loaded.
//
// Call it only at an intent boundary — after New, Advance or Submit has
// returned, so Pending() != nil or G.Over. That is the only moment the
// fields below are not being written. A match host clones at every turn
// start to answer "view at seq N" with at most one turn of replay.
//
// The harness observers are deliberately NOT copied: ManaAbilityHook and the
// auto-pay diagnostics sink (paymentStats, SetPaymentPlanStats) stay nil on
// the copy, so no sink pointer is ever shared between engines (payment-plan
// spec §7) and a clone's planning never counts into the original's sink.
//go:generate env GORGE_GEN_CLONE=1 go test . -run TestCloneGenIsUpToDate -count=1

func (e *Engine) Clone() *Engine { return e.cloneWith(Spare{}) }

// CloneInto is Clone drawing the copy's event log, intent, object-arena and
// Derived-memo arrays from *sp -- a spent clone's storage, handed back by its
// Release -- and consuming it (*sp is left as the zero Spare), so a search
// that clones one root per simulation recycles the same few arrays instead of
// allocating and collecting a mid-game log and arena every simulation. The
// loop is: c := root.CloneInto(&sp); play c; sp = c.Release(). The copy is
// identical to Clone's in everything a caller can observe (same events,
// intents, objects, chain, RNG; TestCloneIntoIsInvisible pins it): the
// recycled arrays were cleared by Release, every slot is overwritten before
// it is read, and an array too small for this root is dropped and the clone
// allocates as Clone would. The zero Spare makes CloneInto exactly Clone.
func (e *Engine) CloneInto(sp *Spare) *Engine {
	var spare Spare
	if sp != nil {
		spare, *sp = *sp, Spare{}
	}
	return e.cloneWith(spare)
}

// rekeyVersion maps a cache's continuousVersion stamp onto a clone, whose
// continuousVersion starts at zero: a cache built under the parent's current
// registry is current in the clone too (same registry, same board), and any
// other stamp must never match the clone's.
func rekeyVersion(stamp, current int) int {
	if stamp == current {
		return 0
	}
	return -1
}

// cloneCarriesStaticMemo reports whether a clone carries the staticEffects
// memo (layercache.go): it is built, and current under the registry or
// provably independent of it (staticMemoQuiet). The clone's board is
// identical at the clone boundary, so the memo is current there too.
func cloneCarriesStaticMemo(e *Engine) bool {
	return e.staticEpoch > 0 && (e.staticVersion == e.continuousVersion || e.staticMemoQuiet())
}

// cloneCarriesAtkOffers reports whether a clone carries attackOffers' memo
// (attack_cost.go): a search clones the engine while its declare-attackers
// decision is pending, and the clone's validateAttackers then reuses the list
// askAttackers derived instead of re-deriving it per simulation. The list is
// shared, never written (a recompute stores a fresh slice).
func cloneCarriesAtkOffers(e *Engine) bool {
	return e.atkOffersEp > 0 && e.atkOffersVer == e.continuousVersion
}

// cloneCarriesCast reports whether a cast is suspended mid-flow: only then
// does a clone carry the held-back cast trigger (CR 601.2i, cast.go), so
// that re-Submitting the target answer still fires it in the clone. The
// event is a value; the LKI is a read-only snapshot, shared.
func cloneCarriesCast(e *Engine) bool { return e.cast != nil }

// cloneWith builds the copy. The generated cloneEngineFields (clone_gen.go,
// from the fields' clone tags) copies every field but the few below, whose
// copy recycles bespoke Spare storage or re-records a key.
func (e *Engine) cloneWith(sp Spare) *Engine {
	c := &Engine{
		G:   e.G.CloneIntoDirty(sp.objs, sp.objDirty),
		L:   e.L.CloneIntoFrom(sp.events, sp.evFrom, sp.evN, sp.evDirty, sp.intents),
		rng: e.rng.clone(),
		// The livelock watcher (rules/livelock.go): carry the Config-given
		// guard thresholds, reset the observation state. A clone only happens
		// at an intent boundary -- the only moment these fields are not being
		// written -- where the watcher holds no in-flight run or quiet count
		// worth carrying, so a fresh watcher over the same thresholds is a
		// faithful copy.
		loop: newLivelockWatcherFromGuard(e.loop.guard, sp.loopSigs, sp.loopRecent, sp.loopPrev, sp.loopHeads, sp.loopHash),
	}
	// The identity-preserving copy of the suspended resolution's memories,
	// transactions and frames (clone_remap.go): stack-resident, allocating
	// only when there is something to copy.
	var remap cloneRemap
	cloneEngineFields(c, e, &sp, &remap)
	// The offer walk's object classes (walk_objclass.go) are exact as of
	// the static catch-up's watermark; the clone's log is a copy of this
	// one, so it carries both and catches up the rest itself.
	if e.walkClsOwner == e && len(e.walkObjCls) != 0 {
		c.walkObjCls, c.walkClsOwner = append(sp.walkCls[:0], e.walkObjCls...), c
		c.staticZonesEp = e.staticZonesEp
	}
	// The resolution kernel's checkpoint storage: a spent engine's dropped
	// checkpoint is recycled.
	if sp.tapeCkpt != nil {
		c.tapeSpare = *sp.tapeCkpt
	}
	if len(e.pendingTriggers) > 0 {
		c.pendingTriggers = clonePendingTriggers(e.pendingTriggers)
	} else if e.pendingTriggers != nil || sp.pending != nil {
		// An empty queue (a drained one keeps its array, putTriggersOnStack)
		// takes the recycled array instead of allocating its first batch.
		c.pendingTriggers = sp.pending
	}
	// The spent engine's recycled snapshot arenas (trigger_snapshot_pool.go):
	// cleared, owned by nobody else, so the clone's look-back windows reuse
	// them; the original's own pool is never shared.
	c.adoptSnapshotObjs(sp.snapObjs)
	// The spent engine's cleared decision-arena chunks, switched off: the
	// clone's owner turns the arena on (SetDecisionArena) if its decisions
	// die with it.
	c.adoptArena(sp.arena)
	c.adoptHypPool(sp.hyp)
	if sp.lookBack != nil {
		c.lookBack, c.lookBackOwner = sp.lookBack, c
	}
	if sp.preview != nil {
		c.preview, c.previewOwner = sp.preview, c
	}
	// The SBA quiet key (sbaquiet.go), when the original is provably quiet
	// without a layer read: see sbaQuietCarry.
	if k, ok := e.sbaQuietCarry(); ok {
		k.ver = c.continuousVersion
		c.sbaQuiet = k
	}
	return c
}

// cloneHeldEvent copies the held-back cast trigger's PutOnStack event
// (deferredPush): an emitted event is an immutable value, so its slices are
// shared.
func cloneHeldEvent(ev *events.Event) *events.Event {
	if ev == nil {
		return nil
	}
	cp := *ev
	return &cp
}

// cloneCastMods copies a suspended cast's cost composition (pendingCast.mods):
// its raise and reduction lists are the clone's own, while the composed extra
// Cost is shared, as the cast's clone always has.
func cloneCastMods(m costMods) costMods {
	m.Raises = append([]int32(nil), m.Raises...)
	m.Reduces = append([]costMod(nil), m.Reduces...)
	return m
}

// forClone is the no-ability-loss proof (abilityloss.go) a clone takes: same
// objects, its own registry copy, which it rechecks once.
func (p abilityLossProof) forClone() abilityLossProof {
	return abilityLossProof{seen: p.seen, objs: p.objs, contLen: -1}
}

// recycledSlice zeroes a spent array to its capacity (so it pins none of its
// elements' references) and returns it empty: Release's `release=clear`.
func recycledSlice[T any](b []T) []T {
	b = b[:cap(b)]
	clear(b)
	return b[:0]
}

// releasedTrigZones invalidates every trigger zone summary, keeping its id
// arrays, for the next engine to copy into.
func releasedTrigZones(zs []trigZoneSummary) []trigZoneSummary {
	for i := range zs {
		zs[i].resetSummary()
	}
	return zs[:0]
}

// releasedReplZones is releasedTrigZones for the replacement summaries.
func releasedReplZones(zs []replZoneSummary) []replZoneSummary {
	for i := range zs {
		z := &zs[i]
		*z = replZoneSummary{ids: z.ids[:0], hotIDs: z.hotIDs[:0]}
	}
	return zs[:0]
}

// releaseCastFree frees the last issued pendingCast (cast_pool.go) and hands
// over the zeroed storage, for the next engine's castFree.
func releaseCastFree(e *Engine) *pendingCast {
	e.recycleCast()
	pc := e.castFree
	e.castIssued = nil
	return pc
}

// clonePendingTriggers gives a clone ownership of the mutable context carried
// by both the ordinary trigger queue and a CR 605.3b batch parked on a mana
// colour choice. Card and SA pointers remain shared immutable corpus data.
func clonePendingTriggers(src []pendingTrigger) []pendingTrigger {
	if src == nil {
		return nil
	}
	out := make([]pendingTrigger, len(src))
	for i, pt := range src {
		pt.Ctx.Targets = append([]state.Target(nil), pt.Ctx.Targets...)
		if pt.Ctx.ModeTargets != nil {
			pt.Ctx.ModeTargets = make([][]state.Target, len(pt.Ctx.ModeTargets))
			for i, group := range pt.Ctx.ModeTargets {
				pt.Ctx.ModeTargets[i] = append([]state.Target(nil), group...)
			}
		}
		pt.Ctx.Remembered = append([]state.Target(nil), pt.Ctx.Remembered...)
		if pt.Ctx.Snap.TargetController != nil {
			m := make(map[state.ObjID]state.PlayerID, len(pt.Ctx.Snap.TargetController))
			for id, controller := range pt.Ctx.Snap.TargetController {
				m[id] = controller
			}
			pt.Ctx.Snap.TargetController = m
		}
		if pt.Ctx.Snap.TargetCounters != nil {
			pt.Ctx.Snap.TargetCounters = effects.CloneTargetCountersLKI(pt.Ctx.Snap.TargetCounters)
		}
		if pt.Ctx.Snap.TargetPT != nil {
			pt.Ctx.Snap.TargetPT = effects.CloneTargetPTLKI(pt.Ctx.Snap.TargetPT)
		}
		if pt.Ctx.Snap.TargetSpell != nil {
			pt.Ctx.Snap.TargetSpell = effects.CloneTargetSpellLKI(pt.Ctx.Snap.TargetSpell)
		}
		if pt.Ctx.SVars != nil {
			m := make(map[string]string, len(pt.Ctx.SVars))
			for k, v := range pt.Ctx.SVars {
				m[k] = v
			}
			pt.Ctx.SVars = m
		}
		if pt.Ctx.LKI != nil {
			lki := pt.Ctx.LKI.CloneDeep()
			pt.Ctx.LKI = &lki
		}
		out[i] = pt
	}
	return out
}

// cloneCost deep-copies a Cost: every slice field is re-allocated so the
// copy owns its own backing arrays and an append through either copy can
// never write into the other's slot (the growth pattern
// recordUnlessPaymentPick produces while a payment advances). The field
// list below is the COMPLETE set of Cost's slice fields, in declaration
// order: a Cost clone must be driven by the full struct, never by the
// subset one call site happens to read, so the next cost component cannot
// be forgotten. TestCloneCostCoversEveryCostSlice walks Cost with reflect
// and fails if a slice field is added without a line here.
func cloneCost(c Cost) Cost {
	c.Hybrid = append([]ManaPair(nil), c.Hybrid...)
	c.Phyrexian = append([]byte(nil), c.Phyrexian...)
	c.Twobrid = append([]Twobrid(nil), c.Twobrid...)
	c.HybridPhyrexian = append([]HybridPhyrexian(nil), c.HybridPhyrexian...)
	c.Sac = append([]CostPart(nil), c.Sac...)
	c.Discard = append([]CostPart(nil), c.Discard...)
	c.SubCounter = append([]CostPart(nil), c.SubCounter...)
	c.AddCounter = append([]CostPart(nil), c.AddCounter...)
	c.Exile = append([]CostPart(nil), c.Exile...)
	c.ExileFromTop = append([]CostPart(nil), c.ExileFromTop...)
	c.Reveal = append([]CostPart(nil), c.Reveal...)
	c.RevealOrChoose = append([]CostPart(nil), c.RevealOrChoose...)
	c.RevealChosen = append([]CostPart(nil), c.RevealChosen...)
	c.Behold = append([]CostPart(nil), c.Behold...)
	c.TapPermanent = append([]CostPart(nil), c.TapPermanent...)
	c.UntapPermanent = append([]CostPart(nil), c.UntapPermanent...)
	c.Blight = append([]CostPart(nil), c.Blight...)
	c.Exert = append([]CostPart(nil), c.Exert...)
	c.Draw = append([]CostPart(nil), c.Draw...)
	c.Energy = append([]CostPart(nil), c.Energy...)
	c.LifeX = append([]CostPart(nil), c.LifeX...)
	c.DamageYou = append([]CostPart(nil), c.DamageYou...)
	c.GainLife = append([]CostPart(nil), c.GainLife...)
	c.Return = append([]CostPart(nil), c.Return...)
	c.PutToLib = append([]CostPart(nil), c.PutToLib...)
	c.MoveToGrave = append([]CostPart(nil), c.MoveToGrave...)
	c.Mill = append([]CostPart(nil), c.Mill...)
	c.Evidence = append([]CostPart(nil), c.Evidence...)
	c.RollDice = append([]CostPart(nil), c.RollDice...)
	c.Withheld = append([]string(nil), c.Withheld...)
	c.Unknown = append([]string(nil), c.Unknown...)
	return c
}

// cloneDecision deep-copies a posed (or deferred) decision's slices, so a
// clone's answer path never writes through to the original's.
func cloneDecision(p *decision.Decision) *decision.Decision {
	d := *p.Clone()
	d.ResumeModes = append([]string(nil), p.ResumeModes...)
	d.ResumeDigPrimary = append([]state.ObjID(nil), p.ResumeDigPrimary...)
	return &d
}

// cloneTrigger copies a compiled trigger line whose Params map the clone owns
// (a granted or continuous trigger is shared by value otherwise).
func cloneTrigger(t cards.Trigger) cards.Trigger {
	if t.Params != nil {
		params := make(map[string]string, len(t.Params))
		for key, value := range t.Params {
			params[key] = value
		}
		t.Params = params
	}
	return t
}
