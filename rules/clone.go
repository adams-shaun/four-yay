package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
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

func (e *Engine) cloneWith(sp Spare) *Engine {
	c := &Engine{
		G: e.G.CloneIntoDirty(sp.objs, sp.objDirty),
		// The genesis manifests are immutable after New (nothing writes
		// deckManifests; OwnDeck publishes copies, OwnDeckShared is read-only
		// by contract), so a clone shares them instead of copying every
		// seat's rows per clone -- the search clones a root per simulation.
		deckManifests: e.deckManifests,
		L:             e.L.CloneIntoFrom(sp.events, sp.evFrom, sp.evN, sp.evDirty, sp.intents),
		compiledText:  e.compiledText,
		landTypeWords: e.landTypeWords,
		rng:           e.rng.clone(),
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
	cloneEngineFields(c, e, &remap)
	c.trigGrant = e.trigGrant.forClone()
	// The no-ability-loss proof (abilityloss.go): same objects, its own
	// registry copy, which it rechecks once.
	c.lossProof = abilityLossProof{seen: e.lossProof.seen, objs: e.lossProof.objs, contLen: -1}
	// The offer walk's incremental log indexes (legal_walk_scratch.go): the
	// clone's log is a copy of this one, so each watermark still names the
	// same prefix and the clone resumes the fold instead of redoing it.
	c.legalScratch = cloneLegalWalkScratch(e.legalScratch)
	// The offer walk's scratch lists come from the Spare (a spent engine's,
	// cleared); a zero Spare leaves them nil, as Clone always has.
	c.legalOptBuf, c.manaAbBuf = sp.legalOpts, sp.manaAb
	// The offer walk's object classes (walk_objclass.go) are exact as of
	// the static catch-up's watermark; the clone's log is a copy of this
	// one, so it carries both and catches up the rest itself.
	if e.walkClsOwner == e && len(e.walkObjCls) != 0 {
		c.walkObjCls, c.walkClsOwner = append(sp.walkCls[:0], e.walkObjCls...), c
		c.staticZonesEp = e.staticZonesEp
	}
	// A spent engine's zeroed pendingCast storage (cast_pool.go).
	c.castFree = sp.cast
	// The resolution kernel (rules/resolve): the switch, and a posed tape
	// resolution's immutable checkpoint, shared by pointer (lasagna spec
	// §7.1); a spent engine's dropped checkpoint storage is recycled.
	c.tape = e.tape.ForClone()
	if sp.tapeCkpt != nil {
		c.tapeSpare = *sp.tapeCkpt
	}

	c.trigSub = e.trigSub.clone()

	// blockerRound (combat.go, Task m34): the declare-blockers round's
	// defender list and cursor, plain-value state like the mulligan round.
	// The order slice itself is never mutated (askBlockers only advances
	// the cursor), so sharing it between a clone and its original is safe,
	// the same reference-sharing Clone already practises for
	// orderedTriggers.
	c.blockerRound = e.blockerRound

	// exertAskState (combat.go, task exert1): the exert election's offer
	// list and cursor, the same plain-value class as blockerRound -- the
	// offers slice is never mutated, so sharing the reference is safe.
	c.exertAskState = e.exertAskState
	// enlistAskState (enlist.go, task enlist1): the enlist election's
	// declaration, offer list and cursor, the same plain-value class as
	// exertAskState -- the slices are never mutated, so sharing the
	// references is safe.
	c.enlistAskState = e.enlistAskState

	c.colorRound = e.colorRound

	// The staticEffects memo (layercache.go), copied into recycled storage;
	// see the staticContinuous note further down.
	if e.staticEpoch > 0 && (e.staticVersion == e.continuousVersion || e.staticMemoQuiet()) {
		c.staticContinuous = append(sp.static[:0], e.staticContinuous...)
		c.staticEpoch, c.staticObjs = e.staticEpoch, e.staticObjs
		c.staticVersion = c.continuousVersion
		c.staticMemoGated, c.staticMemoStateRead = e.staticMemoGated, e.staticMemoStateRead
		// The memo's gate records (static_gatememo.go) travel with it, into
		// recycled storage: they are immutable values naming the shared
		// card tables and the (arena-index) source ids both boards agree on.
		if e.staticGatesKnown {
			c.staticGates, c.staticGatesKnown = append(sp.gates[:0], e.staticGates...), true
		} else {
			c.staticGates = sp.gates[:0]
		}
	} else {
		if sp.static != nil {
			c.staticContinuous = sp.static[:0]
		}
		c.staticGates = sp.gates[:0]
	}

	c.queuedPlays = e.queuedPlays.clone(&remap)
	// attackOffers' memo (attack_cost.go), carried under the same identical-
	// board argument as the tables below: a search clones the engine while
	// its declare-attackers decision is pending, and the clone's
	// validateAttackers then reuses the list askAttackers derived instead of
	// re-deriving it per simulation. The list is shared, never written (a
	// recompute stores a fresh slice); the key's registry version is rekeyed
	// onto the clone's.
	if e.atkOffersEp > 0 && e.atkOffersVer == e.continuousVersion {
		c.atkOffers, c.atkOffersEp = e.atkOffers, e.atkOffersEp
		c.atkOffersVer, c.atkOffersObjs, c.atkOffersActive = c.continuousVersion, e.atkOffersObjs, e.atkOffersActive
	}
	// setname.go's layer-3 rename table and its genesis-time gate. The
	// clone's board is identical at the clone boundary, so the table is
	// carried with its (epoch, version) key rather than rebuilt -- but as
	// a fresh slice, never the original's backing array, so the two
	// engines' next refreshes cannot write over each other. This is what
	// keeps a clone's name filters reading the CLONE's board once the two
	// diverge (setname_filter_scope_test.go).
	c.renames = append([]effects.ObjectName(nil), e.renames...)
	c.renameEpoch = e.renameEpoch
	c.renameVersion = rekeyVersion(e.renameVersion, e.continuousVersion)
	c.renameObjs = e.renameObjs
	// layer4types.go's layer-4 derived-type table and its genesis-time
	// gate, carried with its (epoch, version) key for the same reason: the
	// clone's board is identical at the clone boundary, and a fresh slice
	// (never the original's backing array) keeps the two engines' next
	// refreshes from writing over each other, so a clone's type filters
	// read the CLONE's board once the two diverge.
	c.layer4Types = append([]effects.ObjectTypes(nil), e.layer4Types...)
	c.typesEpoch = e.typesEpoch
	c.typesVersion = rekeyVersion(e.typesVersion, e.continuousVersion)
	c.typesObjs = e.typesObjs
	// The incremental layer-4 state and the statics probe cache describe the
	// same identical board, so a table built under the current registry
	// carries them too (engine_derived_tables.go): the clone's next refresh
	// goes incremental instead of re-deriving and re-probing the whole board.
	if e.typesIncrReady {
		c.typesIncrReady, c.typesSelfOnly = true, e.typesSelfOnly
		c.typesSrcs = append([]state.ObjID(nil), e.typesSrcs...)
		c.typesMayDiffer = append([]state.ObjID(nil), e.typesMayDiffer...)
	}
	if e.typesProbeReady {
		c.typesProbe = append(sp.probe[:0], e.typesProbe...)
		c.typesProbeReady, c.typesProbeTrue = true, e.typesProbeTrue
		c.typesProbeEpoch, c.typesProbeObjs = e.typesProbeEpoch, e.typesProbeObjs
		c.typesProbeVersion = c.continuousVersion
	} else {
		c.typesProbe = sp.probe[:0]
	}

	if len(e.pendingTriggers) > 0 {
		c.pendingTriggers = clonePendingTriggers(e.pendingTriggers)
	} else if e.pendingTriggers != nil || sp.pending != nil {
		// An empty queue (a drained one keeps its array, putTriggersOnStack)
		// takes the recycled array instead of allocating its first batch.
		c.pendingTriggers = sp.pending
	}
	// The emit path's working storage, recycled from a spent engine (genesis
	// Release): zone summaries arrive all invalid, the list arrays empty.
	// The zone summaries are carried (copied into the recycled tables) with
	// their catch-up positions: same board, same log, same obligations.
	c.trigZones, c.trigZonesEp = copyTrigZones(sp.trigZones, e.trigZones), e.trigZonesEp
	c.replZones, c.replZonesEp = copyReplZones(sp.replZones, e.replZones), e.replZonesEp
	c.replArena = e.replArena
	c.activeBuf, c.activeBufAlt = sp.activeBuf, sp.activeBufAlt
	c.activeSrc = sp.activeSrc
	c.lossMemo = sp.lossMemo

	// phaseSpecs, the unbound-face triggerEventMasks fallback and
	// triggerObjectMasks are pure syntax caches. Leave them empty: each branch
	// owns its writable caches, unlike diagnostic history.
	c.triggerObjectMasks = nil

	if e.cast != nil {
		pc := *e.cast
		pc.cost = cloneCost(e.cast.cost)
		pc.revealHandArm = append([]bool(nil), e.cast.revealHandArm...)
		pc.mayPlayHosts = append([]state.ObjID(nil), e.cast.mayPlayHosts...)
		if e.cast.costRemembered != nil {
			pc.costRemembered = make([]costRememberedEntry, len(e.cast.costRemembered))
			for i, c := range e.cast.costRemembered {
				pc.costRemembered[i] = costRememberedEntry{source: c.source, stamp: c.stamp,
					ids: append([]state.ObjID(nil), c.ids...)}
			}
		}
		pc.mods.reduces = append([]costMod(nil), e.cast.mods.reduces...)
		pc.mods.raises = append([]int32(nil), e.cast.mods.raises...)
		pc.delve = append([]state.ObjID(nil), e.cast.delve...)
		pc.sacs = append([]state.ObjID(nil), e.cast.sacs...)
		pc.discards = append([]state.ObjID(nil), e.cast.discards...)
		pc.subCounterPays = append([]subCounterPay(nil), e.cast.subCounterPays...)
		pc.exiles = append([]state.ObjID(nil), e.cast.exiles...)
		pc.returns = append([]state.ObjID(nil), e.cast.returns...)
		pc.moveGraves = append([]state.ObjID(nil), e.cast.moveGraves...)
		pc.putToLibs = append([]state.ObjID(nil), e.cast.putToLibs...)
		pc.reveals = append([]state.ObjID(nil), e.cast.reveals...)
		pc.beholds = append([]state.ObjID(nil), e.cast.beholds...)
		pc.taps = append([]state.ObjID(nil), e.cast.taps...)
		pc.blights = append([]state.ObjID(nil), e.cast.blights...)
		pc.subAsks = append([]*cards.SA(nil), e.cast.subAsks...)
		if e.cast.subAns != nil {
			pc.subAns = make([][]state.Target, len(e.cast.subAns))
			for i, ts := range e.cast.subAns {
				pc.subAns[i] = append([]state.Target(nil), ts...)
			}
		}
		pc.rootOpts = append([]decision.Option(nil), e.cast.rootOpts...)
		// The chosen targets, the Fuse per-stage target slices and the convoke
		// taps all grow by append while the cast's target and payment asks are
		// answered, so a clone sharing their backing arrays would let either
		// engine's next answer write the other's spare-capacity slot.
		pc.targets = append([]state.Target(nil), e.cast.targets...)
		if e.cast.stageTargets != nil {
			pc.stageTargets = make([][]state.Target, len(e.cast.stageTargets))
			for i, ts := range e.cast.stageTargets {
				pc.stageTargets[i] = append([]state.Target(nil), ts...)
			}
		}
		pc.convoke = append([]convokePayment(nil), e.cast.convoke...)
		pc.evidence = append([]state.ObjID(nil), e.cast.evidence...)
		pc.preModes = append([]string(nil), e.cast.preModes...)
		if e.cast.charmTargets != nil {
			pc.charmTargets = make([][]state.Target, len(e.cast.charmTargets))
			for i, group := range e.cast.charmTargets {
				pc.charmTargets[i] = append([]state.Target(nil), group...)
			}
		}
		pc.preSuppress = cloneSuppressed(e.cast.preSuppress)
		pc.preAborts = cloneAbortCounts(e.cast.preAborts)
		pc.proposalTriggers = append([][2]int(nil), e.cast.proposalTriggers...)
		if e.cast.payment != nil {
			payment := *e.cast.payment
			payment.plan = decision.ClonePaymentPlan(e.cast.payment.plan)
			pc.payment = &payment
		}
		if e.cast.paymentFallback != nil {
			fallback := *e.cast.paymentFallback
			pc.paymentFallback = &fallback
		}
		pc.windowTaps = cloneWindowTaps(e.cast.windowTaps)
		if e.cast.mayPlayRemembered != nil {
			m := make(map[state.ObjID][]state.ObjID, len(e.cast.mayPlayRemembered))
			for k, v := range e.cast.mayPlayRemembered {
				m[k] = append([]state.ObjID(nil), v...)
			}
			pc.mayPlayRemembered = m
		}
		c.cast = &pc
		// The held-back cast trigger (CR 601.2i, cast.go): a clone taken at an
		// intent boundary while a cast is suspended (its target/choose decision
		// pending) must carry the deferred PutOnStack event and its LKI so that
		// re-Submitting the target answer still fires the cast trigger in the
		// clone, exactly as it does in the original. The event is a value; the
		// LKI is a read-only snapshot safely shared like every other immutable
		// Object pointer in this function.
		if e.deferredPush != nil {
			ev := *e.deferredPush
			c.deferredPush = &ev
		}
		c.deferredPushLKI = e.deferredPushLKI
	}

	// A clone's Derived memo starts empty (it is not copied above); recycled
	// tables start empty over Release-cleared capacity, the same zeroed state
	// derivedMemoizedAt's growth relies on for a Config.Spare game.
	c.derivedMemo, c.derivedMemoStack = sp.memo, sp.memoStack
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
		if pt.Ctx.TargetControllerLKI != nil {
			m := make(map[state.ObjID]state.PlayerID, len(pt.Ctx.TargetControllerLKI))
			for id, controller := range pt.Ctx.TargetControllerLKI {
				m[id] = controller
			}
			pt.Ctx.TargetControllerLKI = m
		}
		if pt.Ctx.TargetCountersLKI != nil {
			pt.Ctx.TargetCountersLKI = effects.CloneTargetCountersLKI(pt.Ctx.TargetCountersLKI)
		}
		if pt.Ctx.TargetPTLKI != nil {
			pt.Ctx.TargetPTLKI = effects.CloneTargetPTLKI(pt.Ctx.TargetPTLKI)
		}
		if pt.Ctx.TargetSpellLKI != nil {
			pt.Ctx.TargetSpellLKI = effects.CloneTargetSpellLKI(pt.Ctx.TargetSpellLKI)
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

// cloneResume deep-copies a suspended resolution's resume chain (fx34): each
// link is plain value data (kind/obj plus a *cards.SA into the shared,
// immutable corpus), but the outer continuation chain is a linked list this
// cloned engine must own so it can resume outward independently of the
// original's traversal.
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
	d.ResumeClash = cloneClashResume(p.ResumeClash)
	d.ResumeModes = append([]string(nil), p.ResumeModes...)
	d.ResumeChoices = append([]state.Target(nil), p.ResumeChoices...)
	d.ResumeChosenValid = p.ResumeChosenValid
	d.ResumeRemembered = append([]state.Target(nil), p.ResumeRemembered...)
	d.ResumeSearchKnown = append([]state.Target(nil), p.ResumeSearchKnown...)
	d.ResumeForgetOtherSnapshot = append([]state.Target(nil), p.ResumeForgetOtherSnapshot...)
	d.ResumeForgetOtherOwners = append([]state.PlayerID(nil), p.ResumeForgetOtherOwners...)
	d.ResumeVillainousVictims = append([]state.Target(nil), p.ResumeVillainousVictims...)
	d.ResumeVillainousIndex = p.ResumeVillainousIndex
	d.ResumeGenericChoosers = append([]state.Target(nil), p.ResumeGenericChoosers...)
	d.ResumeGenericChooserIndex = p.ResumeGenericChooserIndex
	d.ResumeNumberPicks = append([]int32(nil), p.ResumeNumberPicks...)
	d.ResumeTargetsUnique = append([]state.Target(nil), p.ResumeTargetsUnique...)
	d.ResumeDigPrimary = append([]state.ObjID(nil), p.ResumeDigPrimary...)
	return &d
}

// cloneSuppressed copies the held-out cast set (suppressedCast, engine.go),
// preserving nil (cast.go allocates it lazily; a nil set reads as empty).
func cloneSuppressed(m map[state.ObjID]bool) map[state.ObjID]bool {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// cloneAbortCounts copies the F05-2 per-card no-progress count
// (castAborts, engine.go), preserving nil; the lazily-allocated map is
// created by abortCast and a nil map reads as an empty count.
func cloneAbortCounts(m map[state.ObjID]int32) map[state.ObjID]int32 {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID]int32, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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

func cloneClashResume(r *decision.ClashResume) *decision.ClashResume {
	if r == nil {
		return nil
	}
	return &decision.ClashResume{Players: append([]state.PlayerID(nil), r.Players...), Revealed: append([]state.ObjID(nil), r.Revealed...), Winner: r.Winner, Cursor: r.Cursor}
}
