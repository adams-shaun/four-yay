// resolution_chain.go holds the resumption tail helpers: the continuation-chain builder, the completion of a resumed resolution (finishResumption, moveResolvedOffStack) and the mode-label helpers the answer arms share.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// charmModeTarget re-derives, for a frame of a cross-mode TargetUnique
// Charm's resolution (see SuspendCharmRest / the charm_rest kind), the ONE
// target the SA's chain belongs to: the j-th positional entry of the stack
// object's Targets, where j is the frame's position among the chosen
// target-bearing modes. Returns nil whenever the frame is not part of such a
// charm's resolution — including every non-family shape (classification None
// or Unsupported) and every insufficient-candidate fallback (fewer recorded
// targets than the chosen target-bearing modes need) — so those keep the
// shared list byte-identically. sa == nil is the no-answer continuation
// shape the Line-match cannot serve; the charm_rest frame itself carries the
// Charm SA, whose body text never equals a mode body's, so it matches
// nothing and effCharm's own split handles it.
func (e *Engine) charmModeTarget(obj state.ObjID, sa *cards.SA) []state.Target {
	if sa == nil {
		return nil
	}
	o := e.G.Obj(obj)
	if o == nil || o.Ability == nil || len(o.ChosenModes) == 0 || len(o.Targets) == 0 {
		return nil
	}
	src := e.G.Obj(o.Source)
	if src == nil || src.Face() == nil {
		return nil
	}
	svars := src.Face().SVars
	choices := effects.CharmOf(o.Ability).Modes
	if status, _ := effects.CharmCrossModeShape(svars, choices); status != effects.CharmUniqueSupported {
		return nil
	}
	var tbms []*cards.SA
	for _, name := range o.ChosenModes {
		if sub := cards.ResolveSVar(svars, name); sub != nil && strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" {
			tbms = append(tbms, sub)
		}
	}
	if len(tbms) < 2 || len(o.Targets) < len(tbms) {
		return nil
	}
	for j, sub := range tbms {
		for w := sub; w != nil; w = w.Sub {
			if w.Line != "" && w.Line == sa.Line {
				return []state.Target{o.Targets[j]}
			}
		}
	}
	return nil
}

