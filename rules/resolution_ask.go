// resolution_ask.go holds the ask side of the mid-resolution mechanism: Engine.Ask, its resume-point capture (buildAskResume), the Cxt-scoped resolution state setters and the departing-target snapshots.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Ask implements effects.Host.Ask (rules' side of the interface, and the
// only place a mid-resolution decision is born). It records the resume
// point — the suspended object is always the top of stack, because a
// decision is pending from this moment until it is answered and Advance's
// loop never runs while one is, so nothing in between can resolve or move —
// and hands the decision to the ordinary ask path. Its `outer` is nil here:
// if a resume re-entry posed this nested ask, the enclosing resumeResolution
// (which owns the continuation of the SA it was re-entering) links it once
// effects.Resolve returns. Always returns true: this engine can always ask.
func (e *Engine) Ask(d *decision.Decision) bool {
	// An ask posed inside an off-stack mana resolution has no stack object to
	// park on: carry it through the mana activation's own continuation instead.
	if e.askOffStackMana(d) {
		return true
	}
	obj := state.ObjID(0)
	direct := false
	if n := len(e.G.Stack); n > 0 {
		obj = e.G.Stack[n-1]
	} else {
		obj = d.Source
		direct = true
	}
	kind := d.ResumeKind
	if kind == "" {
		kind = "modes"
	}
	if e.resume != nil && e.pending != nil && e.contChainOwners > 0 {
		// A SECOND mid-resolution ask while an earlier one of the same
		// resolution pass is still unanswered: the first ask was posed from
		// inside a move the asking effect made (a ReplaceWith$ body -- a
		// shock land's "you may pay 2 life" -- suspends the resolution, but
		// the effect that moved the land keeps running its own body: the
		// search reaches its may-shuffle confirm, a mass return moves the
		// next shock land). Posing it now would overwrite the pending
		// decision (Engine.ask's guard). Defer it instead: the fully built
		// resume point rides a "deferred_ask" continuation frame appended to
		// this pass's contChain, so it is posed exactly when the answered
		// earlier frame (and every continuation reported before this ask)
		// has run, and before the continuations the enclosing loops report
		// after it. The asking caller sees an ordinary suspended ask.
		rp := e.buildAskResume(d, obj, direct, kind)
		e.contChain = append(e.contChain, contFrame{sa: d.ResumeSA, deferredAsk: d, deferredResume: rp})
		e.lastDeferred = rp
		e.askCount++
		return true
	}
	e.ask(d)
	e.resume = e.buildAskResume(d, obj, direct, kind)
	if e.contChainOwners > 0 {
		// Mark where the ask sits in this pass's reports, with the Ctx it was
		// posed under, so buildContinuationChain can tell which enclosing
		// loops walked the same Ctx (resumePoint.inheritsRemembered).
		e.contChain = append(e.contChain, contFrame{askMarker: true, ctx: e.resolutionCtx})
	}
	e.lastDeferred = nil
	e.askCount++
	return true
}

// AskCount implements effects' optional askCounter seam: the number of
// mid-resolution asks this engine has taken (posed or deferred). effects.
// Resolve's UnlessCost$ gate compares it across the gate, because a gate ask
// deferred behind an already-suspended resolution leaves Suspended()
// unchanged. Engine scratch, never logged.
func (e *Engine) AskCount() uint64 { return e.askCount }

// EventMark implements effects' optional eventMarker seam: the event log's
// current length, a mark StateChangedSince compares against.
func (e *Engine) EventMark() int { return len(e.L.Events) }

