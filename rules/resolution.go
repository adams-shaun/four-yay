// The mid-resolution ask: an effect that needs a player's decision in the
// middle of resolving the top of the stack (a nested Charm pick, or
// effCopySpellAbility's UnlessCost$ may-pay) poses it through
// effects.Host.Ask, and this file owns what suspends and what resumes.
//
// Suspension is a stack object staying put: resolveTop (rules/stack.go)
// runs the object's effects and, when e.resume is set after the pass,
// leaves the object on the stack and returns with a decision pending, so
// nothing before the ask gets a second chance to run and nothing moves
// until the answer arrives. Resumption re-runs the suspended sub-ability —
// the exact one that asked, never the chain prefix before it — with the
// answer attached to the Ctx, and then moves the fully-resolved object off
// the stack exactly as resolveTop's own tail would have.
//
// fx32's defect: an ask NESTED inside a mode (a Charm whose chosen mode is
// itself an asking primitive, and where the Charm — or that mode — carries
// its own SubAbility$ continuation) used to record ONE resume point and
// overwrite it at each nested ask, so the OUTER continuation the enclosing
// effect was still carrying was dropped. The resolution mechanism is now a
// chain of resume points: the innermost point holds the answer the player
// is being asked, and each point links an `outer` continuation that must
// run once everything inside it resolves. effects.Resolve reports each
// suspended loop through effects.Host.SuspendContinuation, and
// resumeResolution links those reports into the chain, so a nested ask
// finishes its own continuation AND then continues outward until the chain
// is empty — every suspended continuation runs, each exactly once.
//
// All resume state is plain value/pointer data (kind/obj plus *cards.SA
// into the shared, immutable compiled corpus, and the linked outer chain is
// rebuilt deterministically from the same stack object and the same
// recorded answers), never a closure, so Engine.Clone carries it like
// cast/choosing and a replay re-derives the same branch from the same
// recorded intent.
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// object stays on the stack; otherwise — once the re-entry and any outer
// continuation it carries have all completed — the fully-resolved object
// goes where resolveTop's own tail would have sent it.
func (e *Engine) resumeResolution(rp *resumePoint, chosen []decision.Option) {
	// A card-name answer is carried on the frame itself: every NameCard
	// resume reads ctx.NameChoice from rp.name, and binding it here is the
	// ONE home for that interpretation. Routing the answer through any
	// handler that consumes a resume point (handleChoose's general KChoose
	// arm, the off-stack mana rider's answerManaColor, a trigger-optional
	// re-entry) therefore all bind it identically; a path that skipped this
	// re-posed the same NameCard ask forever (the off-stack mana NameCard
	// livelock), because resumed NameCard reads an empty NameChoice and asks
	// again.
	if rp.kind == "name" && len(chosen) == 1 {
		rp.name = chosen[0].Label
	}
	if rp.kind == "turn_face_up_event" {
		prior := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(rp.event)
		e.applyingReplacement = prior
		e.resume = nil
		if rp.outer != nil {
			e.resumeResolution(rp.outer, nil)
		}
		return
	}
	if rp.kind == "deferred_ask" {
		// A deferred second ask (Engine.Ask): everything chained before it
		// has run, so pose it now and park on the resume point captured when
		// it was asked. Its answer continues at this frame's outer.
		inner := rp.deferredResume
		inner.outer = rp.outer
		e.lastDeferred = nil
		e.ask(rp.deferredAsk)
		e.resume = inner
		return
	}
	if rp.kind == "copy_targets" {
		e.resume = nil
		if rp.obj != 0 {
			// A multi-declaration copy (a fused half, a modal declaration) is
			// asked once per declaration. Each answer is ACCUMULATED here and
			// the whole flat list is recorded ONCE, after the LAST
			// declaration -- recording the first stage's answer immediately
			// would REPLACE o.Targets and destroy the later declarations'
			// inherited keep-current slots. copyTargetStage[obj] is the NEXT
			// stage, so the stage just answered is one less.
			stage := e.copyTargetStage[rp.obj] - 1
			if stage < 0 {
				stage = 0
			}
			if e.copyAnswerTargets == nil {
				e.copyAnswerTargets = make(map[state.ObjID][][]decision.Option)
			}
			ans := e.copyAnswerTargets[rp.obj]
			for len(ans) <= stage {
				ans = append(ans, nil)
			}
			ans[stage] = append(ans[stage][:0], chosen...)
			e.copyAnswerTargets[rp.obj] = ans
			// More declarations still owe an ask: re-enter resolveTop, whose
			// AskCopyTargets guard reads copyTargetStage and poses the next
			// one. Nothing is recorded yet.
			decls := e.copyTargetDeclarations(e.G.Obj(rp.obj))
			if e.copyTargetStage[rp.obj] < len(decls) {
				e.resolveTop()
				return
			}
			// Every declaration answered: record the flattened list in
			// declaration order (option 0 replaces, the rest append) and
			// publish the per-declaration split for a fused copy, so
			// resolveFused hands each half exactly the targets chosen FOR it
			// (the same split a cast publishes at payment).
			flat := make([]decision.Option, 0, len(chosen))
			stages := make([][]state.Target, len(ans))
			for i, sl := range ans {
				flat = append(flat, sl...)
				stages[i] = targetOptions(sl)
			}
			if len(flat) == 0 {
				flat = chosen
			}
			e.recordChosenTargets(rp.obj, flat, false)
			if o := e.G.Obj(rp.obj); o != nil && o.CastFlags&state.FlagFused != 0 {
				if ff, _ := fusedSplitFaces(o); ff != nil {
					if sa := ff.SpellAbility(); sa == nil || !effects.TargetsOf(sa).Targeted() {
						stages = append([][]state.Target{nil}, stages...)
					}
				}
				if e.fuseTargets == nil {
					e.fuseTargets = make(map[state.ObjID][][]state.Target)
				}
				e.fuseTargets[rp.obj] = stages
			}
			delete(e.copyAnswerTargets, rp.obj)
		}
		e.resolveTop()
		return
	}
	// A GainLife→Draw replacement body parked its remaining draws on this
	// ask (replacement.go's lifeReplacementDraw). The body is not a stack
	// resolution: there is no sub-ability to re-enter (rp.sa is nil -- the
	// loop called DrawFor directly, which poses its own dredge ask). The
	// signature is exact: DrawFor is the only sa==nil dredge asker (effDraw's
	// frames carry ResumeSA, and a turn-based draw's direct frame never
	// reaches here -- handleModes routes those to its own arm). The answer
	// resolved the draw that asked, whether or not draws remain parked
	// (lifeDraws == 0 is the FINAL draw of the body -- findings-sol5: the old
	// lifeDraws > 0 gate dropped that frame's answer into the no-sub-ability
	// Note below); apply it and re-drive the rest (which may park again on
	// the next dredge ask). The frame's rp.outer continuation and the
	// completion tail below still run after it -- the same order the body ran
	// in before it suspended -- and the drain at this cascade's true end
	// picks up any replacement-order queue the interrupted pass left behind.
	parkedDraws := false
	if rp.kind == "dredge" && rp.sa == nil {
		parkedDraws = true
		if !e.G.Players[rp.player].Lost {
			// CR 800.4f: a departed player makes no choice and draws
			// nothing; the outer continuation below still runs.
			if len(chosen) > 0 && chosen[0].Kind == "dredge" {
				e.applyDredge(rp.player, chosen[0].Obj)
			} else {
				e.resumeOrdinaryDraw(rp.player)
			}
			e.lifeReplacementDraw(rp.player, rp.lifeDraws)
			if e.Suspended() || e.pending != nil {
				// The re-drive parked on the next dredge ask: that frame's
				// own resume arms carry the rest. The new pending point is
				// fresh (outer nil -- Ask builds it bare, and nothing in this
				// re-drive reports continuations), so link the interrupted
				// frame's own continuation onto it -- the same fx32 linking
				// discipline the e.resume != nil branch below practises --
				// or the cascade's last frame would complete the object
				// without ever running what this interrupted resolution was
				// still carrying (findings-sol5: the sub-ability after the
				// GainLife never ran). Every later park re-links the same
				// chain, so rp.outer survives until the cascade truly ends.
				if e.resume != nil {
					e.resume.outer = rp.outer
				}
				return
			}
		}
	}
	before := e.triggerBefore
	e.triggerBefore = rp.before
	defer func() { e.triggerBefore = before }()
	savedResolving := e.resolvingObj
	e.resolvingObj = rp.obj
	defer func() { e.resolvingObj = savedResolving }()
	o := e.G.Obj(rp.obj)
	if o == nil || (o.Zone != state.ZStack && !rp.replacement && !rp.direct) {
		// The suspended object left the stack while the decision was
		// outstanding. Nothing but the answer can un-freeze the engine, so
		// this is unreachable in a well-formed match; it degrades to a
		// no-op rather than panicking, the same totality stance as every
		// other resolution exit.
		return
	}
	if rp.fuseAlt != nil {
		// A fuse-rest continuation (CR 702.101b): run the captured remaining
		// halves of the fused split spell, each with its own filtered target
		// slice (rules/split.go's runFusedHalves). A further suspension parks
		// below with the rest already chained; when the last half ran
		// unsuspended, this frame's shared completion tail runs -- the same
		// finishResumption + priority-reset shape every outermost frame takes.
		if cont, suspended := e.runFusedHalves(o, rp.fuseAlt.halves, rp.fuseAlt.sas, rp.fuseAlt.targets,
			rp.fuseAlt.from, rp.outer); suspended {
			if e.resume != nil {
				e.resume.outer = cont
			}
			return
		}
		if rp.outer != nil {
			e.resumeResolution(rp.outer, nil)
			return
		}
		e.finishResumption(rp.obj)
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
		return
	}
	// A chain can park between two pre-asked subs. Recheck the stored
	// answers against the live board before the resumed effects walk uses
	// them; the first pass in resolveTop already checked those reached earlier.
	var chainRoot *cards.SA
	if o.Ability != nil {
		chainRoot = o.Ability
		e.recheckCastSubTargets(rp.obj, o.Ability, o.Controller, o.Source)
	} else if f := o.Face(); f != nil {
		chainRoot = f.SpellAbility()
		e.recheckCastSubTargets(rp.obj, chainRoot, o.Controller, rp.obj)
	}
	// The list resolveTop resolved this object against (the overload census,
	// the CR 608.2b-filtered set), not the raw recorded Targets: a per-target
	// cursor carried across the ask indexes that list (rules/
	// resolution_targets.go).
	targets := e.resolutionTargets.flatFor(rp.obj, o.Targets)
	ctx := effects.NewCtxPtr(rp.obj, o.Controller, effects.CtxInit{Targets: targets, EffectFrame: rp.effectFrame})
	ctx.NameChoice, ctx.ChosenDirection = rp.name, rp.chosenDirection
	// Forge's Count$ResolvedThisTurn: a chain that suspended at a
	// mid-resolution ask and so re-enters HERE instead of through
	// resolveTop must keep the tally its first pass read. The Resolve event
	// was emitted once, so the tally is unchanged across the ask, but a
	// fresh Ctx defaults the field to zero and a gate would then read the
	// wrong ordinal (Sephiroth would transform a turn early on the
	// resumed pass). resolvedAbilityTally is the same read resolveTop's
	// ability branch makes, in one home.
	ctx.ResolvedThisTurn = e.resolvedAbilityTally(o)
	ctx.ClashContinuation = cloneClashResume(rp.clash)
	ctx.ActivationsThisTurn = e.activationsThisTurnFor(o.Source, o.Ability)
	// alltargeted1: a re-entered walk keeps consuming the cast flow's
	// pre-asked sub-ability target answers (kept until the stack object
	// leaves, so both a later sub and a suspended body can use theirs).
	// A modal root is excluded from the pre-ask whole
	// (collectSubTargetPreAsks), so this map is empty exactly where
	// ModeTargets carries the per-mode groups instead: the two bindings
	// are disjoint by construction, never competing for one chain.
	ctx.SubPreAsk = e.castSubTargets[rp.obj]
	ctx.AllTargets = e.chainTargetUnion(rp.obj, chainRoot, targets)
	ctx.ModeTargets = e.resolutionTargets.modesFor(rp.obj, e.charmTargets[rp.obj])
	ctx.Chosen, ctx.ChosenValid = append([]state.Target(nil), rp.choices...), rp.chosenValid
	ctx.DigUntilMove = rp.digUntilMove
	ctx.ClonePick, ctx.ClonePickDone = rp.clonePick, rp.clonePickDone
	ctx.VillainousVictims = append([]state.Target(nil), rp.villainousVictims...)
	ctx.VillainousIndex = rp.villainousIndex
	ctx.ChoiceTarget = rp.target
	// The pre-move controller snapshot of this resolution's object
	// targets, carried across the suspension: a resumed frame's Ctx is
	// rebuilt from the LIVE objects (whose controllers any completed
	// Destroy has already reset to their owners), so without this a
	// chained TokenOwner$ TargetedController sees the wrong seat. The
	// map is keyed by target ObjID, so a frame whose Targets are later
	// narrowed (a fused half's slice, a Charm mode's target) still
	// resolves the entries it names.
	ctx.TargetControllerLKI = effects.CloneTargetControllerLKI(rp.targetControllerLKI)
	ctx.TargetCountersLKI = effects.CloneTargetCountersLKI(rp.targetCountersLKI)
	ctx.TargetPTLKI = effects.CloneTargetPTLKI(rp.targetPTLKI)
	ctx.TargetSpellLKI = effects.CloneTargetSpellLKI(rp.targetSpellLKI)
	// The resolving stack-object wrapper, same anchor resolveTop's
	// branches set: a SUSPENDED-then-resumed ability (Ulalek's pay ask is
	// exactly such a suspension) keeps the ValidStack otherAbility
	// exclusion pointed at its own wrapper on re-entry. The replacement
	// arm below may rebind ctx.Source to the replacement's host;
	// ResolvingObj stays rp.obj -- the wrapper whose resolution this
	// frame is.
	ctx.ResolvingObj = rp.obj
	// The shared coin-flip memory the chain had at the ask. Re-attached so
	// a chained Defined$ FlippedTails / Wins reader keeps the flips
	// performed before the suspension (Goblin Assassin's per-loser
	// sacrifice asks once after the flips; without this the second loser's
	// read saw an empty set and never sacrificed).
	ctx.FlipMemory = rp.flipMemory
	// The shared ExchangeLife rider memory the chain had at the ask, the
	// same pointer-ride as FlipMemory above: a chained Count$
	// RememberedNumber reader (Mister Negative's SubAbility$ DBDraw) keeps
	// the value the exchange transaction settles, no matter how many Ctx
	// rebuilds the suspension's continuation chain goes through.
	ctx.ExchangeMemory = rp.exchangeMemory
	// The plural replaced-instruction batch (Ctx.ReplacedCards) follows the
	// same rule as the singular Replaced below: a resumed frame that carries
	// one restores it, so a Cascade body's hidden pick re-resolves
	// ReplacedCards.<qual> against the same exiled batch. Set unconditionally
	// when present, because a continuation frame of a replacement body's
	// resolution (the CascadeResidue SA) runs with rp.replacement false yet
	// still needs the batch.
	if len(rp.replacedCards) > 0 {
		ctx.ReplacedCards = append([]state.ObjID(nil), rp.replacedCards...)
	}
	// Publish this rebuilt Ctx for the whole of the resumed resolution (the
	// same restore-on-return bracket effects.Resolve uses), so an ask posed
	// from RULES machinery before the effects.Resolve re-entry -- the Ward
	// pay window's beginWardPayment, a charm-mode continuation -- carries the
	// chain's TargetUnique$ accumulator (restored from rp.targetsUnique
	// above) onto its own resume point instead of dropping it. The nested
	// effects.Resolve re-publishes the same Ctx and restores to this value on
	// return, so the LIFO restore order is exact.
	prevCtx := e.SetResolutionCtx(ctx)
	defer e.SetResolutionCtx(prevCtx)
	if rp.kind == "repeat_optional" {
		ctx.RepeatResume = &effects.RepeatContinuation{
			Continue: len(chosen) > 0 && chosen[0].Kind == "yes",
			Next:     rp.repeatOptionalNext,
		}
	}
	// Cost-sacrificed objects are engine-only LKI keyed by the stack object.
	// Re-entry must restore the same snapshot so an SVar such as Mausoleum
	// Wanderer's Sacrificed$CardPower does not collapse to zero after the
	// unless-pay answer suspends resolution.
	ctx.Sacrificed = e.sacrificedLKI[rp.obj]
	ctx.Exiled = e.castExiled[rp.obj]
	ctx.Revealed = e.castRevealed[rp.obj]
	// CR 107.3i: X is the value paid for the object's {X}, preserved on the
	// stack object by CastInfo -- the same binding resolveTop's spell and
	// ability branches now carry. A spell whose resolution suspends on a
	// mid-resolution ask (modes, discard, dig, unless-pay) keeps the paid
	// X for the rest of the walk instead of resuming with 0. Set here for
	// every resume; the replacement arm below has no other X to restore.
	// For a trigger that suspends, CR 107.3m's binding applies exactly as
	// resolveTop's ability branch does it: the trigger object was never
	// paid an X, so the causing event's card supplies the value.
	ctx.X = o.X
	// CR 601.2b/107.3i: the stack object's own X — an announced, possibly
	// zero payment — binds through the suspension exactly as the value does,
	// so an UnlessCost$ X on a zero-X cast resolves to {0} at the resumed
	// gate instead of staying a raw unpriceable token.
	ctx.XAnnounced = stackXAnnounced(o)
	if rp.replacement {
		// fx44: this suspended frame is a ReplaceWith$ body, so restore the
		// replacement context applyReplacements seeded for it. Ctx.Replaced is
		// the object the replaced event was about (ev.Obj, threaded via
		// rp.replaced), so a Defined$ ReplacedCard resolution still finds its
		// subject after the suspension. Without it the completed move (Mox
		// Diamond's MoveToBattlefield) targets nothing and the object never
		// leaves the stack. Ctx.Remembered is NOT re-seeded with the replaced
		// object: Remembered$Amount inside a replacement body counts the
		// body's own rider-remembered cards (Mox's discarded land, Scorched
		// Ruins' sacrificed two), never the replaced card itself — the same
		// empty-start rule replCtx (rules/replacement.go) documents. The
		// body's own Remembered from before the ask rides rp.remembered. The replacement arm has no other X to restore, so
		// triggerPaidX's fallback below is skipped for it.
		ctx.Replaced = rp.replaced
		ctx.ReplacementTarget = rp.replacementTarget
		ctx.ReplacementSource = rp.replacementSource
		ctx.ReplacementAmount = rp.replacementAmount
		// Remembered is NOT seeded with the replaced/damaged object: no corpus
		// replacement body (Damage ones included — measured, zero use
		// Remembered in a Damage body) reads it, and seeding it made every
		// Remembered$Amount gate or count inside a replacement one too high
		// (Mox Diamond's EQ0/EQ1 discard split broke). The body's own
		// rider-remembered cards ride rp.remembered below.
	} else if ctx.X == 0 {
		ctx.X = e.triggerPaidX(rp.obj, o)
	}
	// The triggered-cost window's dynamic tapXType<X/Spec> payment (the
	// Battlesphere/yotia shape): the election's tap count is the cost's
	// announced X, carried on the resume point. It wins over both reads above
	// -- the trigger object's own X is 0 and the source permanent's X is its
	// cast-time value, not this payment's.
	if rp.tapPaidX != 0 {
		ctx.X = rp.tapPaidX
		ctx.XAnnounced = true
	}
	// The trigger-cost window's X fold (the {X}/{PayLife<X>} announcement:
	// Elenda and Azor, Vizkopa Confessor, Necrodominance): the announced or
	// fixed value binds exactly like the dyn-tap count above, so the body's
	// Count$xPaid / NumCards$ X / TokenPower$ X reads this payment.
	if rp.winPaidX != 0 {
		ctx.X = rp.winPaidX
		ctx.XAnnounced = true
	}
	// The chain's roll publications at the ask (effects.RollRide), after
	// every X binding above: a publication named X is the resolution's X.
	ctx.ResumeRollRide(rp.rolls.ride)
	var svars map[string]string
	if o.Ability != nil {
		// A triggered or activated ability: mirror resolveTop's ability
		// branch — Source is the source permanent, Remembered carries what
		// the trigger captured, and the SVar table comes from that
		// permanent's face.
		ctx.Source = o.Source
		if !rp.replacement {
			ctx.TriggerContext = e.triggerContexts[rp.obj]
		}
		// The same seeds resolveTop's ability branch gives the first pass
		// (rules/resolution_ability_ctx.go): the Phase-trigger Remembered
		// rule, the grantor a granted ability's Defined$ OriginalHost names,
		// the ninjutsu defender, and the announced-X rule.
		ctx.Remembered = e.resolvingRemembered(o)
		ctx.Captured = o.Remembered
		ctx.Grantor = o.GrantedBy
		bindNinjutsuDefender(ctx, o)
		if !rp.replacement && abilityXAnnounced(o) {
			ctx.XAnnounced = true
		}
		reflexiveCaptured(ctx)
		if lki, ok := e.triggerLKI[rp.obj]; ok {
			ctx.LKI = lki.object
			ctx.LKIPower, ctx.LKIToughness, ctx.LKIPTValid =
				lki.power, lki.toughness, lki.ptValid
		}
		if link, ok := e.sourceLifelinkLKI[rp.obj]; ok {
			ctx.SourceLifelinkLKI = link
			ctx.SourceLifelinkLKIValid = true
		}
		if controller, ok := e.sourceControllerLKI[rp.obj]; ok {
			ctx.SourceControllerLKI = controller
			ctx.SourceControllerLKIValid = true
		}
		if lki := e.damageSourceLKI[rp.obj]; lki != nil {
			ctx.DamageSourceLKI = cloneDamageSourceLKI(lki)
		}
		// CR 603.3c: keep the placement-announced mode choice across the
		// suspension -- but only for a frame that re-enters the ability's own
		// root (rules/resume_modes.go). The switch below overrides it (with
		// the NESTED answer) only for a nested "modes" resume.
		ctx.Modes = resumeChosenModes(rp, o)
		if rp.kind == "villainous" {
			if rp.villainousChoice != "" {
				ctx.Modes = []string{rp.villainousChoice}
			}
			ctx.Remembered = append([]state.Target(nil), rp.remembered...)
		}
		// A frame of the chosen body of a VillainousChoice (or of a nested ask
		// IT posed): the victim is this body's Remembered, not the ability's
		// own trigger capture. Without this a nested ask's re-entry (DBSac's
		// sacrifice picker resolves Defined$ Remembered) rebuilds an empty
		// set and drops the answered sacrifice, and a multi-victim choice
		// stops after the first nested choice. It wins over the o.Ability
		// seed above deliberately -- that is the exact binding the choice
		// needs.
		if rp.villainousRememberedSet {
			ctx.Remembered = append([]state.Target(nil), rp.villainousRemembered...)
			ctx.Captured = append([]state.Target(nil), rp.villainousRemembered...)
		}
		// The same owning-face read resolveTop's ability branch makes: a
		// mutated pile's under-card ability (CR 702.140d) must resume on the
		// UNDER-CARD's SVar table, not the pile's top card's. An ordinary
		// trigger's owning face IS the top face, and an activated ability
		// matches no trigger and falls through to Face(), so both are
		// unchanged.
		// The same table resolveTop's first pass read: the owning face of a
		// pile's under-card ability, and above all a granted or delayed
		// trigger's recorded line table -- the GRANTOR's SVars, which the
		// recipient's face does not carry (spike S3: Ninja's Blades on a Hero
		// token resumed its discard with X undefined and drained 0).
		svars = e.abilityResolutionSVars(rp.obj, o)
	} else if f := o.Face(); f != nil {
		svars = f.SVars
	}
	if rp.replacement {
		// fx44: this suspended frame is a ReplaceWith$ body, so restore the
		// replacement context applyReplacements seeded for it. Ctx.Replaced is
		// the object the replaced event was about (ev.Obj, threaded via
		// rp.replaced), so a Defined$ ReplacedCard resolution and an
		// SVar:X Remembered$Amount gate find their subject after the
		// suspension. Without these the completed move (Mox Diamond's
		// MoveToBattlefield) targets nothing and the object never leaves the
		// stack. (Captured, not Remembered, carries the replaced object on
		// the non-draw path — the same empty-Remembered rule replCtx
		// (rules/replacement.go) documents.)
		ctx.Replaced = rp.replaced
		ctx.ReplacedPlayer = rp.replacedPlayer
		if rp.replacedPlayer.IsPlayer {
			// A Draw replacement body: the draw-er's binding is the whole
			// seed, and the body's own RememberDrawn$ records what it draws —
			// a MoveZone-shaped Remembered seed would pollute the reveal and
			// the discard condition with a stale would-be-drawn entry.
			ctx.Remembered, ctx.Captured = nil, nil
		} else {
			// Ctx.Remembered is NOT re-seeded with the replaced object: a
			// Remembered$Amount gate inside a replacement body counts the
			// body's own rider-remembered cards, never the replaced card
			// itself (seeding it made every such gate one too high).
			ctx.Captured = []state.Target{{Obj: rp.replaced}}
		}
		if rp.remembered != nil {
			ctx.Remembered = append([]state.Target(nil), rp.remembered...)
		}
		// The body's own Source is the replacement's host, which differs from
		// the resolving object when that object caused another permanent's
		// replacement (a reanimation spell and Mox Diamond's discard): the
		// body's choices and SVars belong to the host.
		if rs := rp.replSource; rs != 0 && rs != rp.obj {
			if src := e.G.Obj(rs); src != nil {
				ctx.Source, ctx.Controller, ctx.X = rs, src.Controller, src.X
				ctx.TriggerContext = effects.TriggerContext{}
				svars = nil
				if f := src.Face(); f != nil {
					svars = f.SVars
				}
			}
		}
	}
	if len(rp.villainousVictims) > 0 {
		ctx.VillainousVictims = append([]state.Target(nil), rp.villainousVictims...)
		ctx.VillainousIndex = rp.villainousIndex
	}
	// The multi-player GenericChoice chooser cursor: restored so the chosen
	// body runs for its chooser (Remembered below) and the remaining choosers
	// are still asked. An answered chooser's frame carries genericChoice and
	// the index of the NEXT chooser (advanced by the resume switch below); a
	// continuation frame carries no answer and resumes the loop as it stands.
	if len(rp.genericChoosers) > 0 {
		ctx.GenericChoosers = append([]state.Target(nil), rp.genericChoosers...)
		ctx.GenericChooserIndex = rp.genericChooserIndex
	}
	// The AmountFromVotes$ tally the loop read its per-iteration binding from:
	// the vote is a PRIOR chain link, so the fresh Ctx can only re-derive
	// "Votes" from this restored table. Nil stays nil (a vote that published
	// no tally keeps its unbound/zero read).
	if rp.voteCounts != nil {
		ctx.VoteCounts = cloneVoteCounts(rp.voteCounts)
	}
	if rp.loopBound {
		ctx.Remembered = append([]state.Target(nil), rp.loopRemembered...)
		if rp.repeatSubject != (state.Target{}) {
			ctx.RepeatSubject = rp.repeatSubject
		}
		// Re-bind this iteration's per-subject tally on the rebuilt Ctx, the
		// same scalar effRepeatEach writes before resolving a first-pass body:
		// the resumed body reads "Votes" through runtimePublished, which the
		// restored table alone does not serve. A subject with no tally binds 0
		// (Forge's unset VoteNum read), exactly as the first pass does.
		if rp.voteCounts != nil {
			ctx.VotePublished = 0
			if n, ok := effects.VoteCountForTarget(ctx.VoteCounts, rp.repeatSubject); ok {
				ctx.VotePublished = int32(n)
			}
			ctx.VotePublishedSet = true
		}
	}
	// A mid-resolution ask that rode the walk's Remembered (the hidden-library
	// search sets ResumeRemembered -- a cast spell's Remembered lives only in
	// the resolving Ctx frame, so without the ride the resume rebuilds an
	// empty set and the re-entered primitive's eligibility recheck and the
	// chain's later sub-abilities see nothing, and Card.IsRemembered /
	// Defined$ Remembered in a chained hidden-origin ChangeZone would see an
	// empty list on re-entry too). The loop and replacement branches above
	// are authoritative when they fire; this applies only to the ordinary
	// frames, which never carry rp.remembered otherwise.
	if rp.remembered != nil && !rp.replacement && !rp.loopBound {
		ctx.Remembered = append([]state.Target(nil), rp.remembered...)
	}
	// The search chain's known-card set (Decision.ResumeSearchKnown): a
	// planted placement leg's answer rebuilds a fresh Ctx, and the NEXT leg of
	// the same chain must still label its options with the names the chooser
	// already learned. Runtime continuation state of the search walk, the same
	// class as rp.remembered above.
	if rp.searchKnown != nil {
		ctx.SearchKnown = append([]state.Target(nil), rp.searchKnown...)
	}
	ctx.ForgetOtherSnapshot = append([]state.Target(nil), rp.forgetOtherSnapshot...)
	ctx.ForgetOtherOwners = append([]state.PlayerID(nil), rp.forgetOtherOwners...)
	ctx.ForgetOtherReady, ctx.ForgetOtherCleared = rp.forgetOther.ready, rp.forgetOther.cleared
	// The TargetUnique$ accumulator, captured at ask time: the resumed Ctx
	// re-binds it so a LATER TargetUnique$ rider in the same chain still
	// excludes the targets earlier riders chose (a fresh Ctx would otherwise
	// rebuild the accumulator empty). ctx.Targets itself re-binds from the
	// stack object's flat list above, so the parent-target half of the
	// exclusion set survives the suspension untouched.
	if len(rp.targetsUnique) > 0 {
		ctx.TargetsUnique = append(ctx.TargetsUnique, rp.targetsUnique...)
	}
	// The walk's parent-link record, captured at ask time from the live Resolve
	// Ctx (effects/parent_targets.go). The resumed Ctx is rebuilt from the
	// stack object's flat targets, so without this ride a later untargeted
	// link's ParentTarget/ParentTargeted would fall back to Ctx.Targets -- the
	// ROOT's targets -- losing the NEAREST targeting ancestor the record was
	// introduced to name. The in-walk answer a body consumed itself
	// (linkAnswer/linkAnswered) rides with it, because its recordParentLink
	// has not run at ask time.
	ctx.ResumeParentLinks(rp.parentLinks, rp.linkAnswer, rp.linkAnswered)
	// The DamageMap$ True mark set, captured at ask time from the live
	// resolution Ctx: a chain that marks damage and then suspends on a
	// mid-resolution ask before its DB$ DamageResolve re-enters here, and the
	// rebuilt Ctx must still carry the marks the flush is owed (a fresh Ctx
	// would drop them and the flush would silently deal nothing).
	if len(rp.pendingDamage) > 0 {
		ctx.PendingDamage = effects.ClonePendingDamage(rp.pendingDamage)
	}
	// Task mvts1: carry the SA whose targeting the placement/announcement
	// ask covered, exactly as resolveTop's first pass does. An optional
	// trigger's yes (Kor Outfitter) re-enters through here, and without
	// this the re-entered ROOT would re-pose its placement target ask
	// under the generic ValidTgts$ pre-ask.
	if offeredSA := offeredTargetSA(o, svars); offeredSA != nil {
		ctx.OfferedSA = offeredSA
	}
	// A per-mode Charm's mode suspended on its own mid-resolution ask. The
	// generic bind above gave ctx.Targets the stack object's whole flat list
	// -- every selected mode's targets -- so the resumed mode would walk all
	// of them. Re-bind the narrowed group Ask captured, and restore the mode
	// SA as the covered targeting so the pre-ask does not re-pose it.
	if len(rp.charmModeScope) > 0 {
		ctx.Targets = append([]state.Target(nil), rp.charmModeScope...)
		ctx.CharmModeScope = ctx.Targets
		ctx.CharmModeSA = rp.charmModeSA
		if rp.charmModeSA != nil {
			ctx.OfferedSA = rp.charmModeSA
			ctx.TargetsOffered = true
		}
	}
	// A fused half's own mid-resolution ask re-enters here. The generic ctx
	// above binds Targets from the stack object's WHOLE flat target list, and
	// OfferedSA from the front face alone -- for a fused spell the flat list
	// carries BOTH halves' targets and the front face's SA is the other
	// half's, so any frame of the re-entered half would read the other half's
	// targets: a half ROOT re-posing its own ValidTgts$ pre-ask (the spurious
	// "Choose target" after the answered sacrifice, Far // Away) and, just as
	// wrong, a half's SUB-ABILITY reading ParentTargeted$CardPower off the
	// flat list (Flesh // Blood's DBPutCounter counting the sum of both
	// halves' chosen targets). Ask captured the resolving half's own
	// rechecked slice onto this frame (Engine.fusedResolving), so bind it as
	// Targets for EVERY frame of the half. When rp.sa is the half's ROOT its
	// targeting WAS covered by the cast's stage ask, so mark it offered and
	// skip the pre-ask; a sub-ability's targeting was never covered, so its
	// own pre-ask still fires against the half's slice as its parent list.
	if rp.fusedTargetsSet {
		ctx.Targets = rp.fusedTargets
		// The half's OWN SVar table: a fused spell keeps FaceIdx 0, so the
		// generic svars above is the front half's; the re-entered alternate
		// half's sub must resolve its own SVars (Blood's Y).
		svars = rp.fusedSVars
		if _, isRoot := fusedHalfRoot(o, rp.sa); isRoot {
			ctx.OfferedSA = rp.sa
			ctx.TargetsOffered = true
		}
	}
	effects.SetSVars(ctx, svars)
	// The chain's published resolution-scoped bindings (ExcessSVar$) survive
	// the suspension: the face table above does not carry them.
	effects.RebindPublishedSVars(ctx, rp.publishedSVars)
	// An accepted optional trigger may itself carry Cost$ (Mana Vault's
	// "you may pay {4}; if you do" untap). The optional answer chooses to
	// attempt the effect; payment is a separate resolution-time window with
	// mana-ability opportunities. Direct mandatory triggers enter the same
	// window from resolveTop.
	//
	// A Draw-bearing Cost$ joins the two named shapes (Hordewing Skaab's
	// OptionalDecider$ on "you may draw cards ... If you do, discard that
	// many"): the yes answer re-enters here and pays the draw through the
	// same window, rather than running the body for free.
	//
	// trigcost1: the shape test is the shared broadened gate
	// (triggerBodyNeedsCostWindow -- any non-Mandatory Cost$ except Mana /
	// CopySpellAbility), so a Kalastria Highborn `Cost$ B` pays through this
	// arm instead of executing free. CopySpellAbility keeps its own
	// event-role disjunct below.
	tc := e.triggerContexts[rp.obj]
	armed := rp.kind == "optional" && rp.sa != nil &&
		(e.triggerBodyNeedsCostWindow(rp.sa) ||
			// abcopy1: an OptionalDecider$ copy trigger's AB$ CopySpellAbility
			// with a real Cost$ (Rings of Brighthearth, Battlemages' Bracers,
			// Mirari) pays through the same window whenever the trigger context
			// carries an event role -- the activation role TriggerAbility, or
			// the spell-cast arm's TriggerCard (a SpellCast fires on PutOnStack,
			// whose Obj IS the cast spell; no ability wrapper is minted). Only
			// a context-less synthetic push keeps the free-executor semantics.
			(rp.sa.API == "CopySpellAbility" && rp.sa.ParamStr(cards.PKCost) != "" &&
				(tc.TriggerAbility != 0 || tc.TriggerCard != 0)))
	if armed {
		e.startTriggeredEffectCost(rp, ctx.Source)
		return
	}
	if rp.sa != nil {
		// CR 608.2g: an Amount$ Play whose earlier cast parked on its own
		// question left the rest of its chosen cards queued. The resolution
		// waits for those casts: its continuation (the re-entered Play and
		// everything after it) parks on the queue and startQueuedPlay
		// resumes it once the last chosen card's cast is complete, so a
		// chained "put the cards that weren't cast into your graveyard"
		// (Epic Experiment) never buries a card still to be cast.
		if rp.kind == "play" && e.queuedPlays != nil && e.queuedPlays.cont == nil {
			cont := cloneResume(rp)
			cont.kind = "play_resume"
			cont.playFirst = ctx.Play
			e.queuedPlays.cont = cont
			return
		}
		src := rp.obj
		if o.Ability != nil {
			src = o.Source
		}
		if rp.replacement && rp.replSource != 0 {
			src = rp.replSource
		}
		e.damaging = src
		// A fresh re-entry: reset the enclosing-loop continuation reports the
		// loops of THIS effects.Resolve call will accumulate. This reset is
		// safe even though resumeResolution recurses through rp.outer below:
		// an enclosing frame can only reach that recursion after its own
		// effects.Resolve returned with NO nested ask (e.resume == nil, else
		// the nested-ask branch above consumes contChain into e.resume.outer
		// and returns without recursing), at which point its contChain was
		// never needed again — so a deeper frame's reset discards only
		// reports no live frame still needs (measured: the full rules suite
		// runs no path where a recursive reset clobbers a needed report).
		e.contChain = e.contChain[:0]
		e.repeatReported = nil
		// fx44: restore the replacement context the suspended body was
		// resolving under. applyReplacements reset e.applyingReplacement to
		// false when the body suspended, so without this the resumed body's
		// own completion move is re-intercepted by the same replacement it is
		// the product of — the re-asked discard loop. It is saved and restored
		// (not just set) so a resume that reaches here already inside a
		// replacement keeps the outer context intact, exactly the discipline
		// ensureLeftTheStack and applyReplacements already practise.
		savedReplacement := e.applyingReplacement
		e.applyingReplacement = rp.replacement
		e.replReplaced, e.replAction, e.replReplacedPlayer = rp.replaced, rp.action, rp.replacedPlayer
		savedRedirect := e.replRedirect
		e.replRedirect = rp.redirect
		// The body's own re-entry (Host.SuspendUnless): the gate of THIS SA
		// had already resolved when the body posed the pending ask, so the
		// recorded marker re-enters it as an already-resolved answer — the
		// unless gate consumes it and never re-poses the pay ask (the
		// asking-body-under-UnlessCost$ livelock fix). A unless_pay resume
		// point never carries the marker: that ask was posed by the gate
		// itself, before any body ran. The arm's own authoritative answer
		// (the Ward arm's beginWardPayment outcome, the generic arm's
		// payUnlessCost outcome) wins over the marker — the gate-posed ask's
		// own suspension also records a marker (the SA carries UnlessCost$),
		// and letting it clobber the arm's answer counted a PAID ward as a
		// decline (the Kitesail Larcenist regression).
		if rp.unlessResolved != "" && ctx.UnlessPay == "" {
			ctx.UnlessPay = rp.unlessResolved
		}
		// Cross-mode TargetUnique attribution (the family runner effCharm's
		// charmCrossModeRun): a frame whose SA is (or is inside) one of the
		// chosen target-bearing modes' chains re-enters with that mode's OWN
		// target, not the stack object's undivided list — the split the
		// initial pass applied does not survive this ctx rebuild, so it is
		// re-derived here from the same deterministic inputs (ChosenModes,
		// Targets, the face's SVar table). The match is by the SA's Line (the
		// SVar body text parseSA stores): ResolveSVar parses fresh on every
		// call, so pointer identity never holds across a resume.
		if one := e.charmModeTarget(rp.obj, rp.sa); one != nil {
			ctx.Targets = one
		}
		// A MoveCounter resolution whose EARLIER rounds already answered the
		// target pre-ask, the kind pick or the amount pick re-seeds them here:
		// without this the fresh Ctx re-poses the target pre-ask on the way
		// back into the body, which re-poses the kind/amount ask, forever
		// (the movecounter1 livelock).
		if rp.sa.API == "MoveCounter" {
			e.seedMoveCounterAsk(rp.obj, ctx)
		}
		if rp.sa.API == "AddOrRemoveCounter" {
			e.seedAorAsk(rp.obj, ctx)
		}
		if rp.sa.API == "PutCounter" {
			e.seedCounterTypeAsk(rp.obj, rp.sa, ctx)
		}
		// The general form of the three seeds above: an answered generic
		// ValidTgts$ pre-ask for THIS SA, recorded by the "tgts" arm on an
		// earlier round of this same resolution. Runs last so a cursor one of
		// the API-specific seeds already set (MoveCounter writes both) wins,
		// and it never overwrites the current round's own answer.
		e.seedTargetsPick(rp.obj, rp.sa, ctx)
		// A frame of a fused half's resolution re-enters here: restore the
		// half's own target binding as the AMBIENT resolving target for the
		// whole of this re-entry (its root or sub-ability, and every frame
		// reachable through it), so a further nested ask posed below captures
		// the same half slice rather than the flat list. buildContinuationChain
		// below runs while it is still set, so the frames it stamps inherit it
		// too. Saved/restored like the replacement context just above: a
		// re-entry nested inside another fused half (never in the corpus, but
		// structural) keeps the outer binding intact.
		savedFused, savedFusedSet := e.fusedResolving, e.fusedResolvingSet
		savedSVars := e.fusedResolvingSVars
		savedWinX := e.windowPaidX
		e.fusedResolving, e.fusedResolvingSet = rp.fusedTargets, rp.fusedTargetsSet
		e.fusedResolvingSVars = rp.fusedSVars
		e.windowPaidX = rp.winPaidX
		// The VillainousChoice victim of the body this frame is resolving,
		// published as ambient state for the same reason and by the same
		// discipline as fusedResolving above: a nested ask the body poses (or
		// one a later frame in its chain poses) captures it through Ask, so
		// the nested re-entry still reads Defined$ Remembered as the victim.
		// A villainous frame's own victim is rp.remembered (the modes answer
		// recorded it); every other frame carries what its ask captured.
		savedVill, savedVillSet := e.villainousRemembered, e.villainousRememberedSet
		if rp.kind == "villainous" {
			e.villainousRemembered, e.villainousRememberedSet = rp.remembered, len(rp.remembered) > 0
		} else {
			e.villainousRemembered, e.villainousRememberedSet = rp.villainousRemembered, rp.villainousRememberedSet
		}
		// Restore only when this whole re-entry (and every rp.outer
		// continuation it recurses into) has finished: buildContinuationChain
		// in the nested-ask branch below stamps frames that must inherit the
		// same half binding, and an rp.outer recursion saves/restores its own
		// copy on top, so the deferred restore lands the original back.
		defer func() {
			e.fusedResolving, e.fusedResolvingSet, e.fusedResolvingSVars = savedFused, savedFusedSet, savedSVars
			e.windowPaidX = savedWinX
			e.villainousRemembered, e.villainousRememberedSet = savedVill, savedVillSet
		}()
		if resumeGatePassed(rp) {
			ctx.ResumedGatePassed = rp.sa
		}
		e.contChainOwners++
		effects.Resolve(e, ctx, rp.sa)
		e.contChainOwners--
		e.replReplaced, e.replAction, e.replReplacedPlayer = 0, "", state.Target{}
		e.replRedirect = savedRedirect
		e.applyingReplacement = savedReplacement
		e.damaging = 0
		// A resumed EndTurn has already exiled the stack, including the
		// resolving ability. Do not continue its Sub chain or grant priority
		// in the skipped step; enter cleanup just as resolveTop does.
		if e.endTurnRequested {
			e.finishEndTurn()
			// The pass branch's CR 117.3b reset marker, exactly as an
			// unsuspended EndTurn resolution gets it from handlePriority
			// once resolveTop returns: a resolution that suspended (an
			// answered Optional$ EndTurn) and then ended the turn reaches
			// its true end here, and skipping the completion tail below
			// must not also skip the marker the two paths share.
			e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
			return
		}
		// A resolution is still suspended when EITHER the ordinary
		// mid-resolution ask (e.resume) or an off-stack mana rider ask is
		// pending. The latter parks on the mana activation and sets
		// e.choosing == chooseManaColor instead of e.resume
		// (mana_activation.go's askOffStackMana), so the per-resolution answer
		// caches below must test BOTH or the next re-entry of this same SA
		// re-poses an already-answered ask -- a targeted rider that then asks
		// another question (Witch-Engine-shaped DB$ NameCard | ValidTgts$
		// Opponent) alternated tgts/name forever because the target record was
		// dropped while the name ask was still pending.
		parked := e.resume != nil || e.choosing == chooseManaColor
		if rp.sa.API == "MoveCounter" && !parked {
			// The MoveCounter resolution completed this round (nothing
			// suspended): its pending state is spent.
			delete(e.moveCounterAsk, rp.obj)
		}
		if rp.sa.API == "AddOrRemoveCounter" && !parked {
			// The AddOrRemoveCounter resolution completed this round (nothing
			// suspended): its pending state is spent -- delete it so a stale
			// entry can never seed a later resolution of the same object (the
			// moveCounterAsk discipline).
			delete(e.aorAsk, rp.obj)
		}
		if rp.sa.API == "PutCounter" && !parked {
			delete(e.counterTypeAsk, rp.obj)
		}
		if !parked {
			// This SA's resolution completed this round (nothing suspended),
			// so its recorded pre-ask answer is spent -- drop it so a later
			// re-entry of the same body asks afresh.
			e.forgetTargetsPick(rp.obj, rp.sa)
		}
		if e.resume != nil {
			// The re-entry posed a nested mid-resolution ask. The new
			// pending point (e.resume) has no outer yet: it must, once its
			// own answer is applied, continue at the rest of THIS re-entry's
			// chain (the loops effects.Resolve reported via
			// SuspendContinuation this pass) and then at rp.outer — the
			// continuation the frame we were re-entering was itself carrying.
			// Linking them now means the nested ask, when answered, resumes
			// every suspended continuation rather than dropping the outer
			// ones (fx32).
			if rp.loopBound {
				// Still inside the loop iteration this frame resumed: whatever
				// suspended at this level continues with its Remembered.
				e.bindLoopFrames(ctx.Remembered, ctx.VoteCounts, ctx.RepeatSubject)
			}
			e.resume.outer = e.buildContinuationChain(e.contChain, rp.obj, rp.outer)
			// The continuation chain now owns the reported frames. Keep this
			// per-pass scratch empty at the suspension boundary.
			e.contChain = e.contChain[:0]
			return
		}
		e.contChain = e.contChain[:0]
	} else if !parkedDraws && rp.kind != "replacement" && rp.kind != "etb" && rp.fuseAlt == nil {
		// A resume with no sub-ability recorded: normally reachable only from
		// a hand-built Ask (every real asking primitive sets ResumeSA). Three
		// deliberate exceptions need no Note either: a parked GainLife→Draw
		// frame whose answer was applied above, an as-enters entry choice
		// (resumeETBEntry has already re-emitted the entry this frame was
		// parked on, so the continuation likewise begins at rp.outer), and a
		// replacement-order
		// decision — the intercepted event has already completed, and the
		// continuation begins at rp.outer rather than re-running the effect
		// that proposed it. The resolution still finishes — the object leaves
		// the stack with no effect, the same degrade-to-nothing stance as an
		// unrecognised choice, rather than stalling the match forever.
		e.emit(events.Event{Kind: events.Note, Obj: rp.obj,
			Text: "mid-resolution answer resumed with no sub-ability recorded"})
	}
	if rp.replacement && o.Zone != state.ZStack {
		// An Updated ETB replacement has already completed the spell's move.
		// Its answer resumes only the replacement body; there is no stack
		// object to finish or priority round to create here.
		//
		// The suspended body may equally have been a fully-replaced entry that
		// never puts the land on the battlefield: this re-entry posed no new
		// ask (the nested-ask branch above returns), so the entry is done
		// either way and the land play settles here.
		e.settleLandPlayIfDone(rp.replaced)
		if rp.outer != nil {
			// The body interrupted a stack resolution whose frame was chained
			// behind this one (settleReplacementQueue: a replacement-order
			// answer whose chosen body asked, e.g. a shock land's UnlessCost
			// under a mass return). The body is done; the interrupted
			// resolution continues -- dropping it here left the resolving
			// object on the stack for resolveTop to re-resolve from the top.
			e.resumeResolution(rp.outer, nil)
			return
		}
		if rp.ownResolution {
			// The entering permanent WAS the resolving spell (Banner of
			// Kinship's "as this enters, choose a creature type"): its
			// resolution suspended on this body's ask, so handlePriority
			// deferred the CR 117.3b reset to here, the resolution's true end.
			// Without it the next priority round went to whoever held
			// priority when the spell began resolving -- the non-active
			// player who passed last -- with that pass still counted.
			e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
		}
		return
	}
	if rp.outer != nil { // No nested ask this pass and the frame itself completed: continue
		// outward through the runner-up continuations this frame carried.
		if rp.loopBound {
			// Hand this frame's Remembered to the next frame. A loop frame
			// folds in what the finished iteration remembered. Any other next
			// frame runs at this frame's level -- the rest of the same
			// iteration, or (after a loop frame) the rest of the chain that
			// enclosed the loop -- and takes it as is, so what the loop
			// remembered is not lost to the stack object's stale Remembered.
			handOnRemembered(rp.outer, ctx.Remembered)
		} else if next := rp.outer; next.inheritsRemembered && rp.handsOnChainRemembered() {
			// The next frame's loop walked this frame's Ctx on the pass that
			// suspended, so it continues with the Remembered this frame
			// finished with -- what the chain remembered before the ask and
			// what the answered re-entry added -- not the stack object's
			// (spike S3's Remembered-across-a-suspension class).
			next.remembered = append([]state.Target(nil), ctx.Remembered...)
			if next.remembered == nil {
				// An EMPTY handoff is still a handoff: the next frame must see
				// the cleared set this frame finished with (a Repeat loop
				// frame's RepeatDefined$ Remembered gate after Cultivator
				// Colossus's declined pick; a plain frame after a Cleanup
				// ClearRemembered$), not fall back to the stack object's
				// stale one.
				next.remembered = []state.Target{}
			}
		}
		if parkedDraws {
			// CR 608.2c: a resolution's SubAbility$ continuation runs only after
			// the effect that named it — including every replacement application
			// — completes. An exchange transaction the parked draw interrupted
			// (Lich's GainLife→Draw body under Mister Negative's exchange) is
			// still pending here, and rp.outer is that exchange's OWN SubAbility$
			// continuation: settle the transaction FIRST, or the rider reads the
			// transaction's pre-settle state (RememberOwnLoss evaluated to 0
			// because the controller's loss had not been finalised yet). A settle
			// that itself suspends re-parks the transaction (the existing drain
			// contract); the continuation below then runs as before.
			e.settlePendingLifeExchange()
		}
		e.resumeResolution(rp.outer, nil)
		if parkedDraws {
			e.askNextReplacementChoice()
		}
		return
	}
	e.finishResumption(rp.obj)
	// CR 117.3b / the counterpart of handlePriority's pass-branch grant: when
	// a SUSPENDED resolution completes (this is the outermost frame -- a
	// nested ask returns in the e.resume != nil branch above, and an outer
	// continuation recurses before reaching this line, so a resolution that
	// suspends more than once still reaches here exactly once), the pass
	// count resets and priority returns to the active player. The now-
	// suppressed unconditional emit in handlePriority's pass branch used to
	// log this at suspension time while the engine was parked on a
	// mid-resolution question, i.e. while nobody had priority; this emit puts
	// the reset and the "back to active" marker at the resolution's true end.
	// The answering Submit's own tail then grants the next priority round (its
	// grantPriority reads the passes this emit has just reset to zero), which
	// is the same two-event shape an unsuspended resolution already produces
	// (pass-branch grant + grantPriority), so the suspended path now matches.
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
	if parkedDraws {
		// The cascade's true end: any replacement-order choice the
		// interrupted pass left queued is asked now, after the resolution's
		// completion marker, never before it.
		e.askNextReplacementChoice()
	}
}

// resumeGatePassed reports whether rp re-enters an SA that already passed its
// Condition* gate on the pass that suspended: the SA that asked (every frame
// Engine.Ask built) and a rest frame re-entering its own loop SA. A plain
// continuation frame (kind "") re-enters a sub the walk has not reached yet,
// and the frames that re-enter a trigger's root before it ever resolved (the
// CR 603.5 optional yes/no, a trigger-cost window, madness, a deferred ask
// or turn-up event, a copy's target ask) have passed nothing.
func resumeGatePassed(rp *resumePoint) bool {
	k := rp.kind
	return rp.sa != nil && k != "" && k != "optional" && k != "effect_cost" && k != "effect_paid" &&
		k != "madness" && k != "deferred_ask" && k != "turn_face_up_event" && k != "copy_targets"
}