// buildContinuationChain turns the enclosing-loop suspension points reported
// for one re-entry into a linked run of pure-continuation frames (kind ""),
// in report order — inner continuations first, outer last — and chains the
// given `tail` (the outer continuation of the frame being re-entered) onto
// the end. The resulting head is the what the new pending point must run
// after its own answer, or nil if there is nothing left to continue.
//
// A PLAIN report (no repeat/rest/deferred payload) whose SA is the last link
// of its chain (sa.Sub == nil) has nothing left to run: the enclosing loop
// finished at the link that suspended. It gets no frame. Before this rule it
// became a frame with a nil SA, and resuming that frame logged the
// "mid-resolution answer resumed with no sub-ability recorded" degradation
// Note -- Devour Intellect's DB$ Branch, Capital Punishment's Vote and every
// mass move whose as-enters choice parked all reached it (spike S3, 20 of the
// 10k dual-run fuzz divergences). The frame's only effect on completion was
// to hand its Remembered on to the next frame (resumeResolution's loopBound
// rule); a bound empty report keeps that hand-off by applying it to the next
// frame here, and an unbound one had nothing to hand on.
func (e *Engine) buildContinuationChain(frames []contFrame, obj state.ObjID, tail *resumePoint) *resumePoint {
	var head, prev *resumePoint
	var carry []state.Target
	carrySet := false
	// prevCtx is the Ctx of the frame the next kept frame follows: the
	// posed ask's walk (its askMarker) for the head, then each kept frame's
	// own loop.
	var prevCtx *effects.Ctx
	for _, cf := range frames {
		if cf.askMarker {
			prevCtx = cf.ctx
			continue
		}
		sa := cf.sa
		if cf.isPlain() && (sa == nil || sa.Sub == nil) {
			if cf.bound {
				carry, carrySet = cf.remembered, true
			}
			continue
		}
		// fx44: a continuation frame is the rest of the same resolution that
		// just suspended, so it carries the replacement context too — a body
		// that asks again and then continues must keep emitting under the
		// replacement guard, not re-interposed by the replacement it is the
		// product of, and must keep addressing the object it replaced (the
		// replaced/id carried by this frame comes from the engine's active
		// replacement context).
		f := &resumePoint{obj: obj, sa: sa.Sub, replacement: e.applyingReplacement,
			replaced: e.replReplaced, action: e.replAction, replacedPlayer: e.replReplacedPlayer,
			redirect:  e.replRedirect,
			before:    e.retainTriggerBefore(),
			loopBound: cf.bound, loopRemembered: cf.remembered, repeatSubject: cf.repeatSubject,
			voteCounts: cloneVoteCounts(cf.voteCounts),
			// Every continuation the loop of THIS re-entry reported belongs to
			// the same resolution, so a fused half's target binding is inherited
			// verbatim: the frames build while Engine.fusedResolving is set (the
			// callers set it around effects.Resolve and keep it set through this
			// build), and a frame that re-enters a loop body inside the half
			// must bind the half's slice, not the flat list (Flesh // Blood's
			// SubAbility reached through an enclosing loop).
			fusedTargets:    append([]state.Target(nil), e.fusedResolving...),
			fusedTargetsSet: e.fusedResolvingSet,
			fusedSVars:      e.fusedResolvingSVars,
			winPaidX:        e.windowPaidX,
			// The VillainousChoice victim ambient (the fusedResolving pattern):
			// a continuation frame of the chosen body's chain keeps the victim
			// so an ask posed by a later frame of that chain still reads
			// Defined$ Remembered as the victim. A frame built outside a
			// chosen body inherits nil/absent and binds nothing.
			villainousRemembered:    append([]state.Target(nil), e.villainousRemembered...),
			villainousRememberedSet: e.villainousRememberedSet}
		// Every continuation frame re-enters a loop of the SAME resolution as
		// the pending ask, rebuilding its Ctx from the stack object's targets;
		// inherit that resolution's pre-move controller snapshot so a
		// chained TokenOwner$ TargetedController reached through an enclosing
		// loop still sees it (the pending point was built by Ask from the
		// same chain's published map).
		if e.resume != nil {
			f.targetControllerLKI = effects.CloneTargetControllerLKI(e.resume.targetControllerLKI)
			f.targetCountersLKI = effects.CloneTargetCountersLKI(e.resume.targetCountersLKI)
			f.targetPTLKI = effects.CloneTargetPTLKI(e.resume.targetPTLKI)
			f.targetSpellLKI = effects.CloneTargetSpellLKI(e.resume.targetSpellLKI)
			f.replacedCards = append([]state.ObjID(nil), e.resume.replacedCards...)
			// The walk's parent-link record (effects/parent_targets.go) is a
			// property of the whole resolution, not of one frame: a continuation
			// frame that re-enters an ENCLOSING loop of the same resolution
			// rebuilds its Ctx from the stack object's targets, so it must
			// inherit the record the pending point captured or a later link's
			// ParentTarget would fall back to the root's list once more. The
			// in-walk answer rides with it for the same reason.
			f.parentLinks = cloneParentLinks(e.resume.parentLinks)
			f.linkAnswer = append([]state.Target(nil), e.resume.linkAnswer...)
			f.linkAnswered = e.resume.linkAnswered
		}
		// The same-resolution flip memory (Engine.Ask captured it off
		// Engine.resolvingFlipMemory onto the pending point): a continuation
		// frame of the same resolution carries the same shared pointer, so a
		// resumed FlipCoin cursor re-entry (the "flip_rest" frame) and any
		// chained Defined$ FlippedHeads / FlippedTails / Count$RememberedNumber
		// reader in a later frame still see the flips performed before the
		// suspension. The memory is mutated in place (effects.flipRecord), so a
		// pointer copy — never a value clone — keeps every frame live.
		if e.resume != nil {
			f.flipMemory = e.resume.flipMemory
			f.rolls.ride = e.resume.rolls.ride
			f.exchangeMemory = e.resume.exchangeMemory
		}
		if e.replacingEvent != nil && e.replacingEvent.Kind == events.Damage {
			f.replacementTarget = state.Target{Obj: e.replacingEvent.Obj}
			if e.replacingEvent.Obj == 0 {
				f.replacementTarget = state.Target{Player: e.replacingEvent.Player, IsPlayer: true}
			}
			f.replacementAmount = e.replacingEvent.Amount
			f.replacementSource = e.protectionSource(e.damaging)
		}
		if cf.deferredAsk != nil {
			// A deferred second ask (Engine.Ask): its own resume point was
			// fully built when it was asked; this frame only poses it.
			f = &resumePoint{kind: "deferred_ask", obj: obj,
				deferredAsk: cf.deferredAsk, deferredResume: cf.deferredResume}
		} else if cf.charmRest != nil {
			// The Charm re-enters ITSELF (rp.sa = the Charm SA, not sa.Sub — a
			// Charm body has no SubAbility$ chain of its own to resume) with
			// Ctx.Modes = the remaining chosen modes.
			f.kind, f.sa, f.charmRest = "charm_rest", sa, cf.charmRest
		} else if cf.villainousRest {
			// The VillainousChoice re-enters ITSELF (rp.sa = the VillainousChoice
			// SA, not sa.Sub — a VillainousChoice body has no SubAbility$ chain
			// of its own to resume) with the victim cursor restored.
			f.kind, f.sa = "villainous_rest", sa
			f.villainousVictims = append([]state.Target(nil), cf.villainousVictims...)
			f.villainousIndex = cf.villainousIndex
		} else if cf.genericChoiceRest {
			// The GenericChoice re-enters ITSELF (rp.sa = the GenericChoice SA,
			// not sa.Sub — its own SubAbility$ chain runs only after every
			// chooser has answered) with the chooser cursor restored.
			f.kind, f.sa = "generic_players_rest", sa
			f.genericChoosers = append([]state.Target(nil), cf.genericChoosers...)
			f.genericChooserIndex = cf.genericChooserIndex
			f.genericRemembered = append([]state.Target(nil), cf.genericRemembered...)
		} else if cf.flipRest {
			// The FlipCoin re-enters ITSELF (rp.sa = the FlipCoin SA, not
			// sa.Sub — a FlipCoin body has no SubAbility$ chain of its own to
			// resume) with the flip cursor restored.
			f.kind, f.sa = "flip_rest", sa
			f.flipCursor = cf.flipCursor
		} else if cf.tokenRest != nil {
			// The Token re-enters ITSELF (rp.sa = the Token SA): the parked
			// mint's riders, the mints after it and the post-loop work run
			// there, and the re-entry's own walk then continues at sa.Sub.
			f.kind, f.sa = "token_rest", sa
			f.tokenRest = cf.tokenRest.Clone()
			f.unlessResolved = cf.unlessResolved
		} else if cf.repeat != nil {
			f.kind, f.sa, f.repeat = "repeat", sa, cf.repeat
			if cf.repeat.body {
				f.kind, f.sa = "repeat_body", sa
			}
			f.choices, f.chosenValid = cf.choices, cf.chosenValid
		}
		if (cf.isPlain() || cf.isRepeatBody()) && cf.ctx != nil && cf.ctx == prevCtx {
			// An api:Repeat loop frame inherits like a plain one: its body
			// walked the Repeat's own Ctx, so the re-entered loop's gate
			// (RepeatDefined$ Remembered) must read what the answered body
			// finished with, not the stack object's stale Remembered.
			f.inheritsRemembered = true
		}
		prevCtx = cf.ctx
		if carrySet {
			handOnRemembered(f, carry)
			carry, carrySet = nil, false
		}
		if head == nil {
			head = f
		} else {
			prev.outer = f
		}
		prev = f
	}
	if carrySet && tail != nil {
		handOnRemembered(tail, carry)
	}
	if prev != nil {
		prev.outer = tail
	} else {
		head = tail
	}
	return head
}