// StateChangedSince implements effects' optional eventMarker seam: whether
// any event other than a Note was logged after mark. A Note is the log's
// commentary and folds into no game state (events.Apply), so a span holding
// only Notes left the game exactly as it found it. A ZERO-amount Damage
// event is the same kind of no-op: CR 120.8 says 0 damage is never dealt,
// and the fold marks nothing for it (a DamageAll whose NumDmg$ counts an
// empty Remembered set -- Kindle the Carnage repeated over an empty hand
// logs one per creature per pass, and counting it as progress let a bot's
// "Repeat" answer loop forever, cardfuzz batch8 line 1). A ZERO-amount
// LifeChange likewise folds into nothing (events.Apply adds 0 to the life
// total; CR 119.9's "gains 0 life" is no life-gain event, and a 0 loss the
// same): Ad Nauseam repeated over an empty library loses life equal to the
// mana value of no card. AFLifeLost is always published, even for that zero
// loss: a StoreSVar only changes state when it replaces a different value.
func (e *Engine) StateChangedSince(mark int) bool {
	if mark < 0 {
		mark = 0
	}
	for i := mark; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.Note || (ev.Kind == events.Damage && ev.Amount == 0) ||
			(ev.Kind == events.LifeChange && ev.Amount == 0) {
			continue
		}
		if ev.Kind == events.StoreSVar {
			// Read the value immediately before this event, not the final
			// object value: intermediate changes still count as progress.
			unchanged := false
			for j := i - 1; j >= 0; j-- {
				prev := e.L.Events[j]
				// CR 400.7 resets RuntimeSVars on a zone change: an
				// earlier write to that ObjID is no longer its old value.
				if prev.Obj == ev.Obj && (prev.Kind == events.MoveZone || prev.Kind == events.Draw || prev.Kind == events.PutOnStack) {
					break
				}
				if prev.Kind == events.StoreSVar && prev.Obj == ev.Obj && prev.Text == ev.Text {
					unchanged = prev.Amount == ev.Amount
					break
				}
			}
			if unchanged {
				continue
			}
		}
		return true
	}
	return false
}

// chosenDirectionForResume reads the live resolution Ctx's chosen direction
// for a resume point (empty when no chain is published, or none was chosen
// yet). It is how a suspended mid-resolution ask keeps the ChooseDirection
// answer its chained SubAbility reads after a freshly rebuilt Ctx.
func chosenDirectionForResume(c *effects.Ctx) string {
	if c == nil {
		return ""
	}
	return c.ChosenDirection
}

