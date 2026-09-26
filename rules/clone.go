package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
func (e *Engine) Clone() *Engine {
	c := &Engine{
		G:                  e.G.Clone(),
		L:                  e.L.Clone(),
		compiledText:       e.compiledText,
		replayPaymentPlans: e.replayPaymentPlans,
		landTypeWords:      e.landTypeWords,
		turnsTaken:         append([]int32(nil), e.turnsTaken...),
		turnsTakenEpoch:    e.turnsTakenEpoch,
		// turnStartTurns (the next-turn boundary cache) is copied like
		// turnsTaken so a clone never shares the backing slice.
		turnStartTurns: cloneTurnStartTurns(e.turnStartTurns),
		turnStartEpoch: e.turnStartEpoch,
		// combatHitsThisTurn (the per-turn combat-damage ledger): a plain
		// value slice, copied like turnsTaken so an undo/DVR clone owns its
		// own ledger.
		combatHitsThisTurn:  append([]effects.CombatDamageHit(nil), e.combatHitsThisTurn...),
		counterAddsThisTurn: cloneCounterAddsThisTurn(e.counterAddsThisTurn),
		format:              e.format,
		rng:                 e.rng.clone(),
		orderedTriggers:     e.orderedTriggers,
		applyingReplacement: e.applyingReplacement,
		choosing:            e.choosing,
		untapChoiceObj:      e.untapChoiceObj,
		drainAwaitsTarget:   e.drainAwaitsTarget,
		drainAwaitsModes:    e.drainAwaitsModes,
		deferCastTrigger:    e.deferCastTrigger,
		// blockerRound (combat.go, Task m34): the declare-blockers round's
		// defender list and cursor, plain-value state like the mulligan round.
		// The order slice itself is never mutated (askBlockers only advances
		// the cursor), so sharing it between a clone and its original is safe,
		// the same reference-sharing Clone already practises for
		// orderedTriggers.
		blockerRound: e.blockerRound,
		// unblockedRoundChecked (engine.go): the plain-value per-combat latch
		// of the declare-blockers round-complete trigger walk.
		unblockedRoundChecked: e.unblockedRoundChecked,
		// exertAskState (combat.go, task exert1): the exert election's offer
		// list and cursor, the same plain-value class as blockerRound -- the
		// offers slice is never mutated, so sharing the reference is safe.
		exertAskState: e.exertAskState,
		// enlistAskState (enlist.go, task enlist1): the enlist election's
		// declaration, offer list and cursor, the same plain-value class as
		// exertAskState -- the slices are never mutated, so sharing the
		// references is safe.
		enlistAskState: e.enlistAskState,
		// stationing (station.go): the plain-value spacecraft a pending
		// Station tap pick belongs to; zero whenever none is outstanding.
		stationing: e.stationing,
		// combatRound (combat.go, Task jj-cmb): the combat damage step's
		// pass/division continuation state. The queue, answered divisions
		// and the pending ask's option-split table are all written in place
		// as a pass progresses (handleDamageDivision, askNextDivision), so a
		// clone must own its own copies -- the cast/pendingTriggers class,
		// not the blockerRound share class.
		combatRound: cloneCombatRound(e.combatRound),
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
		pregame:  e.pregame,
		mulligan: cloneMulligan(e.mulligan),
		opening:  cloneOpening(e.opening),
		// coloring / colorRound / mulligans (rules/commander_color.go): the
		// CR 903.4b pregame colour round's state and the carried Mulligans
		// limit. colorRound.asks is never mutated (only the cursor advances),
		// so sharing the reference is safe -- the blockerRound class.
		coloring:   e.coloring,
		colorRound: e.colorRound,
		// tossChoice (rules/starting_player_choice.go) is CR 103.1's pending
		// winner-chooses ask: a plain value (no slices, no closure), so the
		// blockerRound share class -- Clone copies it directly, and a clone
		// taken while the ask is outstanding re-poses the same decision for
		// the same winner. host.viewAt clones a snapshot and re-Submits the
		// intents, so the choice must survive like the mulligan round does.
		tossChoice:   e.tossChoice,
		mulligans:    e.mulligans,
		startingLife: e.startingLife,
		// E2 held-out cast suppression (cast.go): the set of card ids whose
		// cast option is held out of the current window after an unpayable
		// decline. A clone taken at any intent boundary carries it forward so
		// a cloned engine offers exactly the same cast options the original
		// would (a declined card stays held out until a state change
		// re-enables it, in both engines alike). It is a plain map of object
		// ids, so it must be re-allocated, not shared.
		suppressedCast: cloneSuppressed(e.suppressedCast),
		// The inert backstop's held-out priority options
		// (rules/priority_guard.go): the suppressedCast class and lifetime,
		// so a clone offers exactly the window the original would.
		inertHeldOut: cloneInertHeldOut(e.inertHeldOut),
		// F05-2 per-card no-progress count (engine.go), carried alongside the
		// held-out set for the same reason: a clone taken at an intent
		// boundary must count a card's no-progress aborts exactly as the
		// live engine does, or a clone would offer (or hold out) a cast the
		// original would not. Same map-of-scalars class, so re-allocated, not
		// shared.
		castAborts:     cloneAbortCounts(e.castAborts),
		suspendedCasts: append([]state.ObjID(nil), e.suspendedCasts...),
		defeatedCasts:  append([]state.ObjID(nil), e.defeatedCasts...),
		// The livelock watcher (rules/livelock.go): carry the Config-given
		// guard thresholds, reset the observation state. A clone only happens
		// at an intent boundary -- the only moment these fields are not being
		// written -- where the watcher holds no in-flight run or quiet count
		// worth carrying, so a fresh watcher over the same thresholds is a
		// faithful copy.
		loop: newLivelockWatcherFromGuard(e.loop.guard),
		// setname.go's layer-3 rename table and its genesis-time gate. The
		// clone's board is identical at the clone boundary, so the table is
		// carried with its (epoch, version) key rather than rebuilt -- but as
		// a fresh slice, never the original's backing array, so the two
		// engines' next refreshes cannot write over each other. This is what
		// keeps a clone's name filters reading the CLONE's board once the two
		// diverge (setname_filter_scope_test.go).
		renames:       append([]effects.ObjectName(nil), e.renames...),
		renameEpoch:   e.renameEpoch,
		renameVersion: e.renameVersion,
		setNameInPool: e.setNameInPool,
		// layer4types.go's layer-4 derived-type table and its genesis-time
		// gate, carried with its (epoch, version) key for the same reason: the
		// clone's board is identical at the clone boundary, and a fresh slice
		// (never the original's backing array) keeps the two engines' next
		// refreshes from writing over each other, so a clone's type filters
		// read the CLONE's board once the two diverge.
		layer4Types:  append([]effects.ObjectTypes(nil), e.layer4Types...),
		typesEpoch:   e.typesEpoch,
		typesVersion: e.typesVersion,
		typesObjs:    e.typesObjs,
		layer4InPool: e.layer4InPool,
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
		c.resume = cloneResume(e.resume)
	}
	c.controlGrants = append([]controlGrant(nil), e.controlGrants...)
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
	if e.pendingTriggers != nil {
		c.pendingTriggers = clonePendingTriggers(e.pendingTriggers)
	}
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
	if e.phaseUnknownNoted != nil {
		c.phaseUnknownNoted = make(map[string]bool, len(e.phaseUnknownNoted))
		for k, v := range e.phaseUnknownNoted {
			c.phaseUnknownNoted[k] = v
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
		c.triggerGameFires = make(map[triggerKey]int32, len(e.triggerGameFires))
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
	// staticContinuous / staticEpoch are likewise deliberately NOT copied:
	// staticEffects rebuilds into the memo's reusable outer storage, so each
	// branch must own its backing array. The zero epoch forces a fresh scan
	// of the cloned board on its first active() rebuild. The static-control
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
		c.manaColorActivation = &ma
	}
	if e.manaDiscardActivation != nil {
		ma := *e.manaDiscardActivation
		ma.cost.Sac = append([]CostPart(nil), e.manaDiscardActivation.cost.Sac...)
		ma.cost.Discard = append([]CostPart(nil), e.manaDiscardActivation.cost.Discard...)
		ma.cost.SubCounter = append([]CostPart(nil), e.manaDiscardActivation.cost.SubCounter...)
		ma.cost.Exile = append([]CostPart(nil), e.manaDiscardActivation.cost.Exile...)
		ma.cost.Reveal = append([]CostPart(nil), e.manaDiscardActivation.cost.Reveal...)
		ma.cost.RevealOrChoose = append([]CostPart(nil), e.manaDiscardActivation.cost.RevealOrChoose...)
		ma.cost.RevealChosen = append([]CostPart(nil), e.manaDiscardActivation.cost.RevealChosen...)
		ma.cost.Behold = append([]CostPart(nil), e.manaDiscardActivation.cost.Behold...)
		ma.cost.TapPermanent = append([]CostPart(nil), e.manaDiscardActivation.cost.TapPermanent...)
		ma.cost.Blight = append([]CostPart(nil), e.manaDiscardActivation.cost.Blight...)
		ma.sacs = append([]state.ObjID(nil), e.manaDiscardActivation.sacs...)
		ma.discards = append([]state.ObjID(nil), e.manaDiscardActivation.discards...)
		ma.exiles = append([]state.ObjID(nil), e.manaDiscardActivation.exiles...)
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
		u.cost.Sac = append([]CostPart(nil), e.unlessPayment.cost.Sac...)
		u.cost.Discard = append([]CostPart(nil), e.unlessPayment.cost.Discard...)
		u.cost.SubCounter = append([]CostPart(nil), e.unlessPayment.cost.SubCounter...)
		u.cost.Draw = append([]CostPart(nil), e.unlessPayment.cost.Draw...)
		u.cost.Reveal = append([]CostPart(nil), e.unlessPayment.cost.Reveal...)
		u.cost.RevealOrChoose = append([]CostPart(nil), e.unlessPayment.cost.RevealOrChoose...)
		u.cost.RevealChosen = append([]CostPart(nil), e.unlessPayment.cost.RevealChosen...)
		u.sacs = append([]state.ObjID(nil), e.unlessPayment.sacs...)
		u.discards = append([]state.ObjID(nil), e.unlessPayment.discards...)
		u.reveals = append([]state.ObjID(nil), e.unlessPayment.reveals...)
		u.ctx = cloneUnlessCtx(e.unlessPayment.ctx)
		u.rp = cloneResume(e.unlessPayment.rp)
		c.unlessPayment = &u
	}
	if e.cumulative != nil {
		cu := *e.cumulative
		cu.amount.Sac = append([]CostPart(nil), e.cumulative.amount.Sac...)
		cu.amount.Discard = append([]CostPart(nil), e.cumulative.amount.Discard...)
		cu.amount.SubCounter = append([]CostPart(nil), e.cumulative.amount.SubCounter...)
		cu.amount.AddCounter = append([]CostPart(nil), e.cumulative.amount.AddCounter...)
		cu.amount.Exile = append([]CostPart(nil), e.cumulative.amount.Exile...)
		cu.amount.Reveal = append([]CostPart(nil), e.cumulative.amount.Reveal...)
		cu.amount.RevealOrChoose = append([]CostPart(nil), e.cumulative.amount.RevealOrChoose...)
		cu.amount.RevealChosen = append([]CostPart(nil), e.cumulative.amount.RevealChosen...)
		cu.amount.Behold = append([]CostPart(nil), e.cumulative.amount.Behold...)
		cu.amount.TapPermanent = append([]CostPart(nil), e.cumulative.amount.TapPermanent...)
		cu.amount.Blight = append([]CostPart(nil), e.cumulative.amount.Blight...)
		cu.amount.Hybrid = append([]ManaPair(nil), e.cumulative.amount.Hybrid...)
		cu.amount.Twobrid = append([]Twobrid(nil), e.cumulative.amount.Twobrid...)
		cu.amount.HybridPhyrexian = append([]HybridPhyrexian(nil), e.cumulative.amount.HybridPhyrexian...)
		cu.amount.Phyrexian = append([]byte(nil), e.cumulative.amount.Phyrexian...)
		cu.amount.Unknown = append([]string(nil), e.cumulative.amount.Unknown...)
		if e.cumulative.action != nil {
			action := *e.cumulative.action
			cu.action = &action
		}
		c.cumulative = &cu
	}
	if e.triggerCost != nil {
		tc := *e.triggerCost
		tc.resume = cloneResume(e.triggerCost.resume)
		tc.amount.Sac = append([]CostPart(nil), e.triggerCost.amount.Sac...)
		tc.amount.Discard = append([]CostPart(nil), e.triggerCost.amount.Discard...)
		tc.amount.SubCounter = append([]CostPart(nil), e.triggerCost.amount.SubCounter...)
		tc.amount.AddCounter = append([]CostPart(nil), e.triggerCost.amount.AddCounter...)
		tc.amount.Exile = append([]CostPart(nil), e.triggerCost.amount.Exile...)
		tc.amount.MoveToGrave = append([]CostPart(nil), e.triggerCost.amount.MoveToGrave...)
		tc.amount.Reveal = append([]CostPart(nil), e.triggerCost.amount.Reveal...)
		tc.amount.RevealOrChoose = append([]CostPart(nil), e.triggerCost.amount.RevealOrChoose...)
		tc.amount.RevealChosen = append([]CostPart(nil), e.triggerCost.amount.RevealChosen...)
		tc.amount.Behold = append([]CostPart(nil), e.triggerCost.amount.Behold...)
		tc.amount.TapPermanent = append([]CostPart(nil), e.triggerCost.amount.TapPermanent...)
		tc.amount.Blight = append([]CostPart(nil), e.triggerCost.amount.Blight...)
		tc.amount.Hybrid = append([]ManaPair(nil), e.triggerCost.amount.Hybrid...)
		tc.amount.Twobrid = append([]Twobrid(nil), e.triggerCost.amount.Twobrid...)
		tc.amount.HybridPhyrexian = append([]HybridPhyrexian(nil), e.triggerCost.amount.HybridPhyrexian...)
		tc.amount.Phyrexian = append([]byte(nil), e.triggerCost.amount.Phyrexian...)
		tc.amount.Unknown = append([]string(nil), e.triggerCost.amount.Unknown...)
		tc.sacs = append([]state.ObjID(nil), e.triggerCost.sacs...)
		tc.exiles = append([]state.ObjID(nil), e.triggerCost.exiles...)
		tc.moveGraves = append([]state.ObjID(nil), e.triggerCost.moveGraves...)
		c.triggerCost = &tc
	}
	if e.echo != nil {
		// kw:Echo (rules/echo.go): the same plain-value class as cumulative
		// above — the Cost's slice fields deep-copied so the clone owns them.
		ef := *e.echo
		ef.amount.Sac = append([]CostPart(nil), e.echo.amount.Sac...)
		ef.amount.Discard = append([]CostPart(nil), e.echo.amount.Discard...)
		ef.amount.SubCounter = append([]CostPart(nil), e.echo.amount.SubCounter...)
		ef.amount.AddCounter = append([]CostPart(nil), e.echo.amount.AddCounter...)
		ef.amount.Exile = append([]CostPart(nil), e.echo.amount.Exile...)
		ef.amount.Reveal = append([]CostPart(nil), e.echo.amount.Reveal...)
		ef.amount.RevealOrChoose = append([]CostPart(nil), e.echo.amount.RevealOrChoose...)
		ef.amount.RevealChosen = append([]CostPart(nil), e.echo.amount.RevealChosen...)
		ef.amount.Behold = append([]CostPart(nil), e.echo.amount.Behold...)
		ef.amount.TapPermanent = append([]CostPart(nil), e.echo.amount.TapPermanent...)
		ef.amount.Blight = append([]CostPart(nil), e.echo.amount.Blight...)
		ef.amount.Hybrid = append([]ManaPair(nil), e.echo.amount.Hybrid...)
		ef.amount.Twobrid = append([]Twobrid(nil), e.echo.amount.Twobrid...)
		ef.amount.HybridPhyrexian = append([]HybridPhyrexian(nil), e.echo.amount.HybridPhyrexian...)
		ef.amount.Phyrexian = append([]byte(nil), e.echo.amount.Phyrexian...)
		ef.amount.Unknown = append([]string(nil), e.echo.amount.Unknown...)
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
		pc.cost.Sac = append([]CostPart(nil), e.cast.cost.Sac...)
		pc.cost.Discard = append([]CostPart(nil), e.cast.cost.Discard...)
		pc.cost.SubCounter = append([]CostPart(nil), e.cast.cost.SubCounter...)
		pc.cost.Exile = append([]CostPart(nil), e.cast.cost.Exile...)
		pc.cost.ExileFromTop = append([]CostPart(nil), e.cast.cost.ExileFromTop...)
		pc.cost.MoveToGrave = append([]CostPart(nil), e.cast.cost.MoveToGrave...)
		pc.cost.Reveal = append([]CostPart(nil), e.cast.cost.Reveal...)
		pc.cost.RevealOrChoose = append([]CostPart(nil), e.cast.cost.RevealOrChoose...)
		pc.revealHandArm = append([]bool(nil), e.cast.revealHandArm...)
		pc.cost.RevealChosen = append([]CostPart(nil), e.cast.cost.RevealChosen...)
		pc.cost.Behold = append([]CostPart(nil), e.cast.cost.Behold...)
		pc.cost.TapPermanent = append([]CostPart(nil), e.cast.cost.TapPermanent...)
		pc.cost.Blight = append([]CostPart(nil), e.cast.cost.Blight...)
		pc.cost.Draw = append([]CostPart(nil), e.cast.cost.Draw...)
		pc.cost.LifeX = append([]CostPart(nil), e.cast.cost.LifeX...)
		pc.cost.Evidence = append([]CostPart(nil), e.cast.cost.Evidence...)
		pc.cost.DamageYou = append([]CostPart(nil), e.cast.cost.DamageYou...)
		pc.cost.Energy = append([]CostPart(nil), e.cast.cost.Energy...)
		pc.cost.Return = append([]CostPart(nil), e.cast.cost.Return...)
		pc.cost.PutToLib = append([]CostPart(nil), e.cast.cost.PutToLib...)
		pc.cost.Hybrid = append([]ManaPair(nil), e.cast.cost.Hybrid...)
		pc.cost.Phyrexian = append([]byte(nil), e.cast.cost.Phyrexian...)
		pc.cost.Twobrid = append([]Twobrid(nil), e.cast.cost.Twobrid...)
		pc.cost.HybridPhyrexian = append([]HybridPhyrexian(nil), e.cast.cost.HybridPhyrexian...)
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
			if rc.untap != nil {
				resume := *rc.untap
				rc.untap = &resume
			}
			c.replChoices[i] = rc
		}
	}
	c.madnessChoices = append([]events.Event(nil), e.madnessChoices...)
	c.madnessSuspended = e.madnessSuspended
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
	// askElection is a plain bool, carried by the value copy.
	return cr
}