// handOnRemembered is the loopBound hand-off a completed frame makes to the
// frame after it (resumeResolution): a repeat frame folds what the finished
// iteration remembered into its cursor, and any other frame runs at the same
// level and takes the Remembered as is.
func handOnRemembered(next *resumePoint, remembered []state.Target) {
	if next.kind == "repeat" && next.repeat != nil {
		next.repeat.last = append([]state.Target(nil), remembered...)
		next.repeat.hasLast = true
		return
	}
	next.loopBound = true
	next.loopRemembered = append([]state.Target(nil), remembered...)
}

// finishResumption is the shared tail of resolveTop and resumeResolution: a
// fully resolved spell leaves the stack for the battlefield when it is a
// permanent (CR 608.3), otherwise to its resting zone (exile for a
// Flashback cast or a copy, the graveyard for the rest).
// ensureLeftTheStack then guards the same replacement-discarded-the-move
// corner both callers already guard, so a resolution can never leave its
// object resolving forever.
func (e *Engine) finishResumption(id state.ObjID) {
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		return // the continuation already moved it (or it ceased to exist).
	}
	if e.G.Obj(id).Ability != nil {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
		e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this resolved "+
			"ability's own move off the stack without relocating it anywhere; sent to exile "+
			"instead of re-resolving forever")
		return
	}
	e.moveResolvedOffStack(e.G.Obj(id))
}