// buildAskResume builds the resume point of the mid-resolution ask d from
// the engine's ambient resolution state at the moment the ask is posed (or
// deferred). See Engine.Ask.
func (e *Engine) buildAskResume(d *decision.Decision, obj state.ObjID, direct bool, kind string) *resumePoint {
	var replacementTarget state.Target
	var replacementAmount int32
	if e.replacingEvent != nil && e.replacingEvent.Kind == events.Damage {
		replacementTarget = state.Target{Obj: e.replacingEvent.Obj}
		if e.replacingEvent.Obj == 0 {
			replacementTarget = state.Target{Player: e.replacingEvent.Player, IsPlayer: true}
		}
		replacementAmount = e.replacingEvent.Amount
	}
	replacementSource := e.protectionSource(e.damaging)
	if e.replacingEvent == nil && e.applyingReplacement && e.resolutionCtx != nil &&
		e.resolutionCtx.ReplacementTarget != (state.Target{}) {
		// A replacement body re-entered from a resume frame (a second ask of
		// a resumed body, or a body queued behind an earlier ask by
		// resolveReplacementBody) runs with no live replacingEvent: the
		// frame's rebuilt Ctx is the only record of what the body replaced,
		// so this ask's resume must carry it on, or the answered re-entry
		// (Nefarious Lich's DefinedPlayer$ ReplacedTarget pick) resolves
		// against nobody and moves nothing.
		replacementTarget = e.resolutionCtx.ReplacementTarget
		replacementAmount = e.resolutionCtx.ReplacementAmount
		replacementSource = e.resolutionCtx.ReplacementSource
	}
	// Capture whether the ask is being posed from inside a replacement
	// effect's ReplaceWith$ body (fx44). e.applyingReplacement is true for
	// the whole of that body's resolution, so an ask posed from within it
	// must resume still under the flag — see the resumePoint field's
	// comment and resumeResolution's restore of it.
	//
	// A replacement body resumes from the object whose resolution it
	// interrupted -- normally still on top of the stack (a sorcery that
	// reanimates Mox Diamond), so the rest of that spell's chain and its
	// completion still run after the answer. The one exception is a permanent
	// spell whose own Updated entry replacement asks (Sower of Discord): the
	// move has already taken the resolving object off the stack, so the top
	// of the stack is some unrelated object and the resume must rebuild from
	// the entering permanent itself. replSource keeps the replacement's host
	// for the resumed body's own Source either way. The one further
	// exception this build adds is an effect invoked outside stack
	// resolution (direct), whose resume must find its own source.
	var replSource state.ObjID
	ownResolution := false
	if e.applyingReplacement && d.Source != 0 {
		replSource = d.Source
		// The resume must rebuild from the replacement's host in exactly two
		// shapes: a permanent spell whose own Updated entry replacement asks
		// (Sower of Discord — d.Source IS e.resolvingObj but the move has
		// already taken it off the stack, so the top of the stack is some
		// unrelated object), and a permanent that entered by a replacement with
		// NO stack resolution in flight at all (a land drop: Hallowed Fountain's
		// "you may pay 2 life. If you don't, it enters tapped" — e.resolvingObj
		// is 0, so the original d.Source == e.resolvingObj gate never fired and
		// the answer degraded to a no-op). Every other shape — a replacement
		// asking while a DIFFERENT spell is resolving (Mox Diamond reanimated by
		// a sorcery: d.Source is the mox, already on the battlefield, but the
		// spell whose chain the replacement interrupted is still the top of the
		// stack and MUST own the resume) — keeps the ordinary top-of-stack
		// resume. resolveTop keeps e.resolvingObj == the stack top for the whole
		// of a spell's resolution, so "top of stack is the interrupted spell" is
		// exactly "e.resolvingObj != 0 && d.Source != e.resolvingObj".
		if so := e.G.Obj(d.Source); so != nil && so.Zone != state.ZStack &&
			(e.resolvingObj == 0 || d.Source == e.resolvingObj) {
			obj = d.Source
			ownResolution = e.resolvingObj != 0
		}
	}
	parentLinks, linkAnswer, linkAnswered := e.resolutionParentLinks()
	// An asking site that rides no ResumeRemembered of its own resumes with
	// the chain's Remembered as it stood when it asked: the first pass
	// resolved in ONE Ctx whose Remembered the chain had already grown, and
	// 83 of the 120 asking sites in effects/ ride nothing (spike S3's
	// Remembered-across-a-suspension class). A VillainousChoice ask's ride is
	// its victim list, never the chain's, so an empty one stays empty.
	remembered := d.ResumeRemembered
	if remembered == nil && e.resolutionCtx != nil && kind != "villainous" {
		remembered = e.resolutionCtx.Remembered
	}
	return &resumePoint{kind: kind, obj: obj, sa: d.ResumeSA, replSource: replSource,
		replacement: e.applyingReplacement, replaced: e.replReplaced, action: e.replAction,
		replacedCards:     append([]state.ObjID(nil), e.replReplacedCards...),
		redirect:          e.replRedirect,
		replacedPlayer:    e.replReplacedPlayer,
		replacementTarget: replacementTarget, replacementSource: replacementSource,
		replacementAmount: replacementAmount,
		effectFrame:       e.currentEffectFrame,
		before:            e.retainTriggerBefore(), target: d.ResumeTarget, player: d.Player,
		chosenDirection: chosenDirectionForResume(e.resolutionCtx),
		direct:          direct, ownResolution: ownResolution, rolls: d.Rolls, clash: cloneClashResume(d.ResumeClash),
		choices:     append([]state.Target(nil), d.ResumeChoices...),
		chosenValid: d.ResumeChosenValid, remembered: append([]state.Target(nil), remembered...),
		pendingDamage:       effects.ClonePendingDamage(e.resolutionPendingDamage()),
		parentLinks:         parentLinks,
		linkAnswer:          linkAnswer,
		linkAnswered:        linkAnswered,
		searchKnown:         append([]state.Target(nil), d.ResumeSearchKnown...),
		forgetOtherSnapshot: append([]state.Target(nil), d.ResumeForgetOtherSnapshot...),
		forgetOtherOwners:   append([]state.PlayerID(nil), d.ResumeForgetOtherOwners...),
		forgetOther:         forgetOtherRide{ready: d.ResumeForgetOtherReady, cleared: d.ResumeForgetOtherCleared},
		digUntilMove:        d.ResumeDigUntilMove, digUntilMoveDone: d.ResumeDigUntilMoveDone,
		clonePick: d.ResumeClonePick, clonePickDone: d.ResumeClonePickDone,
		moved:   append([]state.ObjID(nil), d.ResumeMoved...),
		uptoIdx: d.ResumeUptoIdx, uptoCount: d.ResumeUptoCount, exploreDone: d.ResumeExploreDone,
		villainousVictims:       append([]state.Target(nil), d.ResumeVillainousVictims...),
		villainousIndex:         d.ResumeVillainousIndex,
		genericChoosers:         append([]state.Target(nil), d.ResumeGenericChoosers...),
		genericChooserIndex:     d.ResumeGenericChooserIndex,
		numberPicks:             append([]int32(nil), d.ResumeNumberPicks...),
		villainousRemembered:    append([]state.Target(nil), e.villainousRemembered...),
		villainousRememberedSet: e.villainousRememberedSet,
		targetsUnique:           e.targetsUniqueRide(d),
		fusedTargets:            append([]state.Target(nil), e.fusedResolving...),
		fusedTargetsSet:         e.fusedResolvingSet,
		charmModeScope:          e.charmModeScope(),
		charmModeSA:             e.charmModeScopeSA(),
		fusedSVars:              e.fusedResolvingSVars,
		winPaidX:                e.windowPaidX,
		timeTravelObjects:       append([]state.ObjID(nil), d.ResumeObjects...),
		timeTravelRound:         d.ResumeRound,
		repeatOptionalNext:      d.ResumeRepeatNext,
		// The pre-move controller snapshot of this chain's object targets,
		// published by effects.Resolve around the whole chain. Captured onto
		// the pending frame so a resumed continuation (which rebuilds its Ctx
		// from objects whose live controllers may already have been reset to
		// their owners) still sees the CR 608.2h last-known controller.
		targetControllerLKI: effects.CloneTargetControllerLKI(e.resolvingTargetControllerLKI),
		targetCountersLKI:   resolutionTargetCounters(e.resolutionCtx),
		targetPTLKI:         resolutionTargetPT(e.resolutionCtx),
		targetSpellLKI:      resolutionTargetSpells(e.resolutionCtx),
		flipMemory:          e.resolvingFlipMemory,
		exchangeMemory:      e.resolvingExchangeMemory}
}

