// Re-entry of a resolution body after a rules-side payment window. A
// resolution that runs on the W3 kernel (rules/resolve) never suspends: every
// ask inside it is answered in place from the intent tape. What remains of the
// old suspend/resume machinery is the body re-entry three flows need after
// they have settled their own window in line: a triggered ability's Cost$
// window (kind "effect_paid"), an optional trigger's yes (kind "optional",
// CR 603.5) and a copy's new-target election (kind "copy_targets").
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// resumeResolution re-enters the body of the stack object rp names and then
// completes the resolution: it rebuilds the Ctx resolveTop built for the
// object on its first pass (Source/Controller/Targets/Remembered and the SVar
// table are all re-derivable from the stack object, which has not moved),
// re-runs rp's ability through effects.Resolve, and finally moves the
// fully-resolved object off the stack as resolveTop's own tail would have.
func (e *Engine) resumeResolution(rp *resumePoint, chosen []decision.Option) {
	if rp.kind == "copy_targets" {
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
	savedResolving := e.resolvingObj
	e.resolvingObj = rp.obj
	defer func() { e.resolvingObj = savedResolving }()
	o := e.G.Obj(rp.obj)
	if o == nil || o.Zone != state.ZStack {
		// The object left the stack while its window was open: a no-op, the
		// same totality stance as every other resolution exit.
		return
	}
	var chainRoot *cards.SA
	if o.Ability != nil {
		chainRoot = o.Ability
		e.recheckCastSubTargets(rp.obj, o.Ability, o.Controller, o.Source)
	} else if f := o.Face(); f != nil {
		chainRoot = f.SpellAbility()
		e.recheckCastSubTargets(rp.obj, chainRoot, o.Controller, rp.obj)
	}
	// The list resolveTop resolved this object against (the overload census,
	// the CR 608.2b-filtered set), not the raw recorded Targets.
	targets := e.resolutionTargets.flatFor(rp.obj, o.Targets)
	ctx := effects.NewCtxPtr(rp.obj, o.Controller, effects.CtxInit{Targets: targets})
	// Forge's Count$ResolvedThisTurn: the Resolve event was emitted once, so
	// the tally is unchanged, but a fresh Ctx defaults the field to zero.
	ctx.ResolvedThisTurn = e.resolvedAbilityTally(o)
	ctx.ActivationsThisTurn = e.activationsThisTurnFor(o.Source, o.Ability)
	// alltargeted1: the cast flow's pre-asked sub-ability target answers.
	ctx.SubPreAsk = e.castSubTargets[rp.obj]
	ctx.AllTargets = e.chainTargetUnion(rp.obj, chainRoot, targets)
	ctx.ModeTargets = e.resolutionTargets.modesFor(rp.obj, e.charmTargets[rp.obj])
	// The resolving stack-object wrapper, same anchor resolveTop's branches
	// set (the ValidStack otherAbility exclusion).
	ctx.ResolvingObj = rp.obj
	// Publish this rebuilt Ctx for the whole of the resumed resolution (the
	// same restore-on-return bracket effects.Resolve uses).
	prevCtx := e.SetResolutionCtx(ctx)
	defer e.SetResolutionCtx(prevCtx)
	// Cost-sacrificed objects are engine-only LKI keyed by the stack object.
	ctx.Sacrificed = e.sacrificedLKI[rp.obj]
	ctx.Exiled = e.castExiled[rp.obj]
	ctx.Revealed = e.castRevealed[rp.obj]
	// CR 107.3i: X is the value paid for the object's {X}, preserved on the
	// stack object by CastInfo; CR 107.3m binds a trigger's X to the causing
	// event's card.
	ctx.X = o.X
	ctx.XAnnounced = stackXAnnounced(o)
	if ctx.X == 0 {
		ctx.X = e.triggerPaidX(rp.obj, o)
	}
	// The triggered-cost window's dynamic tapXType<X/Spec> payment (the
	// Battlesphere/yotia shape): the election's tap count is the cost's
	// announced X. It wins over both reads above.
	if rp.tapPaidX != 0 {
		ctx.X = rp.tapPaidX
		ctx.XAnnounced = true
	}
	// The trigger-cost window's X fold (the {X}/{PayLife<X>} announcement):
	// the announced or fixed value binds exactly like the dyn-tap count.
	if rp.winPaidX != 0 {
		ctx.X = rp.winPaidX
		ctx.XAnnounced = true
	}
	var svars map[string]string
	if o.Ability != nil {
		// A triggered or activated ability: mirror resolveTop's ability
		// branch -- Source is the source permanent, Remembered carries what
		// the trigger captured, and the SVar table comes from that
		// permanent's face.
		ctx.Source = o.Source
		ctx.TriggerContext = e.triggerContexts[rp.obj]
		ctx.Remembered = e.resolvingRemembered(o)
		ctx.Captured = o.Remembered
		ctx.Grantor = o.GrantedBy
		bindNinjutsuDefender(ctx, o)
		if abilityXAnnounced(o) {
			ctx.XAnnounced = true
		}
		reflexiveCaptured(ctx)
		if lki, ok := e.triggerLKI[rp.obj]; ok {
			ctx.LKI = lki.object
			ctx.Snap.Power, ctx.Snap.Toughness, ctx.Snap.PTValid =
				lki.power, lki.toughness, lki.ptValid
		}
		if link, ok := e.sourceLifelinkLKI[rp.obj]; ok {
			ctx.Snap.SourceLifelink = link
			ctx.Snap.SourceLifelinkValid = true
		}
		if controller, ok := e.sourceControllerLKI[rp.obj]; ok {
			ctx.Snap.SourceController = controller
			ctx.Snap.SourceControllerValid = true
		}
		if lki := e.damageSourceLKI[rp.obj]; lki != nil {
			ctx.Snap.DamageSource = cloneDamageSourceLKI(lki)
		}
		// CR 603.3c: keep the placement-announced mode choice.
		ctx.Modes = resumeChosenModes(rp, o)
		// The same table resolveTop's first pass read: the owning face of a
		// pile's under-card ability, and above all a granted or delayed
		// trigger's recorded line table.
		svars = e.abilityResolutionSVars(rp.obj, o)
	} else if f := o.Face(); f != nil {
		svars = f.SVars
	}
	effects.SetSVars(ctx, svars)
	// Task mvts1: carry the SA whose targeting the placement/announcement ask
	// covered, exactly as resolveTop's first pass does.
	if offeredSA := offeredTargetSA(o, svars); offeredSA != nil {
		ctx.OfferedSA = offeredSA
	}
	// An accepted optional trigger may itself carry Cost$ (Mana Vault's "you
	// may pay {4}; if you do" untap). The optional answer chooses to attempt
	// the effect; payment is a separate resolution-time window.
	tc := e.triggerContexts[rp.obj]
	armed := rp.kind == "optional" && rp.sa != nil &&
		(e.triggerBodyNeedsCostWindow(rp.sa) ||
			// abcopy1: an OptionalDecider$ copy trigger's AB$ CopySpellAbility
			// with a real Cost$ pays through the same window whenever the
			// trigger context carries an event role.
			(rp.sa.API == "CopySpellAbility" && rp.sa.ParamStr(cards.PKCost) != "" &&
				(tc.TriggerAbility != 0 || tc.TriggerCard != 0)))
	if armed {
		e.startTriggeredEffectCost(rp, ctx.Source)
		return
	}
	if rp.sa != nil {
		// Cross-mode TargetUnique attribution: a frame whose SA is one of the
		// chosen target-bearing modes' chains re-enters with that mode's own
		// target.
		if one := e.charmModeTarget(rp.obj, rp.sa); one != nil {
			ctx.Targets = one
		}
		src := rp.obj
		if o.Ability != nil {
			src = o.Source
		}
		e.damaging = src
		savedWinX := e.windowPaidX
		e.windowPaidX = rp.winPaidX
		defer func() { e.windowPaidX = savedWinX }()
		effects.Resolve(e, ctx, rp.sa)
		e.damaging = 0
		// A resumed EndTurn has already exiled the stack, including the
		// resolving ability: enter cleanup just as resolveTop does.
		if e.endTurnRequested {
			e.finishEndTurn()
			e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
			return
		}
	}
	e.finishResumption(rp.obj)
	// CR 117.3b / the counterpart of handlePriority's pass-branch grant: when
	// a resolution that ran a window completes, the pass count resets and
	// priority returns to the active player.
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
}