// chosenModeLabels returns the human-facing labels in answer order.
func chosenModeLabels(chosen []decision.Option) []string {
	labels := make([]string, 0, len(chosen))
	for _, o := range chosen {
		labels = append(labels, o.Label)
	}
	return labels
}

// modeDecision builds the shared KModes option vocabulary used by spell
// announcement and triggered-ability placement. min and max are resolved by
// effects.CharmModeBounds against the caller's complete effects context, and
// repeat is its CanRepeatModes$ result: when set, the decision permits the
// same mode index more than once and max is NOT clamped to the distinct-mode
// count.
func modeDecision(p state.PlayerID, source state.ObjID, sa *cards.SA, svars map[string]string, min, max int, repeat bool) *decision.Decision {
	return modeDecisionForChoices(p, source, sa, svars, effects.CharmOf(sa).Modes, min, max, repeat)
}

// modeDecisionForChoices is modeDecision over an explicit eligible subset.
// Casting uses it to omit modes whose mandatory targets cannot be chosen;
// ResumeModes preserves the SVar vocabulary server-side while Index stays
// dense for the wire. repeat marks a CanRepeatModes$ Charm: the same eligible
// mode may fill several slots, and the max clamp is skipped so a CharmNum$
// larger than the eligible count is still satisfiable (by repetition).
func modeDecisionForChoices(p state.PlayerID, source state.ObjID, sa *cards.SA, svars map[string]string, choices []string, min, max int, repeat bool) *decision.Decision {
	if !repeat && max > len(choices) {
		max = len(choices)
	}
	d := &decision.Decision{Player: p, Kind: decision.KModes, Min: min, Max: max,
		Source: source, Repeatable: repeat,
		ResumeKind: "modes", ResumeSA: sa,
		ResumeModes: append([]string(nil), choices...),
		Prompt:      "Choose " + strconv.Itoa(min) + " to " + strconv.Itoa(max) + " mode(s)"}
	for i, name := range choices {
		d.Options = append(d.Options, decision.Option{
			Index: i, Kind: "mode", Label: effects.CharmModeLabel(cards.ResolveSVar(svars, name), name),
			Obj: source, Player: p})
	}
	return d
}

// modeLabels maps SVar names to their human-facing option labels. abortCast
// uses it for the reverse ModeChosen marker that accompanies restoring the
// pre-proposal ChosenModes cache.
func modeLabels(sa *cards.SA, svars map[string]string, names []string) []string {
	labels := make([]string, 0, len(names))
	for _, name := range names {
		labels = append(labels, effects.CharmModeLabel(cards.ResolveSVar(svars, name), name))
	}
	return labels
}

// modeChoiceNames maps the chosen modal options back to the SVar names of
// the Choices$ sub-abilities they pick, in the order chosen — the answer
// effCharm's re-entry reads (Ctx.Modes). eligible carries a filtered cast
// decision's server-only vocabulary; nil falls back to the SA's full Choices$
// list for placement and mid-resolution decisions. Out-of-range indices drop.
func modeChoiceNames(sa *cards.SA, chosen []decision.Option, eligible []string) []string {
	if sa == nil {
		return nil
	}
	choices := eligible
	if choices == nil {
		choices = effects.CharmOf(sa).Modes
	}
	names := make([]string, 0, len(chosen))
	for _, o := range chosen {
		if o.Index >= 0 && o.Index < len(choices) {
			names = append(names, choices[o.Index])
		}
	}
	return names
}