// Suspended implements effects.Host.Suspended: the resolution is suspended
// when a mid-resolution ask set e.resume and the answer has not yet arrived
// to clear it. effects.Resolve checks this after every sub-ability so that a
// suspended ask stops the SubAbility chain instead of running what sits
// beneath it (B1). It is the rules-side half of the pairing with Ask: Ask
// sets e.resume, and handleModes clears it the moment the answer lands, so
// the resume pass re-enters the chain with nothing suspended and walks the
// rest of it exactly once.
//
// SetResolutionFlipMemory implements effects' flipMemoryHost (an optional
// seam, so the effects test doubles need no method): effects.Resolve publishes
// the resolving chain's shared coin-flip memory around the whole walk and
// restores the enclosing value on return, and effFlipCoin re-publishes when it
// lazily allocates that memory. Returns the previous value for the defer
// restore. Engine-transient scratch like resolvingTargetControllerLKI.
func (e *Engine) SetResolutionFlipMemory(m *effects.FlipMemory) *effects.FlipMemory {
	prev := e.resolvingFlipMemory
	e.resolvingFlipMemory = m
	return prev
}

// SetResolutionExchangeMemory implements effects' exchangeMemoryHost (an
// optional seam, so the effects test doubles need no method): effects.Resolve
// publishes the resolving chain's shared ExchangeLife rider memory around the
// whole walk and restores the enclosing value on return, and effExchangeLife
// re-publishes when it lazily allocates that memory. Returns the previous
// value for the defer restore. Engine-transient scratch like
// resolvingFlipMemory, never a writer of the resume state the archtest guards.
func (e *Engine) SetResolutionExchangeMemory(m *effects.ExchangeMemory) *effects.ExchangeMemory {
	prev := e.resolvingExchangeMemory
	e.resolvingExchangeMemory = m
	return prev
}

