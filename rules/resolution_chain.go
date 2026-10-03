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
		if sub := cards.ResolveSVar(svars, name); sub != nil && effects.TargetsOf(sub).Targeted() {
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
		if e.pending != nil {
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

// charmModeScope and charmModeScopeSA read the per-mode Charm narrowing off
// the live resolution Ctx at ask time (effects.Ctx.CharmModeScope). Nil
// whenever the resolution is not inside a distinct modal Charm's mode.
func (e *Engine) charmModeScope() []state.Target {
	if e.resolutionCtx == nil {
		return nil
	}
	return append([]state.Target(nil), e.resolutionCtx.CharmModeScope...)
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