// moveResolvedOffStack is the shared tail of resolveTop and
// resumeResolution: a fully resolved spell leaves the stack for the
// battlefield when it is a permanent (CR 608.3), otherwise to its resting
// zone (exile for a Flashback cast or a copy, the graveyard for the rest).
// ensureLeftTheStack then guards the same replacement-discarded-the-move
// corner both callers already guard, so a resolution can never leave its
// object resolving forever.
func (e *Engine) moveResolvedOffStack(o *state.Object) {
	if o == nil || o.Zone != state.ZStack {
		return
	}
	id := o.ID
	if f := o.Face(); f != nil && f.IsPermanent() {
		// Morph / Megamorph / Disguise (CR 708.5): a face-down cast's spell
		// enters the battlefield face down. The entry marker re-carries the
		// same payload events.Apply folded onto the stack object at the
		// PutOnStack, so the battlefield entry takes the manifest decode's
		// face-down fold: CR 708.5's 2/2 colourless creature, with the cloak
		// marker adding the ward {2} a Disguise entry carries. The morph
		// flags (not the stack object's transient FaceDown bit) are the
		// gate, so a stack COPY of a face-down spell -- it inherits the
		// flags, the StackCopy way -- enters face down too, and every
		// ordinary permanent's entry stays byte-identical.
		if morph := o.CastFlags & (state.FlagMorphed | state.FlagMegamorphed | state.FlagDisguised); morph != 0 {
			marker := events.FaceDownEntryCounter
			if morph&state.FlagDisguised != 0 {
				marker = events.CloakEntryCounter
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZBattlefield, Counter: marker})
		} else {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZBattlefield})
		}
		// An as-enters choice parks this move through the mid-resolution ask
		// path. Keep the object on the stack until the answer re-emits it.
		if e.pending != nil || e.resume != nil {
			return
		}
		e.ensureLeftTheStack(id, spellRestZone(o), "an ETB replacement fully replaced this "+
			"permanent's entry to the battlefield without moving it anywhere; sent to its "+
			"resting zone instead of re-resolving forever")
		return
	}
	rest := spellRestZone(o)
	// CR 702.95a: a hand-cast Rebound spell is exiled as it resolves and
	// leaves a delayed promise to recast it at its controller's next upkeep.
	// Both the flag and the controller are captured before the MoveZone:
	// events.Apply resets CastFlags on the stack->exile move, and the emit
	// below runs synchronously. The registration is created only when the
	// card actually reached exile -- a replacement that redirected the move
	// leaves the promise uncreated, so no stale permission can outlive it.
	// FlagRebound is a cast-provenance bit, so a stack COPY -- put on the
	// stack, never cast (CR 707.10) -- carries none at the mint and registers
	// nothing. (No corpus card grants Rebound to a permanent, so the
	// permanent branch above's lack of a registration site stays
	// corpus-unreachable.)
	rebound := o.CastFlags&state.FlagRebound != 0
	controller := o.Controller
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: rest})
	if rebound && rest == state.ZExile {
		if cur := e.G.Obj(id); cur != nil && cur.Zone == state.ZExile {
			e.registerRebound(id, controller)
		}
	}
	e.ensureLeftTheStack(id, rest, "a replacement fully discarded this resolved "+
		"spell's own move off the stack without relocating it anywhere; sent to its "+
		"resting zone instead of re-resolving forever")
}

// registerRebound creates the CR 702.95a delayed trigger for a Rebound spell
// that was just exiled: at the controller's next upkeep, that player may cast
// the card from exile without paying its mana cost. The registration's source
// IS the exiled card, so the builtin __kwReboundCast body's Card.Self names
// it, and ValidZone$ Exile keeps the offer to the exile zone even if the card
// has left it (or later returns). ValidPlayer$ You gates the fire to the
// registration controller's own upkeep, and the one-shot DelayedPush consumes
// the registration on its first firing, so the permission never outlives the
// next upkeep.
func (e *Engine) registerRebound(id state.ObjID, controller state.PlayerID) {
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id, Player: controller,
		Step: state.StepUpkeep, Counter: "__kwReboundCast", Text: "Upkeep|VP=You"})
}