// SetResolutionTargetControllerLKI implements
// effects.Host.SetResolutionTargetControllerLKI: effects.Resolve publishes
// the target-controller snapshot of the chain it is about to walk, and
// restores the previous value on return, so the scratch holds exactly the
// innermost running chain's map. Engine.Ask consumes it onto the pending
// resumePoint. Writes engine scratch, never e.resume, so it is not a writer
// of the resume state the archtest guards.
func (e *Engine) SetResolutionTargetControllerLKI(m map[state.ObjID]state.PlayerID) map[state.ObjID]state.PlayerID {
	prev := e.resolvingTargetControllerLKI
	e.resolvingTargetControllerLKI = m
	return prev
}

// SetResolutionCtx implements effects.Host's optional resolutionCtxHost
// interface: effects.Resolve publishes the live Ctx of the chain it is about
// to walk, and restores the previous value on return, so the scratch holds
// exactly the innermost running chain's Ctx. Engine.Ask consumes it as the
// fallback source of the chain's TargetUnique$ accumulator. Writes engine
// scratch, never e.resume, so it is not a writer of the resume state the
// archtest guards.
func (e *Engine) SetResolutionCtx(c *effects.Ctx) *effects.Ctx {
	prev := e.resolutionCtx
	e.resolutionCtx = c
	return prev
}

// resolutionTargetCounters is the target-counters snapshot a pending ask
// carries onto its resume point: the live Resolve chain's snapshot (published
// through SetResolutionCtx), cloned so the frame owns its storage. Nil outside
// a chain (a combat or mulligan ask) or when the chain captured none.
func resolutionTargetCounters(c *effects.Ctx) map[state.ObjID][]state.Counter {
	if c == nil {
		return nil
	}
	return effects.CloneTargetCountersLKI(c.TargetCountersLKI)
}

// resolutionTargetPT is the target P/T look-back a pending ask carries onto
// its resume point, cloned like resolutionTargetCounters.
func resolutionTargetPT(c *effects.Ctx) map[state.ObjID]effects.TargetPT {
	if c == nil {
		return nil
	}
	return effects.CloneTargetPTLKI(c.TargetPTLKI)
}

// resolutionTargetSpells is the target-spell snapshot a pending ask carries
// onto its resume point: the live Resolve chain's resolution-start set of
// object targets that were spells on the stack, cloned so the frame owns its
// storage. Nil outside a chain (a combat or mulligan ask) or when the chain
// captured none. Without it a resumed Ctx -- rebuilt from the stack object's
// targets, whose live zone a completed Counter/ChangeZone has already
// changed -- would answer SpellTargeted$CardManaCostLKI as zero.
func resolutionTargetSpells(c *effects.Ctx) map[state.ObjID]bool {
	if c == nil {
		return nil
	}
	return effects.CloneTargetSpellLKI(c.TargetSpellLKI)
}

// resolutionPendingDamage is the DamageMap$ True mark set a pending ask
// carries onto its resume point: the live Resolve chain's marks (published
// through SetResolutionCtx), cloned so the frame owns its storage. Nil
// outside a chain (a combat or mulligan ask) or when the chain has marked
// nothing.
func (e *Engine) resolutionPendingDamage() []effects.PendingDamage {
	if e.resolutionCtx == nil {
		return nil
	}
	return effects.ClonePendingDamage(e.resolutionCtx.PendingDamage)
}

