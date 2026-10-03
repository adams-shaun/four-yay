package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cloneTurnLedger copies the per-turn ledger cluster wholesale
// (engine_turnledger.go): every member is the fresh-slice copy class, so a
// clone never shares backing state with the original. turnStartTurns (the
// next-turn boundary cache) is copied like turnsTaken so a clone never
// shares the backing slice; combatHitsThisTurn (the per-turn combat-damage
// ledger) is a plain value slice, copied like turnsTaken so an undo/DVR
// clone owns its own ledger; crimeSeatsThisTurn and bendSeatsThisTurn are
// plain scalars, carried by the struct copy.
func cloneTurnLedger(t engineTurnLedger) engineTurnLedger {
	out := t
	out.turnsTaken = append([]int32(nil), t.turnsTaken...)
	out.turnsTakenEpoch = t.turnsTakenEpoch
	out.turnStartTurns = cloneTurnStartTurns(t.turnStartTurns)
	out.turnStartEpoch = t.turnStartEpoch
	out.combatHitsThisTurn = append([]effects.CombatDamageHit(nil), t.combatHitsThisTurn...)
	out.counterAddsThisTurn = cloneCounterAddsThisTurn(t.counterAddsThisTurn)
	out.activationsThisTurn = cloneActivationsThisTurn(t.activationsThisTurn)
	return out
}

func cloneTurnStartTurns(in [][]int32) [][]int32 {
	if in == nil {
		return nil
	}
	out := make([][]int32, len(in))
	for i, v := range in {
		out[i] = append([]int32(nil), v...)
	}
	return out
}