// payUnlessDamageCost lands the damage an accepting opponent chose to take
// from Sacrifice's damage-payment offer (Vexing Devil's "any opponent may
// have it deal 4 damage to them"). rules owns payment events, so the Damage
// event is emitted here — never in effects — exactly the split payMana's
// ManaAdd events already follow. The source is the offering permanent (the
// resolving ability object unwrapped to its source, the same rule
// effects.resolveSourceObject applies), published through SetDamageSource so
// the emit-side protection check (CR 702.16d) and DamageDone trigger
// matching see the real source, and the lifelink rider (CR 702.15a) is paid
// for its controller when the hit actually landed (a prevention or other
// replacement that substituted the event pays no life, the same gate
// rules/combat.go's rider uses).
func (e *Engine) payUnlessDamageCost(ctx *effects.Ctx, payer state.PlayerID, n int) {
	if n <= 0 {
		return
	}
	source := ctx.Source
	if o := e.G.Obj(source); o != nil && o.Ability != nil && o.Source != 0 {
		source = o.Source
	}
	keywords := e.damageKeywordsOf(source)
	controller := ctx.Controller
	if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield {
		controller = o.Controller
	} else if lki, ok := ctx.DamageSourceLKI[source]; ok {
		keywords = damageKeywordLKI{lifelink: lki.Lifelink, infect: lki.Infect,
			wither: lki.Wither, deathtouch: lki.Deathtouch}
		controller = lki.Controller
	}
	prev := e.SetDamageSource(source)
	dam := events.Event{Kind: events.Damage, Player: payer, Amount: int32(n)}
	if keywords.infect {
		dam.Counter = "infect"
	}
	ev := e.emit(dam)
	e.SetDamageSource(prev)
	if ev.Kind != events.Damage || !keywords.lifelink {
		return
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: controller, Amount: int32(n)})
}

// bindLoopFrames binds the pending ask and every continuation frame recorded
// this pass that no deeper RepeatEach already bound to remembered. votes is
// the AmountFromVotes$ tally the current loop frame carries (nil outside such
// a loop); it is bound to the same frames so a nested ask posed after the
// resume restores the table too.
func (e *Engine) bindLoopFrames(remembered []state.Target, votes []effects.VoteCount) {
	snap := append([]state.Target(nil), remembered...)
	if e.resume != nil && !e.resume.loopBound {
		e.resume.loopBound, e.resume.loopRemembered = true, snap
		e.resume.voteCounts = cloneVoteCounts(votes)
	}
	for i := range e.contChain {
		if !e.contChain[i].bound {
			e.contChain[i].bound, e.contChain[i].remembered = true, snap
			e.contChain[i].voteCounts = cloneVoteCounts(votes)
		}
	}
}

// charmModeScope and charmModeScopeSA read the per-mode Charm narrowing off
// the live resolution Ctx at ask time (effects.Ctx.CharmModeScope). Nil
// whenever the resolution is not inside a distinct modal Charm's mode.
func (e *Engine) charmModeScope() []state.Target {
	if e.resolutionCtx == nil {
		return nil
	}
	return append([]state.Target(nil), e.resolutionCtx.CharmModeScope...)
}

func (e *Engine) charmModeScopeSA() *cards.SA {
	if e.resolutionCtx == nil {
		return nil
	}
	return e.resolutionCtx.CharmModeSA
}