// cloneResume deep-copies a suspended resolution's resume chain (fx34): each
// link is plain value data (kind/obj plus a *cards.SA into the shared,
// immutable corpus), but the outer continuation chain is a linked list this
// cloned engine must own so it can resume outward independently of the
// original's traversal.
func cloneResume(rp *resumePoint) *resumePoint {
	if rp == nil {
		return nil
	}
	cp := *rp
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
	cp.targetSpellLKI = effects.CloneTargetSpellLKI(rp.targetSpellLKI)
	cp.targetsUnique = append([]state.Target(nil), rp.targetsUnique...)
	// The VillainousChoice cursor and victim binding are sliced values the
	// resumed Ctx re-binds, so the clone owns its own copies instead of
	// sharing backing arrays with the original (the same discipline every
	// other slice here follows).
	cp.villainousVictims = append([]state.Target(nil), rp.villainousVictims...)
	cp.villainousRemembered = append([]state.Target(nil), rp.villainousRemembered...)
	// The multi-player GenericChoice chooser cursor is likewise a sliced value
	// the resumed Ctx re-binds; the clone owns its own copy.
	cp.genericChoosers = append([]state.Target(nil), rp.genericChoosers...)
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
	cp.deferredResume = cloneResume(rp.deferredResume)
	cp.outer = cloneResume(rp.outer)
	return &cp
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
	d.ResumeTargetsUnique = append([]state.Target(nil), p.ResumeTargetsUnique...)
	d.ResumeDigPrimary = append([]state.ObjID(nil), p.ResumeDigPrimary...)
	return &d
}