func cloneCounterAddsThisTurn(in []counterAddedThisTurn) []counterAddedThisTurn {
	if in == nil {
		return nil
	}
	out := make([]counterAddedThisTurn, len(in))
	for i, v := range in {
		out[i] = v
		out[i].object = v.object.CloneDeep()
	}
	return out
}

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
		deckManifests:     e.deckManifests,
		L:                 e.L.CloneIntoFrom(sp.events, sp.evFrom, sp.evN, sp.evDirty, sp.intents),
		compiledText:      e.compiledText,
		landTypeWords:     e.landTypeWords,
		format:            e.format,
		rng:               e.rng.clone(),
		mulligans:         e.mulligans,
		windowDiagnostics: e.windowDiagnostics,
		startingLife:      e.startingLife,
		// The livelock watcher (rules/livelock.go): carry the Config-given
		// guard thresholds, reset the observation state. A clone only happens
		// at an intent boundary -- the only moment these fields are not being
		// written -- where the watcher holds no in-flight run or quiet count
		// worth carrying, so a fresh watcher over the same thresholds is a
		// faithful copy.
		loop:                newLivelockWatcherFromGuard(e.loop.guard, sp.loopSigs, sp.loopRecent, sp.loopPrev, sp.loopHeads, sp.loopHash),
		setNameInPool:       e.setNameInPool,
		layer4InPool:        e.layer4InPool,
		controlStaticInPool: e.controlStaticInPool,
	}
	// The identity-preserving copy of the suspended resolution's memories,
	// transactions and frames (clone_remap.go): stack-resident, allocating
	// only when there is something to copy.
	var remap cloneRemap
	c.trigGrant = e.trigGrant.forClone()
	// The no-ability-loss proof (abilityloss.go): same objects, its own
	// registry copy, which it rechecks once.
	c.lossProof = abilityLossProof{seen: e.lossProof.seen, objs: e.lossProof.objs, contLen: -1}
	// The per-turn ledger cluster (engine_turnledger.go) is one clone
	// class: every member is copied as a fresh slice so a clone owns its
	// own ledgers; the detail lives on cloneTurnLedger.
	c.engineTurnLedger = cloneTurnLedger(e.engineTurnLedger)
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
	c.orderedTriggers = e.orderedTriggers
	c.applyingReplacement = e.applyingReplacement
	c.choosing = e.choosing
	c.untapChoiceObj = e.untapChoiceObj
	c.drainAwaitsTarget = e.drainAwaitsTarget
	c.drainAwaitsModes = e.drainAwaitsModes
	c.trigSub = e.trigSub.clone()
	c.deferCastTrigger = e.deferCastTrigger
	// blockerRound (combat.go, Task m34): the declare-blockers round's
	// defender list and cursor, plain-value state like the mulligan round.
	// The order slice itself is never mutated (askBlockers only advances
	// the cursor), so sharing it between a clone and its original is safe,
	// the same reference-sharing Clone already practises for
	// orderedTriggers.
	c.blockerRound = e.blockerRound
	// unblockedRoundChecked (engine.go): the plain-value per-combat latch
	// of the declare-blockers round-complete trigger walk.
	c.unblockedRoundChecked = e.unblockedRoundChecked
	// exertAskState (combat.go, task exert1): the exert election's offer
	// list and cursor, the same plain-value class as blockerRound -- the
	// offers slice is never mutated, so sharing the reference is safe.
	c.exertAskState = e.exertAskState
	// enlistAskState (enlist.go, task enlist1): the enlist election's
	// declaration, offer list and cursor, the same plain-value class as
	// exertAskState -- the slices are never mutated, so sharing the
	// references is safe.
	c.enlistAskState = e.enlistAskState
	// stationing (station.go): the plain-value spacecraft a pending
	// Station tap pick belongs to; zero whenever none is outstanding.
	c.stationing = e.stationing
	// combatRound (combat.go, Task jj-cmb): the combat damage step's
	// pass/division continuation state. The queue, answered divisions
	// and the pending ask's option-split table are all written in place
	// as a pass progresses (handleDamageDivision, askNextDivision), so a
	// clone must own its own copies -- the cast/pendingTriggers class,
	// not the blockerRound share class.
	c.combatRound = cloneCombatRound(e.combatRound)
	// pregame / mulligan (rules/mulligan.go, engine.go): the London
	// round's own state. Both are documented on the Engine as fields
	// Clone copies, and neither was here — so a clone taken between the
	// opening deal and turn 1 came back with the round not running and a
	// zero mulliganRound, and re-submitting the recorded mulligan intent
	// found no seat in mulligan.seats and indexed kept[-1]. That is the
	// exact path host.viewAt takes (clone a snapshot, re-Submit the
	// intents), so every view?seq= inside the mulligan window 500'd.
	// The three slices are re-allocated, not shared: kept and taken are
	// written in place, so this is the cast/pendingTriggers class, not
	// the blockerRound class above.
	c.pregame = e.pregame
	c.mulligan = cloneMulligan(e.mulligan)
	c.opening = cloneOpening(e.opening)
	// coloring / colorRound / mulligans (rules/commander_color.go): the
	// CR 903.4b pregame colour round's state and the carried Mulligans
	// limit. colorRound.asks is never mutated (only the cursor advances),
	// so sharing the reference is safe -- the blockerRound class.
	c.coloring = e.coloring
	c.colorRound = e.colorRound
	// tossChoice (rules/starting_player_choice.go) is CR 103.1's pending
	// winner-chooses ask: a plain value (no slices, no closure), so the
	// blockerRound share class -- Clone copies it directly, and a clone
	// taken while the ask is outstanding re-poses the same decision for
	// the same winner. host.viewAt clones a snapshot and re-Submits the
	// intents, so the choice must survive like the mulligan round does.
	c.tossChoice = e.tossChoice
	// oppSel (rules/stack.go) is the TargetingPlayer$ Opponent selection
	// ask's flow record -- plain scalars like tossChoice, so it is copied
	// the same way: a clone taken while the which-opponent ask is
	// outstanding re-poses the same selection.
	c.oppSel = e.oppSel
	// oppPicksMid (rules/stack.go) is the effects-tier answered-selection
	// store, keyed by SA line. Re-allocated (not shared) so the two
	// engines' next reads cannot collide.
	c.oppPicksMid = cloneOppPicksMid(e.oppPicksMid)
	// tpCtlChooser (rules/stack.go) is the TargetingPlayerControls$
	// answered-ask record, keyed by the resolving stack object. Plain
	// struct values, re-allocated like oppPicksMid so a clone taken
	// between the answer and the CR 608.2b recheck still sees the seat
	// that answered.
	c.tpCtlChooser = cloneTpCtlChooser(e.tpCtlChooser)
	// E2 held-out cast suppression (cast.go): the set of card ids whose
	// cast option is held out of the current window after an unpayable
	// decline. A clone taken at any intent boundary carries it forward so
	// a cloned engine offers exactly the same cast options the original
	// would (a declined card stays held out until a state change
	// re-enables it, in both engines alike). It is a plain map of object
	// ids, so it must be re-allocated, not shared.
	c.suppressedCast = cloneSuppressed(e.suppressedCast)
	// The inert backstop's held-out priority options
	// (rules/priority_guard.go): the suppressedCast class and lifetime,
	// so a clone offers exactly the window the original would.
	c.inertHeldOut = cloneInertHeldOut(e.inertHeldOut)
	// F05-2 per-card no-progress count (engine.go), carried alongside the
	// held-out set for the same reason: a clone taken at an intent
	// boundary must count a card's no-progress aborts exactly as the
	// live engine does, or a clone would offer (or hold out) a cast the
	// original would not. Same map-of-scalars class, so re-allocated, not
	// shared.
	c.castAborts = cloneAbortCounts(e.castAborts)
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
	c.suspendedCasts = append([]state.ObjID(nil), e.suspendedCasts...)
	c.defeatedCasts = append([]state.ObjID(nil), e.defeatedCasts...)
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
	if e.etbMove != nil {
		ev := *e.etbMove
		c.etbMove = &ev
	}
	c.etbNext = e.etbNext
	c.etbLandPlay, c.etbLandObj, c.etbLandPlayer = e.etbLandPlay, e.etbLandObj, e.etbLandPlayer
	if e.riotMove != nil {
		ev := *e.riotMove
		c.riotMove = &ev
	}
	if e.unleashMove != nil {
		ev := *e.unleashMove
		c.unleashMove = &ev
	}
	if e.siegeMove != nil {
		ev := *e.siegeMove
		c.siegeMove = &ev
	}
	if e.entryStageDone != nil {
		st := *e.entryStageDone
		c.entryStageDone = &st
	}
	// Parked-mint collectors (rules/token_rest.go): value data keyed by id,
	// re-allocated so a clone's answer never appends into the original's.
	c.mintParkFrom, c.mintSinkSeq = e.mintParkFrom, e.mintSinkSeq
	c.mintParkElection = e.mintParkElection
	c.tokenMintSinkID, c.pendingMintSink = e.tokenMintSinkID, e.pendingMintSink
	c.copyMintsPending = append([]state.ObjID(nil), e.copyMintsPending...)
	if e.mintSinks != nil {
		c.mintSinks = make([]mintSink, len(e.mintSinks))
		for i, ms := range e.mintSinks {
			c.mintSinks[i] = mintSink{id: ms.id, ids: append([]state.ObjID(nil), ms.ids...)}
		}
	}
	if e.untapResume != nil {
		r := *e.untapResume
		c.untapResume = &r
	}
	if e.attachedChoice != nil {
		ac := *e.attachedChoice
		c.attachedChoice = &ac
	}
	c.attachedApplying = e.attachedApplying
	if e.tokenChoice != nil {
		tc := *e.tokenChoice
		// plan is the slice the resume mutates in place; matches is read-only
		// after the park (the repl pointers are immutable face entries), so
		// only the plan is re-allocated.
		tc.plan = append([]tokenPlanMint(nil), e.tokenChoice.plan...)
		c.tokenChoice = &tc
	}
	if e.pending != nil {
		c.pending = cloneDecision(e.pending)
	}
	if e.deferredAsks != nil {
		c.deferredAsks = make([]*decision.Decision, len(e.deferredAsks))
		for i, d := range e.deferredAsks {
			c.deferredAsks[i] = cloneDecision(d)
		}
	}
	if e.resume != nil {
		// Plain value data (kind/obj plus a *cards.SA into the shared
		// immutable corpus — the same pointer class every other field here
		// shares), so one struct copy is a faithful clone (M2d-2). A clone
		// made while a mid-resolution decision is pending sees the same
		// suspended resolution the original does. The outer continuation
		// chain (fx34) is a linked list of these same value frames, so it is
		// deep-copied per-link to keep the clone independent of the
		// original's list.
		c.resume = remap.resume(e.resume)
	}
	c.controlGrants = append([]controlGrant(nil), e.controlGrants...)
	// A parked ExchangeLife transaction (life_exchange.go): parked exactly
	// when a side's life change suspended on a decision, so it is live at the
	// intent boundary that decision makes, and Submit's tail settles it
	// (settlePendingLifeExchange). The clone owns the transaction and its
	// staged sides, and its rider memory is the clone's one copy -- the same
	// one the clone's resume frames carry (cloneRemap), so the clone's settle
	// reaches its own resumed reader and never the original's.
	c.pendingLifeExchange = remap.lifeExchange(e.pendingLifeExchange)
	if e.counterTypeAsk != nil {
		c.counterTypeAsk = make(map[state.ObjID]*counterTypePending, len(e.counterTypeAsk))
		for id, p := range e.counterTypeAsk {
			if p == nil {
				continue
			}
			c.counterTypeAsk[id] = &counterTypePending{sa: p.sa, answers: append([]string(nil), p.answers...)}
		}
	}
	// The per-turn ManaExpend tally (engine scratch, rules/cast.go): a clone
	// taken at an intent boundary must resume mid-turn with the original's
	// cumulative spend, or a crossing measured after the clone would see a
	// reset tally. Copied as a plain value slice plus its turn stamp.
	c.manaExpended = append([]int32(nil), e.manaExpended...)
	c.manaExpendedTurn = e.manaExpendedTurn
	// The in-flight Resolve chain's target-controller snapshot (engine
	// scratch, published by effects.Resolve): nil at an intent boundary, but
	// copied as a plain map when present so the clone owns its own storage.
	c.resolvingTargetControllerLKI = effects.CloneTargetControllerLKI(e.resolvingTargetControllerLKI)
	if e.continuous != nil {
		c.continuous = make([]ContinuousEffect, len(e.continuous))
		for i, ce := range e.continuous {
			ce.AddKeywords = append([]string(nil), ce.AddKeywords...)
			ce.RemoveKeywords = append([]string(nil), ce.RemoveKeywords...)
			ce.CantHaveKeywords = append([]string(nil), ce.CantHaveKeywords...)
			ce.AddTypes = append([]string(nil), ce.AddTypes...)
			ce.RemoveTypes = append([]string(nil), ce.RemoveTypes...)
			if ce.RestrictParams != nil {
				m := make(map[string]string, len(ce.RestrictParams))
				for k, v := range ce.RestrictParams {
					m[k] = v
				}
				ce.RestrictParams = m
			}
			if ce.AssignmentStaticParams != nil {
				m := make(map[string]string, len(ce.AssignmentStaticParams))
				for k, v := range ce.AssignmentStaticParams {
					m[k] = v
				}
				ce.AssignmentStaticParams = m
			}
			if ce.AssignmentStaticSVars != nil {
				m := make(map[string]string, len(ce.AssignmentStaticSVars))
				for k, v := range ce.AssignmentStaticSVars {
					m[k] = v
				}
				ce.AssignmentStaticSVars = m
			}
			ce.Remembered = append([]state.ObjID(nil), ce.Remembered...)
			ce.RememberedPlayers = append([]state.PlayerID(nil), ce.RememberedPlayers...)
			ce.Chosen = append([]state.ObjID(nil), ce.Chosen...)
			// The has-all-abilities-of face lists: deep-copied like the other
			// rider slices so an intent-boundary clone never shares a backing
			// array the live engine may extend (the entries' Face pointers are
			// immutable compiled faces and are shared deliberately).
			ce.GainedFaces = append([]state.GainedFace(nil), ce.GainedFaces...)
			ce.GainedTriggerFaces = append([]state.GainedFace(nil), ce.GainedTriggerFaces...)
			ce.ShieldTargets = append([]state.ObjID(nil), ce.ShieldTargets...)
			ce.ShieldTargetPlayers = append([]state.PlayerID(nil), ce.ShieldTargetPlayers...)
			if ce.ReplacementParams != nil {
				m := make(map[string]string, len(ce.ReplacementParams))
				for k, v := range ce.ReplacementParams {
					m[k] = v
				}
				ce.ReplacementParams = m
			}
			c.continuous[i] = ce
		}
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
	if e.triggerContexts != nil {
		c.triggerContexts = make(map[state.ObjID]effects.TriggerContext, len(e.triggerContexts))
		for id, tc := range e.triggerContexts {
			c.triggerContexts[id] = tc
		}
	}
	if e.triggerEffectFrames != nil {
		c.triggerEffectFrames = make(map[state.ObjID]effects.EffectFrame, len(e.triggerEffectFrames))
		for id, ef := range e.triggerEffectFrames {
			c.triggerEffectFrames[id] = ef
		}
	}
	if e.triggerLines != nil {
		c.triggerLines = make(map[state.ObjID]cards.Trigger, len(e.triggerLines))
		for id, t := range e.triggerLines {
			if t.Params != nil {
				params := make(map[string]string, len(t.Params))
				for key, value := range t.Params {
					params[key] = value
				}
				t.Params = params
			}
			c.triggerLines[id] = t
		}
	}
	if e.triggerLineSVars != nil {
		c.triggerLineSVars = make(map[state.ObjID]map[string]string, len(e.triggerLineSVars))
		for id, svars := range e.triggerLineSVars {
			// Card script tables are immutable after parsing; only the lookup
			// index is engine-owned. An undo clone retains the same owning face.
			c.triggerLineSVars[id] = svars
		}
	}
	if e.triggerLKI != nil {
		c.triggerLKI = make(map[state.ObjID]triggerObjectLKI, len(e.triggerLKI))
		for id, lki := range e.triggerLKI {
			if lki.object != nil {
				cp := lki.object.CloneDeep()
				lki.object = &cp
			}
			c.triggerLKI[id] = lki
		}
	}
	if e.sacrificedLKI != nil {
		c.sacrificedLKI = make(map[state.ObjID][]state.SacrificedInfo, len(e.sacrificedLKI))
		for id, info := range e.sacrificedLKI {
			c.sacrificedLKI[id] = append([]state.SacrificedInfo(nil), info...)
		}
	}
	if e.castExiled != nil {
		c.castExiled = make(map[state.ObjID][]state.ObjID, len(e.castExiled))
		for id, ids := range e.castExiled {
			c.castExiled[id] = append([]state.ObjID(nil), ids...)
		}
	}
	if e.castRevealed != nil {
		c.castRevealed = make(map[state.ObjID][]state.ObjID, len(e.castRevealed))
		for id, ids := range e.castRevealed {
			c.castRevealed[id] = append([]state.ObjID(nil), ids...)
		}
	}
	if e.fuseTargets != nil {
		c.fuseTargets = make(map[state.ObjID][][]state.Target, len(e.fuseTargets))
		for id, stages := range e.fuseTargets {
			cp := make([][]state.Target, len(stages))
			for i, sl := range stages {
				cp[i] = append([]state.Target(nil), sl...)
			}
			c.fuseTargets[id] = cp
		}
	}
	// moveCounterAsk / aorAsk (resolution.go, the movecounter1/counterchoice1
	// discipline): the two decision-derived answer cursors keyed by the
	// resolving stack object. Both are written by the resume arms and read by
	// seedMoveCounter/seedAorAsk on every fresh re-entry Ctx, so a clone taken
	// while one of these resolutions is suspended (the pending mid-resolution
	// ask IS the intent boundary) must carry the answered entries forward or
	// the clone re-asks an already-answered kind and the decision/event stream
	// diverges from the original's. Both are the cast/pendingTriggers class:
	// deep-copied, including the inner maps and pointed-to pendings, never
	// shared.
	if e.moveCounterAsk != nil {
		c.moveCounterAsk = make(map[state.ObjID]*moveCounterPending, len(e.moveCounterAsk))
		for id, p := range e.moveCounterAsk {
			cp := *p
			cp.targets = append([]state.Target(nil), p.targets...)
			c.moveCounterAsk[id] = &cp
		}
	}
	// targetsPickAsk (resolution.go, the general form of the same
	// discipline): the answered generic ValidTgts$ pre-ask per resolving
	// stack object and SA Line. Same reason as the two above -- a clone taken
	// while such a resolution is suspended must carry the answer or the clone
	// re-poses the pre-ask and diverges. Deep-copied to the inner map and the
	// target slices; nothing is shared.
	if e.targetsPickAsk != nil {
		c.targetsPickAsk = make(map[state.ObjID]map[string][]state.Target, len(e.targetsPickAsk))
		for id, byLine := range e.targetsPickAsk {
			inner := make(map[string][]state.Target, len(byLine))
			for line, ts := range byLine {
				inner[line] = append([]state.Target(nil), ts...)
			}
			c.targetsPickAsk[id] = inner
		}
	}
	if e.aorAsk != nil {
		c.aorAsk = make(map[state.ObjID]map[string]bool, len(e.aorAsk))
		for id, set := range e.aorAsk {
			inner := make(map[string]bool, len(set))
			for k, v := range set {
				inner[k] = v
			}
			c.aorAsk[id] = inner
		}
	}
	if e.castSubTargets != nil {
		c.castSubTargets = make(map[state.ObjID]map[string][]state.Target, len(e.castSubTargets))
		for id, lines := range e.castSubTargets {
			cm := make(map[string][]state.Target, len(lines))
			for line, ts := range lines {
				cm[line] = append([]state.Target(nil), ts...)
			}
			c.castSubTargets[id] = cm
		}
	}
	if e.resolutionTargets != nil {
		c.resolutionTargets = make(resolutionTargetMap, len(e.resolutionTargets))
		for id, set := range e.resolutionTargets {
			set.flat = append([]state.Target(nil), set.flat...)
			set.modes = cloneCharmTargetGroups(set.modes)
			c.resolutionTargets[id] = set
		}
	}
	if e.copyTargetStage != nil {
		c.copyTargetStage = make(map[state.ObjID]int, len(e.copyTargetStage))
		for id, stage := range e.copyTargetStage {
			c.copyTargetStage[id] = stage
		}
	}
	if e.copyAnswerTargets != nil {
		c.copyAnswerTargets = make(map[state.ObjID][][]decision.Option, len(e.copyAnswerTargets))
		for id, stages := range e.copyAnswerTargets {
			cp := make([][]decision.Option, len(stages))
			for i, sl := range stages {
				cp[i] = append([]decision.Option(nil), sl...)
			}
			c.copyAnswerTargets[id] = cp
		}
	}
	if e.charmTargets != nil {
		c.charmTargets = make(map[state.ObjID][][]state.Target, len(e.charmTargets))
		for id, groups := range e.charmTargets {
			cp := make([][]state.Target, len(groups))
			for i, group := range groups {
				cp[i] = append([]state.Target(nil), group...)
			}
			c.charmTargets[id] = cp
		}
	}
	if e.exploitedLKI != nil {
		c.exploitedLKI = make(map[state.ObjID]state.SacrificedInfo, len(e.exploitedLKI))
		for id, info := range e.exploitedLKI {
			c.exploitedLKI[id] = info
		}
	}
	if e.sourceLifelinkLKI != nil {
		c.sourceLifelinkLKI = make(map[state.ObjID]bool, len(e.sourceLifelinkLKI))
		for id, link := range e.sourceLifelinkLKI {
			c.sourceLifelinkLKI[id] = link
		}
	}
	if e.sourceControllerLKI != nil {
		c.sourceControllerLKI = make(map[state.ObjID]state.PlayerID, len(e.sourceControllerLKI))
		for id, controller := range e.sourceControllerLKI {
			c.sourceControllerLKI[id] = controller
		}
	}
	if e.sourceCharLKI != nil {
		// Each snapshot's counters slice is never written after capture, so
		// the clone shares it.
		c.sourceCharLKI = make(map[state.ObjID]sourceCharSnapshot, len(e.sourceCharLKI))
		for id, snap := range e.sourceCharLKI {
			c.sourceCharLKI[id] = snap
		}
	}
	if e.damageSourceLKI != nil {
		c.damageSourceLKI = make(map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI, len(e.damageSourceLKI))
		for stack, lki := range e.damageSourceLKI {
			c.damageSourceLKI[stack] = cloneDamageSourceLKI(lki)
		}
	}
	c.triggerFireCount = cloneCounts(e.triggerFireCount)
	if e.damageBatchOpen {
		c.damageBatchOpen = true
		c.damageBatchDepth = e.damageBatchDepth
		if e.damageBatchIdx != nil {
			c.damageBatchIdx = make(map[damageBatchKey]int, len(e.damageBatchIdx))
			for k, v := range e.damageBatchIdx {
				c.damageBatchIdx[k] = v
			}
		}
		// Deep-copy the DamageAll batch sets: a shared backing array under two
		// engines' appends must never leak an entry across a clone boundary.
		c.damageBatchLog = make([]damageBatchEntry, len(e.damageBatchLog))
		for i, ent := range e.damageBatchLog {
			c.damageBatchLog[i] = ent
			if len(ent.sources) > 0 {
				c.damageBatchLog[i].sources = append([]state.ObjID(nil), ent.sources...)
			}
			if len(ent.targets) > 0 {
				c.damageBatchLog[i].targets = append([]state.Target(nil), ent.targets...)
			}
		}
	}
	// The ChangeZoneTable$ zone batch and the api:Discard discard batch stay
	// OPEN across a mid-resolution suspension (a RepeatEach/Dig loop body's
	// ask, effDiscard's choice): the first pass opens the bracket and the pass
	// that completes the action closes it. A clone taken at that intent
	// boundary must carry the open bracket and its entries -- the damage
	// batch's class -- or its resumed pass queues per move and never patches
	// the triggers the first pass queued.
	if e.zoneBatchOpen {
		c.zoneBatchOpen, c.zoneBatchDepth = true, e.zoneBatchDepth
		c.zoneBatchIdx = cloneBatchIdx(e.zoneBatchIdx)
		if e.zoneBatchLog != nil {
			c.zoneBatchLog = make([]zoneBatchEntry, len(e.zoneBatchLog))
			for i, ent := range e.zoneBatchLog {
				ent.moved = append([]state.Target(nil), ent.moved...)
				c.zoneBatchLog[i] = ent
			}
		}
	}
	if e.discardBatchOpen {
		c.discardBatchOpen, c.discardBatchDepth = true, e.discardBatchDepth
		c.discardBatchIdx = cloneBatchIdx(e.discardBatchIdx)
		if e.discardBatchLog != nil {
			c.discardBatchLog = make([]discardBatchEntry, len(e.discardBatchLog))
			for i, ent := range e.discardBatchLog {
				ent.discarded = append([]state.Target(nil), ent.discarded...)
				c.discardBatchLog[i] = ent
			}
		}
	}
	if e.phaseUnknownNoted != nil {
		c.phaseUnknownNoted = make(map[string]bool, len(e.phaseUnknownNoted))
		for k, v := range e.phaseUnknownNoted {
			c.phaseUnknownNoted[k] = v
		}
	}
	if e.disableTriggersNoted != nil {
		c.disableTriggersNoted = make(map[string]bool, len(e.disableTriggersNoted))
		for k, v := range e.disableTriggersNoted {
			c.disableTriggersNoted[k] = v
		}
	}
	// phaseSpecs, the unbound-face triggerEventMasks fallback and
	// triggerObjectMasks are pure syntax caches. Leave them empty: each branch
	// owns its writable caches, unlike diagnostic history.
	c.triggerObjectMasks = nil
	if e.triggerTurnFires != nil {
		c.triggerTurnFires = make(map[triggerKey]turnFires, len(e.triggerTurnFires))
		for k, v := range e.triggerTurnFires {
			c.triggerTurnFires[k] = v
		}
	}
	if e.triggerGameFires != nil {
		c.triggerGameFires = make(map[triggerKey]gameFires, len(e.triggerGameFires))
		for k, v := range e.triggerGameFires {
			c.triggerGameFires[k] = v
		}
	}
	if e.unblockedOnceFired != nil {
		c.unblockedOnceFired = make(map[triggerKey]combatFires, len(e.unblockedOnceFired))
		for k, v := range e.unblockedOnceFired {
			c.unblockedOnceFired[k] = v
		}
	}
	if e.attackersDeclaredFired != nil {
		c.attackersDeclaredFired = make(map[triggerKey]combatFires, len(e.attackersDeclaredFired))
		for k, v := range e.attackersDeclaredFired {
			c.attackersDeclaredFired[k] = v
		}
	}
	if e.triggerTurnResolved != nil {
		c.triggerTurnResolved = make(map[state.ObjID]turnFires, len(e.triggerTurnResolved))
		for k, v := range e.triggerTurnResolved {
			c.triggerTurnResolved[k] = v
		}
	}
	c.triggerTurnDiceTurn = e.triggerTurnDiceTurn
	if e.triggerTurnDice != nil {
		c.triggerTurnDice = make(map[triggerKey]turnFires, len(e.triggerTurnDice))
		for k, v := range e.triggerTurnDice {
			c.triggerTurnDice[k] = v
		}
	}
	if e.tappedTurn != nil {
		c.tappedTurn = make(map[state.ObjID]int32, len(e.tappedTurn))
		for id, turn := range e.tappedTurn {
			c.tappedTurn[id] = turn
		}
	}
	if e.discardAllTurn != nil {
		c.discardAllTurn = make(map[triggerKey]int32, len(e.discardAllTurn))
		for k, turn := range e.discardAllTurn {
			c.discardAllTurn[k] = turn
		}
	}
	// tapObj/tapPlayer/tapEntering and tappingForMana/tappingManaProduced are
	// emitTap's synchronous context, zero at every intent boundary.
	// triggerBefore is scoped to a batch emission/resumption, so it is nil
	// at intent boundaries and is deliberately not copied. Parked replacement
	// and commander choices and resume frames retain their own immutable
	// triggerSnapshot pointers; sharing those is safe because matching builds
	// fresh Engine scratch caches and never applies events to the snapshot.
	//
	// foreachBuf / foreachDepth (Task A2) are deliberately NOT copied: they
	// are forEachObject's scratch snapshot buffer and re-entry depth counter
	// (engine.go), live only for the duration of a single walk. A clone is
	// taken at an intent boundary (never mid-walk, so foreachDepth is zero);
	// sharing the buffer field between the original and the clone would be a
	// bug, because either one walking would clobber the other's zone snapshot
	// mid-range. Leaving both zero lets each engine grow its own buffer on
	// its next depth-0 forEachObject call.
	//
	// staticContinuous / staticEpoch are COPIED into the clone's own outer
	// storage (below, after the struct literal): staticEffects rebuilds into
	// the memo's reusable outer storage, so each branch must own its backing
	// array, while the nested keyword/type slices are read-only once built
	// and are shared. The clone's board and log are the parent's at the clone
	// boundary, so the memo describes the clone exactly; it is carried only
	// when it was built under the current registry (staticVersion ==
	// continuousVersion) or read no registry state at all (staticMemoQuiet),
	// re-keyed to the clone's own zero continuousVersion.
	// Otherwise the zero epoch forces a fresh scan. The static-control
	// reconcile (rules/control_static.go) derives its wanted set fresh from
	// the same memo under the same epoch key, so it needs no copied cache
	// either; reconcilingControlStatics (engine.go) is a transient re-entry
	// guard, false at every intent boundary exactly like expiringControl,
	// which Clone has never copied for the same reason.
	//
	// activeBuf / activeEpoch / activeVersion / activeDepth / continuousVersion
	// (engine.go, layers.go) are likewise deliberately NOT copied, with the
	// same precedent. activeBuf is active()'s shared sorted effect list and
	// activeDepth its re-entry guard; a clone must grow its own buffer, never
	// alias the original's scratch, or the copy's next rebuild would clobber
	// the original's live cache mid-range (or vice versa). activeEpoch /
	// activeVersion / continuousVersion start at zero in the fresh struct, so
	// the cloned engine misses the cache and rebuilds the identical,
	// deterministic list on its first Derived after the clone boundary.
	//
	// derivedKW / derivedTypes / derivedDepth (engine.go, layers.go) are the
	// same class: Derived's reusable keyword/type scratch buffers and their
	// re-entry guard. A clone starts with nil buffers and grows its own on its
	// first full Derived, never aliasing the original's mutable scratch — the
	// A2 buffer / C3 digest precedent, spelled out in clone.go's contract.
	if e.manaActivation != nil {
		ma := *e.manaActivation
		ma.abilities = append([]*cards.SA(nil), e.manaActivation.abilities...)
		c.manaActivation = &ma
	}
	if e.manaColorActivation != nil {
		ma := *e.manaColorActivation
		ma.triggers = clonePendingTriggers(e.manaColorActivation.triggers)
		if pt := e.manaColorActivation.trigger; pt != nil {
			ma.trigger = &clonePendingTriggers([]pendingTrigger{*pt})[0]
		}
		// A routed off-stack-mana rider ask parks its resume chain here; the
		// clone must own its own chain (the engine's own e.resume does too),
		// or resuming the clone would traverse the original's outer links.
		ma.nestedResume = remap.resume(e.manaColorActivation.nestedResume)
		c.manaColorActivation = &ma
	}
	if e.manaDiscardActivation != nil {
		ma := *e.manaDiscardActivation
		ma.cost = cloneCost(e.manaDiscardActivation.cost)
		ma.sacs = append([]state.ObjID(nil), e.manaDiscardActivation.sacs...)
		ma.discards = append([]state.ObjID(nil), e.manaDiscardActivation.discards...)
		ma.exiles = append([]state.ObjID(nil), e.manaDiscardActivation.exiles...)
		ma.taps = append([]state.ObjID(nil), e.manaDiscardActivation.taps...)
		ma.untaps = append([]state.ObjID(nil), e.manaDiscardActivation.untaps...)
		ma.subCounterPays = append([]subCounterPay(nil), e.manaDiscardActivation.subCounterPays...)
		c.manaDiscardActivation = &ma
	}
	if e.manaAfterCost != nil {
		ma := *e.manaAfterCost
		ma.triggers = clonePendingTriggers(e.manaAfterCost.triggers)
		ma.sacs = append([]state.ObjID(nil), e.manaAfterCost.sacs...)
		c.manaAfterCost = &ma
	}
	if e.manaUnlessActivation != nil {
		ma := *e.manaUnlessActivation
		ma.triggers = clonePendingTriggers(e.manaUnlessActivation.triggers)
		ma.payers = append([]state.PlayerID(nil), e.manaUnlessActivation.payers...)
		c.manaUnlessActivation = &ma
	}
	if e.unlessPayment != nil {
		u := *e.unlessPayment
		// The whole Cost is deep-copied through cloneCost so no cost component
		// can be forgotten when one is added (this block predates Return and
		// Exile once already); the pick slices ride alongside it.
		u.cost = cloneCost(e.unlessPayment.cost)
		u.sacs = append([]state.ObjID(nil), e.unlessPayment.sacs...)
		u.discards = append([]state.ObjID(nil), e.unlessPayment.discards...)
		u.reveals = append([]state.ObjID(nil), e.unlessPayment.reveals...)
		u.beholds = append([]state.ObjID(nil), e.unlessPayment.beholds...)
		u.returns = append([]state.ObjID(nil), e.unlessPayment.returns...)
		u.exiles = append([]state.ObjID(nil), e.unlessPayment.exiles...)
		u.ctx = remap.unlessCtx(e.unlessPayment.ctx)
		u.rp = remap.resume(e.unlessPayment.rp)
		c.unlessPayment = &u
	}
	if e.cumulative != nil {
		cu := *e.cumulative
		cu.amount = cloneCost(e.cumulative.amount)
		if e.cumulative.action != nil {
			action := *e.cumulative.action
			cu.action = &action
		}
		c.cumulative = &cu
	}
	if e.triggerCost != nil {
		tc := *e.triggerCost
		tc.resume = remap.resume(e.triggerCost.resume)
		tc.amount = cloneCost(e.triggerCost.amount)
		tc.sacs = append([]state.ObjID(nil), e.triggerCost.sacs...)
		tc.exiles = append([]state.ObjID(nil), e.triggerCost.exiles...)
		tc.moveGraves = append([]state.ObjID(nil), e.triggerCost.moveGraves...)
		c.triggerCost = &tc
	}
	if e.echo != nil {
		// kw:Echo (rules/echo.go): the same plain-value class as cumulative
		// above — the Cost's slice fields deep-copied so the clone owns them.
		ef := *e.echo
		ef.amount = cloneCost(e.echo.amount)
		c.echo = &ef
	}
	if e.wardMana != nil {
		wm := *e.wardMana
		c.wardMana = &wm
	}
	// attackPay (combat.go/attack_cost.go): the declare-attackers attack-cost
	// payment window. The chosen slice is shared with the original -- the
	// enlistAsk reference-sharing class, never mutated by the window.
	if e.attackPay != nil {
		ap := *e.attackPay
		c.attackPay = &ap
	}
	if e.blockPay != nil {
		bp := *e.blockPay
		c.blockPay = &bp
	}
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
	if e.turnUp != nil {
		// The CR 708.6 turn-up payment flow (rules/morph_turnup.go): a plain
		// value struct with four object slices, cloned like cast so a clone
		// taken while one of its KChoose asks is outstanding re-answers it
		// faithfully (host.viewAt clones and re-Submits the intents).
		tp := *e.turnUp
		tp.sacs = append([]state.ObjID(nil), e.turnUp.sacs...)
		tp.discs = append([]state.ObjID(nil), e.turnUp.discs...)
		tp.reveal = append([]state.ObjID(nil), e.turnUp.reveal...)
		tp.returns = append([]state.ObjID(nil), e.turnUp.returns...)
		tp.mods.reduces = append([]costMod(nil), e.turnUp.mods.reduces...)
		tp.mods.raises = append([]int32(nil), e.turnUp.mods.raises...)
		c.turnUp = &tp
	}
	if e.cmdZone != nil {
		// The parked commander zone changes (CR 903.9, Task m32): a clone
		// taken while a KCommanderZone decision is outstanding must carry the
		// same queue the original does, or answering the copied decision
		// would find no parked move and the commander would never move.
		// Plain value entries, so one slice copy is a faithful clone.
		c.cmdZone = append([]cmdZoneMove(nil), e.cmdZone...)
	}
	if e.legendBatch != nil {
		// The parked CR 704.5j legend-rule application (rules/sba.go): a clone
		// taken while the duplicate set's controller is choosing must carry
		// the same batch, or answering the copied decision would find nothing
		// parked and the kept permanent would be recorded against an empty
		// flow. Plain value data plus one shared immutable snapshot, so the
		// slices are re-allocated and `before` is shared like cmdZoneMove's.
		lb := *e.legendBatch
		lb.group.ids = append([]state.ObjID(nil), e.legendBatch.group.ids...)
		lb.dead = append([]casualty(nil), e.legendBatch.dead...)
		c.legendBatch = &lb
	}
	if e.replChoices != nil {
		// Parked replacement choices (MoveZone/ProduceMana/BeginPhase order,
		// replacement mana colour and optional phase apply/decline): same class
		// as cmdZone. Event/scalar data copies by value;
		// candidate pointers share immutable corpus data, while every mutable
		// bookkeeping slice is re-allocated for the clone.
		c.replChoices = make([]replChoice, len(e.replChoices))
		for i, rc := range e.replChoices {
			rc.cands = append([]replMatch(nil), rc.cands...)
			rc.used = append([]replMatch(nil), rc.used...)
			rc.applied = append([]bool(nil), rc.applied...)
			rc.appliedRepls = append([]replMatch(nil), rc.appliedRepls...)
			rc.applicable = append([]int(nil), rc.applicable...)
			// A parked life-replacement choice's ExchangeLife transaction is
			// the same object pendingLifeExchange may hold, and resumeAtPose
			// is compared against Engine.resume by identity: both go through
			// the remap so the clone owns them and the identities survive.
			rc.exchange = remap.lifeExchange(rc.exchange)
			rc.resumeAtPose = remap.resume(rc.resumeAtPose)
			if rc.untap != nil {
				resume := *rc.untap
				rc.untap = &resume
			}
			if rc.stage != nil {
				// The stage is mutable while its ask is outstanding: the
				// completed re-drive consumes its token-plan tail and the
				// resume appends placements. Copy the value and re-allocate
				// the tail so a clone answered on either side cannot alias
				// the original's continuation.
				st := *rc.stage
				if st.tokenPlan != nil {
					tp := *st.tokenPlan
					tp.plan = append([]tokenPlanMint(nil), st.tokenPlan.plan...)
					st.tokenPlan = &tp
				}
				st.applied = append([]replMatch(nil), st.applied...)
				st.placed = append([]events.EntryCounterGrant(nil), st.placed...)
				st.bodyIDs = append([]string(nil), st.bodyIDs...)
				rc.stage = &st
			}
			c.replChoices[i] = rc
		}
	}
	c.madnessChoices = append([]events.Event(nil), e.madnessChoices...)
	c.madnessSuspended = e.madnessSuspended
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

// cloneCounts copies a trigger-bookkeeping map, preserving nil (trigger.go
// lazily allocates these on first use and checks for nil itself).
func cloneCounts(m map[triggerKey]int32) map[triggerKey]int32 {
	if m == nil {
		return nil
	}
	out := make(map[triggerKey]int32, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// cloneInertHeldOut copies the inert backstop's held-out set, preserving nil.
func cloneInertHeldOut(m map[inertKey]bool) map[inertKey]bool {
	if m == nil {
		return nil
	}
	out := make(map[inertKey]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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

// cloneOppPicksMid copies the TargetingPlayer$ Opponent mid-tier selection
// store (oppPicksMid, stack.go), preserving nil; the lazily-allocated map
// only ever carries the pin between the "opp_pick" resume arm and the
// synchronous ChooserFor read, so a nil map reads as an empty store.
func cloneOppPicksMid(m map[string]state.PlayerID) map[string]state.PlayerID {
	if m == nil {
		return nil
	}
	out := make(map[string]state.PlayerID, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// cloneTpCtlChooser copies the TargetingPlayerControls$ answered-ask record
// (tpCtlChooser, stack.go), preserving nil; values are plain structs, so a
// memberwise copy is complete.
func cloneTpCtlChooser(m map[state.ObjID]tpCtlAnswer) map[state.ObjID]tpCtlAnswer {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID]tpCtlAnswer, len(m))
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

// cloneMulligan deep-copies the London round: the phase flags and counters
// are plain values, but seats/kept/taken are written in place while the
// round runs (rules/mulligan.go), so the copy must own its own arrays.
func cloneMulligan(m mulliganRound) mulliganRound {
	m.seats = append([]state.PlayerID(nil), m.seats...)
	m.kept = append([]bool(nil), m.kept...)
	m.taken = append([]int(nil), m.taken...)
	return m
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

// cloneCombatRound deep-copies the combat damage step's continuation state
// (combat.go, Task jj-cmb): the division queue, answered divisions and the
// pending ask's option-split table are all written in place while a pass
// progresses, so a clone must own its own arrays rather than alias the
// original's.
func cloneCombatRound(cr combatRound) combatRound {
	cr.queue = append([]state.ObjID(nil), cr.queue...)
	cr.assignments = append([]assignment(nil), cr.assignments...)
	if cr.done != nil {
		done := make([]divChoice, len(cr.done))
		for i, dc := range cr.done {
			done[i] = divChoice{attacker: dc.attacker, amounts: append([]int32(nil), dc.amounts...)}
		}
		cr.done = done
	}
	if cr.askOptions != nil {
		table := make([][]int32, len(cr.askOptions))
		for i, row := range cr.askOptions {
			table[i] = append([]int32(nil), row...)
		}
		cr.askOptions = table
	}
	// asunblk1: the as-unblocked election queues the same way.
	cr.electQueue = append([]state.ObjID(nil), cr.electQueue...)
	cr.doneElect = append([]state.ObjID(nil), cr.doneElect...)
	// CR 726.2 pass adjudication state: the landed candidate list must not
	// alias the original's (the holder snapshot and the bools are plain
	// values, carried by the value copy).
	cr.initCtrls = append([]state.PlayerID(nil), cr.initCtrls...)
	// askElection is a plain bool, carried by the value copy.
	return cr
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

// cloneParentLinks deep-copies a resume point's parent-link record, so a
// frame's record never shares a backing array with another frame's (the same
// rule every other sliced ride in cloneResume follows). A recorded empty
// entry (a Min-0 parent) is preserved as an empty entry, never dropped: its
// PRESENCE is what makes the parent empty rather than unset.
func cloneParentLinks(links [][]state.Target) [][]state.Target {
	if links == nil {
		return nil
	}
	out := make([][]state.Target, len(links))
	for i, ts := range links {
		out[i] = append([]state.Target(nil), ts...)
	}
	return out
}

func cloneResume(rp *resumePoint) *resumePoint { return cloneResumeWith(rp, nil) }

// cloneResumeWith is cloneResume with the cross-engine identity remap: m nil
// is an intra-engine copy (a continuation frame of the same resolution, which
// keeps sharing the resolution's memories); a non-nil m is Clone's, which
// gives the clone its own copy of every memory and frame, each copied once.
func cloneResumeWith(rp *resumePoint, m *cloneRemap) *resumePoint {
	if rp == nil {
		return nil
	}
	if m != nil {
		if q := m.resumes.find(rp); q != nil {
			return q
		}
	}
	cp := new(resumePoint)
	*cp = *rp
	if m != nil {
		m.resumes.add(rp, cp)
	}
	// The resolution's coin-flip and ExchangeLife rider memories are mutated
	// in place by the resolution after the suspension: a clone owns one copy
	// of each, shared by all of its frames (cloneRemap).
	cp.flipMemory = m.flipMemory(rp.flipMemory)
	cp.exchangeMemory = m.exchangeMemory(rp.exchangeMemory)
	cp.clash = cloneClashResume(rp.clash)
	cp.choices = append([]state.Target(nil), rp.choices...)
	cp.chosenValid = rp.chosenValid
	cp.remembered = append([]state.Target(nil), rp.remembered...)
	cp.forgetOtherSnapshot = append([]state.Target(nil), rp.forgetOtherSnapshot...)
	cp.forgetOtherOwners = append([]state.PlayerID(nil), rp.forgetOtherOwners...)
	cp.loopRemembered = append([]state.Target(nil), rp.loopRemembered...)
	// The AmountFromVotes$ tally snapshot: plain value entries, copied so the
	// clone never shares a backing array with the original's pending frames.
	cp.voteCounts = cloneVoteCounts(rp.voteCounts)
	cp.replacedCards = append([]state.ObjID(nil), rp.replacedCards...)
	// The pre-move controller snapshot is immutable once captured, but a clone
	// must not share the original's map storage: an explicit copy keeps the
	// two engines' pending frames independent.
	cp.targetControllerLKI = effects.CloneTargetControllerLKI(rp.targetControllerLKI)
	cp.targetCountersLKI = effects.CloneTargetCountersLKI(rp.targetCountersLKI)
	cp.targetPTLKI = effects.CloneTargetPTLKI(rp.targetPTLKI)
	cp.targetSpellLKI = effects.CloneTargetSpellLKI(rp.targetSpellLKI)
	cp.targetsUnique = append([]state.Target(nil), rp.targetsUnique...)
	// The parent-link record and the pending in-walk link answer are sliced
	// values the resumed Ctx re-binds (effects.Ctx.ResumeParentLinks), so the
	// clone owns its own copies instead of sharing backing arrays with the
	// original's pending frames -- the same discipline every other slice here
	// follows. An empty recorded link (a Min-0 parent) is preserved as an
	// entry, not dropped.
	cp.parentLinks = cloneParentLinks(rp.parentLinks)
	cp.linkAnswer = append([]state.Target(nil), rp.linkAnswer...)
	cp.linkAnswered = rp.linkAnswered
	// The VillainousChoice cursor and victim binding are sliced values the
	// resumed Ctx re-binds, so the clone owns its own copies instead of
	// sharing backing arrays with the original (the same discipline every
	// other slice here follows).
	cp.villainousVictims = append([]state.Target(nil), rp.villainousVictims...)
	cp.villainousRemembered = append([]state.Target(nil), rp.villainousRemembered...)
	// The multi-player GenericChoice chooser cursor is likewise a sliced value
	// the resumed Ctx re-binds; the clone owns its own copy.
	cp.genericChoosers = append([]state.Target(nil), rp.genericChoosers...)
	cp.genericRemembered = append([]state.Target(nil), rp.genericRemembered...)
	cp.numberPicks = append([]int32(nil), rp.numberPicks...)
	cp.tokenRest = rp.tokenRest.Clone()
	if rp.repeat != nil {
		cur := *rp.repeat
		cur.subjects = append([]state.Target(nil), rp.repeat.subjects...)
		cur.last = append([]state.Target(nil), rp.repeat.last...)
		cp.repeat = &cur
	}
	// A deferred second ask (Engine.Ask) rides its own decision and resume
	// point; the frame re-links deferredResume.outer when posed, so the clone
	// must own both.
	if rp.deferredAsk != nil {
		cp.deferredAsk = cloneDecision(rp.deferredAsk)
	}
	cp.deferredResume = cloneResumeWith(rp.deferredResume, m)
	cp.outer = cloneResumeWith(rp.outer, m)
	return cp
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

// cloneBatchIdx copies an open trigger batch's line index (the zone and
// discard batches), preserving nil.
func cloneBatchIdx[K comparable](m map[K]int) map[K]int {
	if m == nil {
		return nil
	}
	out := make(map[K]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