// applyCastModes records a CR 601.2b cast-time mode announcement (the
// "cast_modes" answer) on the proposed spell and re-enters continueCast. It is
// the answer handler's body, shared with castModeAsk's no-ask path: a modal
// spell whose only legal announcement is the empty one ("choose up to two"
// with no mode that has a legal target) announces zero modes without posting
// a decision nobody could answer differently.
func (e *Engine) applyCastModes(d *decision.Decision, player state.PlayerID, chosen []decision.Option) {
	pc := e.cast
	if pc == nil || pc.ability >= 0 || pc.stackObj == 0 {
		e.emit(events.Event{Kind: events.Note, Player: player,
			Text: "cast modes answered with no spell proposal pending"})
		return
	}
	names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes)
	labels := chosenModeLabels(chosen)
	// ChoiceRestriction$: log each announced mode on the SPELL object so a
	// later Charm of the same source sees the pick. A no-op unless the SA
	// carries the param.
	effects.RecordCharmChoices(e, pc.card, d.ResumeSA, names)
	if o := e.G.Obj(pc.stackObj); o != nil {
		if !pc.modeChosen {
			pc.preModes = state.CloneChosenModes(o.ChosenModes)
			pc.modeChosen = true
		}
		// Non-nil even for a zero-mode announcement: resolution must run
		// no mode, not re-pose the modal ask (state.CloneChosenModes).
		o.ChosenModes = append(make([]string, 0, len(names)), names...)
	}
	// CR 702.171b: a Spree/Tiered cast pays each chosen mode's own
	// ModeCost$ on top of the printed cost -- the same additional-cost
	// composition beginCast folds for Kicker, but per chosen mode and so
	// only known once the CR 601.2b mode answer is in. Folded into pc.cost
	// here (once; the guard survives a Clone) so the CR 601.2g mana window,
	// the cost modifiers and the final payment all see the composed total.
	// An unaffordable total aborts through the ordinary payment-reversal
	// path (CR 733.1) -- this branch never silently discounts or drops a
	// chosen mode.
	if !pc.modeCostsDone {
		pc.modeCostsDone = true
		pc.cost = pc.cost.Plus(modeCostTotal(e.G.Obj(pc.card).Face(), names))
	}
	// Escalate (the modal additional cost): a cast choosing N modes pays
	// the escalate cost N-1 times. Folded into pc.cost once, exactly like
	// the ModeCost$ fold above, so the tap/discard part asks the
	// continueCast re-entry below walks ask for the extra resources and
	// the payment window charges the composed total. An unpriceable
	// parameter (ParseCost's degraded Unknown tokens) is a loud no-charge,
	// never a fabricated generic. A one-mode cast folds nothing and stays
	// byte-identical.
	if pc.escalateSet && !pc.escalateDone && len(names) > 1 {
		pc.escalateDone = true
		if esc := ParseCost(pc.escalateParam); len(esc.Unknown) == 0 {
			for i := 1; i < len(names); i++ {
				pc.cost = pc.cost.Plus(esc)
			}
		} else {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				Text: "escalate cost unpriceable; casting without the escalate charge"})
		}
	}
	e.emit(events.Event{Kind: events.ModeChosen, Obj: pc.stackObj, Player: player,
		Text: strings.Join(labels, ",")})
	e.continueCast()
}

// isPlain reports whether a continuation report is an ordinary Resolve loop's
// (its frame resumes at sa.Sub) rather than one of the payload kinds that
// re-enter sa itself (repeat, charm/villainous/generic-choice/flip/token
// rests) or pose a deferred ask.
func (cf *contFrame) isPlain() bool {
	return !cf.askMarker && cf.deferredAsk == nil && cf.charmRest == nil && !cf.villainousRest &&
		!cf.genericChoiceRest && !cf.flipRest && cf.tokenRest == nil && cf.repeat == nil
}

// isRepeatBody reports whether cf is an api:Repeat loop frame
// (SuspendRepeatBody), as opposed to a RepeatEach one.
func (cf *contFrame) isRepeatBody() bool {
	return cf.repeat != nil && cf.repeat.body
}

// handsOnChainRemembered reports whether a completed frame's Ctx.Remembered
// is the chain's own, so it may be handed to an inheritsRemembered next
// frame: a frame that ran a chain (sa != nil) or re-bound the chain's
// captured Remembered (an as-enters or replacement-order answer, which runs
// no SA), outside a replacement body, and not one whose Remembered is a
// VillainousChoice victim or a GenericChoice chooser binding.
func (rp *resumePoint) handsOnChainRemembered() bool {
	return (rp.sa != nil || rp.remembered != nil) && !rp.replacement && !rp.villainousRememberedSet && rp.kind != "villainous" &&
		len(rp.villainousVictims) == 0 && len(rp.genericChoosers) == 0
}