// resolutionParentLinks is the walk's parent-link record a pending ask carries
// onto its resume point: the live Resolve chain's record (published through
// SetResolutionCtx), deep-copied so the frame owns its storage. It is the same
// ride as resolutionPendingDamage -- runtime continuation state of the walk,
// never client input -- and it is what lets a resumed resolution keep naming
// the NEAREST targeting ancestor when a later link's ParentTarget is read.
// Nil/empty outside a chain or when no targeting link ran before the ask.
func (e *Engine) resolutionParentLinks() ([][]state.Target, []state.Target, bool) {
	if e.resolutionCtx == nil {
		return nil, nil, false
	}
	return e.resolutionCtx.ParentLinkRide()
}

// snapshotDepartingTargetCounters refreshes the resolving chain's target-
// counters look-back (Ctx.TargetCountersLKI) at the DEPARTURE boundary, the
// authoritative capture point for the CR 608.2b/h read: oid is about to leave
// the battlefield (its MoveZone is in flight, pre-Apply), so if it is an
// object target of the published chain Ctx its counters are captured NOW --
// immediately before events.Apply's Move fold clears them -- overwriting
// effects.Resolve's resolution-start snapshot. A chain that changed the
// target's counters earlier in the same resolution (Dismantle-style destroy
// preceded by a counter swing) must look back to the counters as they were at
// the zone change, not to the stale entry capture. An object that departs
// with no counters drops its entry, so the read fails closed to the live
// (already-cleared) zero. Non-targets are never captured: the look-back is
// read only through the chain's target groups.
func (e *Engine) snapshotDepartingTargetCounters(oid state.ObjID) {
	c := e.resolutionCtx
	if c == nil {
		return
	}
	targeted := false
	for _, t := range c.Targets {
		if !t.IsPlayer && t.Obj == oid {
			targeted = true
			break
		}
	}
	if !targeted && c.PickedTargets != nil {
		for _, t := range c.PickedTargets {
			if !t.IsPlayer && t.Obj == oid {
				targeted = true
				break
			}
		}
	}
	if !targeted {
		return
	}
	o := e.G.Obj(oid)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	// The P/T half (Ctx.TargetPTLKI): the layer-derived power and toughness
	// the target has at this last battlefield instant (CR 608.2h -- a
	// Giant Growth-pumped Condemn target's toughness, not the printed one).
	if o.Face() != nil {
		if c.TargetPTLKI == nil {
			c.TargetPTLKI = make(map[state.ObjID]effects.TargetPT)
		}
		c.TargetPTLKI[oid] = effects.TargetPT{Power: e.Power(oid), Toughness: e.Toughness(oid)}
	}
	if len(o.Counters) == 0 {
		delete(c.TargetCountersLKI, oid)
		return
	}
	if c.TargetCountersLKI == nil {
		c.TargetCountersLKI = make(map[state.ObjID][]state.Counter)
	}
	c.TargetCountersLKI[oid] = append([]state.Counter(nil), o.Counters...)
}

// targetsUniqueRide is the TargetUnique$ accumulator a decision resumes with:
// the accumulator the asking site already stamped onto the decision when it
// carried one, else the LIVE accumulator of the Resolve chain currently
// running (published through SetResolutionCtx). It is the one home of the
// ride, so an ask of ANY kind -- a modal election, a ward pay, a
// dig/scry/arrange pick -- parked between two TargetUnique$ riders preserves
// the picks earlier riders chose, rather than only the shared target pre-ask
// and the unless gate that stamped it explicitly. Nil outside a chain (a
// combat or mulligan ask), where there is no accumulator to carry.
func (e *Engine) targetsUniqueRide(d *decision.Decision) []state.Target {
	if len(d.ResumeTargetsUnique) > 0 {
		return append([]state.Target(nil), d.ResumeTargetsUnique...)
	}
	if e.resolutionCtx == nil {
		return nil
	}
	return append([]state.Target(nil), e.resolutionCtx.TargetsUnique...)
}
