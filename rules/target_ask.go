// target_ask.go holds the target-choice surface: who chooses (targetChooserCore,
// targetAskChooser, midChooserCore, ChooserFor), the opponent-picker asks, the
// ask/handle for a target decision, and the charm target recheck that runs when
// a copy resolves.
// Split out of stack.go by a pure move (no rename, no behaviour change).
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) targetChooserCore(controller state.PlayerID, remembered []state.Target, tc effects.TriggerContext, source state.ObjID, sa *cards.SA) (state.PlayerID, bool, bool) {
	if sa == nil {
		return controller, false, false
	}
	spec := effects.TargetsOf(sa).TargetingPlayer
	if spec == "" {
		return controller, false, false
	}
	if spec == "Opponent" || spec == "Player.Opponent" {
		// The rules-tier selection pin (answerOppPick's answer, consumed by
		// the re-posed ask): the answered ask matched on the same source
		// object and SA line.
		if s := e.oppSel; s.done && s.line == sa.Line && s.source == source {
			e.oppSel = oppSelectState{}
			return s.player, true, false
		}
		return e.opponentPicker(controller)
	}
	who, ok := e.targetChooserFromSpec(spec, controller, remembered, tc)
	return who, ok, false
}

// opponentPicker resolves the Opponent form's seat from the controller's
// living-opponent table: no living opponent fails closed to the controller, a
// sole living opponent answers directly (an ask nobody could answer
// differently is never posed), and two or more owe the controller's
// which-opponent selection (pick=true).
func (e *Engine) opponentPicker(controller state.PlayerID) (state.PlayerID, bool, bool) {
	opponents := e.livingOpponents(controller)
	switch len(opponents) {
	case 0:
		return controller, false, false
	case 1:
		return opponents[0], true, false
	}
	return controller, false, true
}

// livingOpponents lists the controller's living opponents in AliveFrom(0)
// turn order -- the same order the former deterministic first-opponent
// contract scanned, so a deterministic answerer (the bot's first-option
// clamp) still names the seat the old contract named.
func (e *Engine) livingOpponents(controller state.PlayerID) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.G.AliveFrom(0) {
		if p != controller {
			out = append(out, p)
		}
	}
	return out
}

// tpControlState is the TargetingPlayerControls$ restriction's resolution
// state (tpc1): Forge's `TargetingPlayerControls$ True` makes every target
// OBJECT of the SA a battlefield permanent controlled by the answering
// TargetingPlayer$ seat (Evangelize: "target creature of an opponent's
// choice they control"), without touching ValidTgts$'s own
// controller-relative reading (YouCtrl, hexproof, protection stay judged
// from the ability controller).
type tpControlState int

const (
	// tpNone: the SA carries no TargetingPlayerControls$ True -- candidates
	// and targets are judged exactly as before.
	tpNone tpControlState = iota
	// tpResolved: the answering seat is known -- the answered ask's record,
	// either tier's answered selection pin, a sole living opponent, a bound
	// trigger-relative referent, or the fail-closed controller.
	tpResolved
	// tpPending: the multi-opponent Opponent form's which-opponent selection
	// is still owed, so the answering seat is not known yet. The offer
	// census admits the union over the seats the selection may name (the
	// same set the selection ask offers), so the offer never lies about
	// feasibility; the eventual selection re-poses the ask with the exact
	// seat (the pin reads above) and an empty exact set falls to the
	// ordinary CR 608.2b/733.1 reversal machinery.
	tpPending
)

// tpCtlAnswer is one TargetingPlayerControls$ answered-ask record: the SA
// line whose ask was answered and the seat that answered it. Plain scalars,
// so Clone carries the store like the oppSel class.
type tpCtlAnswer struct {
	line   string
	player state.PlayerID
}

// targetControlsChooser resolves the TargetingPlayerControls$ restriction
// for a target census (candidatesForLimit's post-filter) or a CR 608.2b
// recheck (legalTargets). It shares the chooser derivation targetChooserCore
// owns -- the answered selection pins of both tiers, the sole-opponent and
// fail-closed shapes, the trigger-relative referent grammar -- plus the
// answered-ask record (tpCtlChooser), so the offer and the recheck cannot
// disagree about who "they" is (Critical C2's one-definition rule).
// p stays the ability CONTROLLER: target legality is judged from it exactly
// as the ValidTgts$ filter judges it; only the restriction's controller
// binding moves to the answering seat. The pending state is returned only
// for the Opponent form: every other TargetingPlayer$ referent resolves
// deterministically or fails closed.
func (e *Engine) targetControlsChooser(p state.PlayerID, source state.ObjID, sa *cards.SA) (state.PlayerID, tpControlState) {
	if sa == nil || !effects.TargetsOf(sa).Has(effects.TgtPlayerControls) {
		return 0, tpNone
	}
	// The answered ask's record first: once a target decision for this
	// (object, line) was answered, the answering seat is what every later
	// census and recheck reads -- the selection pins are consumed between
	// the ask and the recheck (the rules-tier one by the re-posed ask's own
	// targetChooserCore, the mid-tier one by OpponentPickAsk), so a pin
	// read must not be able to change the answer afterwards.
	if rec, ok := e.tpCtlChooser[source]; ok && (sa.Line == "" || rec.line == "" || rec.line == sa.Line) {
		return rec.player, tpResolved
	}
	spec := effects.TargetsOf(sa).TargetingPlayer
	if spec == "Opponent" || spec == "Player.Opponent" {
		// The mid-tier answered selection (oppPicksMid, keyed by the SA's
		// line): present between the "opp_pick" resume arm and the
		// synchronous re-entry that poses the target ask -- exactly the
		// window this census runs in (chosenTargetsFor builds its candidates
		// before ChooserFor/OpponentPickAsk consume the entry).
		if sa.Line != "" {
			if who, ok := e.oppPicksMid[sa.Line]; ok {
				return who, tpResolved
			}
		}
		// The rules-tier selection pin, read WITHOUT consuming it:
		// targetChooserCore owns the consume, at the same re-posed ask this
		// census has just filtered for (candidates are built before
		// targetAskChooser runs, so the pin is still set here).
		if s := e.oppSel; s.done && s.line == sa.Line && s.source == source {
			return s.player, tpResolved
		}
		opponents := e.livingOpponents(p)
		switch len(opponents) {
		case 0:
			// targetChooserCore's fail-closed shape: no living opponent keeps
			// the ask with the controller, so "they" is the controller.
			return p, tpResolved
		case 1:
			return opponents[0], tpResolved
		}
		return 0, tpPending
	}
	// Trigger-relative referents (TriggeredTarget, TriggeredPlayer, ...):
	// the same referent grammar targetChooserCore resolves through, against
	// the trigger context stored for the asking object. An unknown, unbound
	// or dead referent fails closed to the controller, the same shape
	// targetChooserCore keeps the ask with the controller for.
	var tc effects.TriggerContext
	if ctx, ok := e.triggerContexts[source]; ok {
		tc = ctx
	}
	who, ok := e.targetChooserFromSpec(spec, p, nil, tc)
	if !ok {
		return p, tpResolved
	}
	return who, tpResolved
}

// tpControlsAdmits judges one OBJECT target against the restriction state.
// The object must be a battlefield permanent (control exists nowhere else)
// and, once the answering seat is resolved, controlled by that seat. While
// the multi-opponent selection is still owed the census admits a permanent
// of any seat the selection may name -- the union over the controller's
// living opponents, the same set the selection ask offers. A non-battlefield
// object (a stack spell, a graveyard card) is never a legal target of a
// TargetingPlayerControls$ ask.
func (e *Engine) tpControlsAdmits(o *state.Object, p, chooser state.PlayerID, st tpControlState) bool {
	if st == tpNone {
		return true
	}
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	if st == tpResolved {
		return o.Controller == chooser
	}
	for _, q := range e.livingOpponents(p) {
		if o.Controller == q {
			return true
		}
	}
	return false
}

// filterTargetingPlayerControls applies TargetingPlayerControls$ True to a
// target census (tpc1): every OBJECT candidate must be a battlefield
// permanent controlled by the answering TargetingPlayer$ seat. Player
// candidates are out of the parameter's scope and stay. It runs last in
// candidatesForLimit's post-filter chain and only on the targeting census
// (the Overload affected sweep keeps its non-target semantics).
func (e *Engine) filterTargetingPlayerControls(out []targetCandidate, sa *cards.SA, p state.PlayerID, source state.ObjID) []targetCandidate {
	if len(out) == 0 {
		return out
	}
	chooser, st := e.targetControlsChooser(p, source, sa)
	if st == tpNone {
		return out
	}
	filtered := out[:0]
	for _, c := range out {
		if c.kind == "player" || e.tpControlsAdmits(e.G.Obj(c.obj), p, chooser, st) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// recordTpControlsChooser pins the seat that ANSWERED a target ask whose SA
// carries TargetingPlayerControls$ True (tpc1), keyed by the RESOLVING
// stack object -- the spell object for a cast (pc.stackObj), the
// AbilityPush-minted object for an activation, the TriggerPush object for a
// placement ask. The CR 608.2b recheck (legalTargets) reads the same key
// through its self parameter, so the restriction is judged against exactly
// the seat the ask was answered by, never re-derived past the selection
// pins' short lifetimes. The entry lives from the answer to the object
// leaving the stack (the zone-change clear), so the next targeting cycle on
// a fresh object never sees a stale seat.
func (e *Engine) recordTpControlsChooser(obj state.ObjID, sa *cards.SA, chooser state.PlayerID) {
	if sa == nil || obj == 0 || !effects.TargetsOf(sa).Has(effects.TgtPlayerControls) {
		return
	}
	if e.tpCtlChooser == nil {
		e.tpCtlChooser = make(map[state.ObjID]tpCtlAnswer)
	}
	e.tpCtlChooser[obj] = tpCtlAnswer{line: sa.Line, player: chooser}
}

// targetAskChooser resolves who answers a target ask declared by sa at a
// rules-tier ask site. Forge's TargetingPlayer$ names another player as the
// chooser; the trigger-relative grammar in targetChooserFromSpec resolves
// those referents from the stored trigger context, while the non-triggered
// cast/activation form names an opponent (with the multi-opponent selection
// ask owed by the controller -- see targetChooserCore's pick return). It
// delegates to targetChooserCore, the shared home, and is called by askTarget
// (trigger placement and resolution sub-abilities, this file), targetAsk (CR
// 601.2c cast and activation targeting, rules/cast.go) and subTargetAsk
// (chained sub-abilities). The effects-tier mid-resolution asks reach the
// same core through Engine.ChooserFor and Engine.OpponentPickAsk.
//
// Returns (controller, false, false) when sa names no chooser, or the spec
// is unknown, unbound or dead, so the ask stays with the controller.
func (e *Engine) targetAskChooser(controller state.PlayerID, source state.ObjID, sa *cards.SA) (state.PlayerID, bool, bool) {
	var tc effects.TriggerContext
	if triggerContext, ok := e.triggerContexts[source]; ok {
		tc = triggerContext
	}
	return e.targetChooserCore(controller, nil, tc, source, sa)
}

// chooseOppPick is the chooseFor for the TargetingPlayer$ Opponent
// controller-selection ask (poseOpponentPick). Keep its value distinct from
// every other chooseFor flow; handleChoose dispatches on these values.
const chooseOppPick chooseFor = 47

// oppPickStage names which rules-tier flow posed a TargetingPlayer$
// Opponent selection ask, so answerOppPick re-poses the right target ask.
type oppPickStage string

const (
	// oppPickTarget: the askTarget site (trigger placement, resolution-sub
	// placement, handleModes' modal sub asks).
	oppPickTarget oppPickStage = "target"
	// oppPickCastRoot: the CR 601.2c root cast/activation ask (targetAsk).
	oppPickCastRoot oppPickStage = "cast_root"
	// oppPickCastSub: the chained sub-ability cast-time pre-ask
	// (subTargetAsk, ResumeKind "cast_sub").
	oppPickCastSub oppPickStage = "cast_sub"
)

// oppSelectState is the engine-side record of an outstanding or answered
// TargetingPlayer$ Opponent selection ask at a rules-tier site: set by
// poseOpponentPick when the controller's which-opponent ask is posted,
// flipped to done by answerOppPick when the controller names a seat, and
// consumed by targetChooserCore when the re-posed target ask reads it. Plain
// scalars only, so Clone carries it like the blockerRound class and a
// snapshot boundary mid-ask re-poses the same selection.
type oppSelectState struct {
	stage  oppPickStage
	ctl    state.PlayerID
	source state.ObjID
	line   string
	done   bool
	player state.PlayerID
}

// oppPicksMid carries the mid-resolution tier's answered selections, keyed by
// the asking SA's line: the "opp_pick" resume arm (rules/resolution.go)
// records the controller's chosen opponent there and the re-entered walk's
// ChooserFor consumes it when it re-derives the chooser. Lines are unique
// per SVar body, the pin only lives between the arm and the synchronous
// re-entry that reads it, and the read deletes it, so no entry can outlive
// the ask it belongs to.

// poseOpponentPick poses the controller's which-opponent selection ask for a
// TargetingPlayer$ Opponent target ask at a rules-tier site: one "player"
// option per living opponent, Min/Max 1, answered by the CONTROLLER (never
// by a target candidate). The caller has already established that the target
// ask itself would be posted (feasibility ran first), so the selection ask
// replaces it one-for-one and the target ask is re-posed by answerOppPick
// once the seat is named. The pending KChoose is routed through the
// chooseOppPick flow marker (handleChoose), NOT the mid-resolution resume
// machinery: a cast begun inside a suspended resolution (a Miracle cast in
// the trigger drain) parks the resolution's own resume point, and a resume
// dispatch would consume the selection answer as the resolution's ask
// answer.
func (e *Engine) poseOpponentPick(controller state.PlayerID, source state.ObjID, sa *cards.SA, stage oppPickStage) {
	d := &decision.Decision{Player: controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: source, ResumeKind: "opp_pick", ResumeSA: sa,
		Prompt: "Choose which opponent answers the target ask for " + e.targetName(source)}
	for _, p := range e.livingOpponents(controller) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "player", Label: e.G.Players[p].Name, Player: p})
	}
	e.oppSel = oppSelectState{stage: stage, ctl: controller, source: source, line: sa.Line}
	e.choosing = chooseOppPick
	e.ask(d)
}

// answerOppPick applies the controller's answered selection ask: the chosen
// "player" option names the opponent who answers the re-posed target ask.
// The selection rides e.oppSel (done) back to the asking site's re-entry,
// which consumes it. An empty or malformed answer cannot legally arrive
// (Min 1 / Max 1, validated before handle), so the conservative fallback is
// the old deterministic contract -- the first living opponent in AliveFrom(0)
// turn order -- which can never strand the flow.
func (e *Engine) answerOppPick(d *decision.Decision, chosen []decision.Option) {
	st := e.oppSel
	e.oppSel = oppSelectState{}
	e.choosing = chooseNone
	if len(chosen) > 0 && chosen[0].Kind == "player" {
		st.player = chosen[0].Player
	} else {
		// Malformed answer (the ask is Min 1 / Max 1, so this is the
		// conservative read): fall back to the deterministic first living
		// opponent in AliveFrom(0) turn order.
		opp := e.livingOpponents(st.ctl)
		if len(opp) == 0 {
			return
		}
		st.player = opp[0]
	}
	st.done = true
	e.oppSel = st
	switch st.stage {
	case oppPickTarget:
		e.askTarget(st.ctl, st.source, d.ResumeSA)
	case oppPickCastRoot:
		e.continueCast()
	case oppPickCastSub:
		if pc := e.cast; pc != nil && !e.postTargetAsks(pc) {
			// The selected seat's ask needed no decision after all (its pool
			// emptied on a Min-0 link): nothing else is outstanding, so the
			// announcement is complete and the cast pays -- the same tail the
			// cast_sub answer takes. Without it the proposal would be left
			// parked with no decision pending.
			e.finishTargetedCast(pc, pc.player)
		}
	}
}

// midChooserCore is the effects-tier arm of targetChooserCore: the
// mid-resolution ValidTgts$ asks (effects.chosenTargetsFor's "tgts" ask and
// effects.changeZoneChosenTargets' "choice" ask) consult it through
// ChooserFor/OpponentPickAsk. It reads the mid tier's answered selection
// (the "opp_pick" resume arm's pin, keyed by the SA's line) before the
// shared core, so a multi-opponent Opponent form resolves to the seat the
// controller named on the re-entered walk.
func (e *Engine) midChooserCore(c *effects.Ctx, sa *cards.SA) (state.PlayerID, bool, bool) {
	if sa != nil && sa.Line != "" {
		if p, ok := e.oppPicksMid[sa.Line]; ok {
			// Do not consume here: effects asks call ChooserFor first and
			// OpponentPickAsk second. The latter owns consumption after both
			// seams have observed the same selected seat.
			return p, true, false
		}
	}
	// A pre-captured ChangeZone target ask resumes at its enclosing Effect
	// root. The answer is consequently keyed by that root's line, while the
	// re-entered chooser lookup still receives the ChangeZone child SA.
	if c != nil && c.TargetAskResume != nil && c.TargetAskResume.Line != "" {
		if p, ok := e.oppPicksMid[c.TargetAskResume.Line]; ok {
			return p, true, false
		}
	}
	if c == nil {
		return 0, false, false
	}
	return e.targetChooserCore(c.Controller, c.Remembered, c.TriggerContext, c.Source, sa)
}

// ChooserFor implements effects.Host's chooser seam for the mid-resolution
// ValidTgts$ asks (effects.chosenTargetsFor and effects.changeZoneChosenTargets).
// Those asks run below the rules tier and cannot import it, so the resolver
// arrives through this hook instead; c carries the controller, source,
// remembered set and trigger context the effects-side ask already holds. An
// absent chooser (or an unknown/unbound/dead referent) keeps c.Controller,
// the same fail-closed default every rules-tier ask uses. The multi-opponent
// Opponent form's selection ask is NOT posed here -- this hook is a
// synchronous read inside a running walk; the asking site calls
// OpponentPickAsk first, which may suspend the walk on the controller's
// selection ask, and ChooserFor then only ever answers the already-selected
// (or sole-opponent) form.
func (e *Engine) ChooserFor(c *effects.Ctx, sa *cards.SA) state.PlayerID {
	if who, ok, pick := e.midChooserCore(c, sa); ok && !pick {
		return who
	}
	if c == nil {
		return 0
	}
	return c.Controller
}

// OpponentPickAsk is the effects.Host seam for the multi-opponent
// TargetingPlayer$ Opponent selection at a mid-resolution ask site
// (chosenTargetsFor / changeZoneChosenTargets, which are mid-walk and can
// suspend). With two or more living opponents and no answered selection it
// poses the controller's which-opponent ask through Engine.Ask -- the
// ordinary mid-resolution resume machinery, so the walk parks on it and the
// answer re-enters this very SA (the "opp_pick" resume arm records the pin
// midChooserCore reads) -- and reports posed=true; the caller returns a
// handled-nil set and stops before the body. Every other shape reports
// posed=false with the seat that answers the target ask: the pinned or sole
// living opponent, or c.Controller when the resolver fails closed (and for
// a host that has no resolver at all -- the effects test double, which never
// reaches this method -- the caller keeps plain ChooserFor).
func (e *Engine) OpponentPickAsk(c *effects.Ctx, sa *cards.SA) (state.PlayerID, bool) {
	who, ok, pick := e.midChooserCore(c, sa)
	if !pick {
		if sa != nil && sa.Line != "" {
			delete(e.oppPicksMid, sa.Line)
		}
		if c != nil && c.TargetAskResume != nil {
			delete(e.oppPicksMid, c.TargetAskResume.Line)
		}
		if ok {
			return who, false
		}
		if c == nil {
			return 0, false
		}
		return c.Controller, false
	}
	resumeSA := sa
	if c.TargetAskResume != nil {
		// An Effect may pre-capture the target of its following ChangeZone
		// sub before registering a replacement scoped to that target. The
		// opponent-selection ask is part of that same pre-capture: resume the
		// Effect root so it registers with the chosen target before the child
		// ChangeZone can move it. Keep `sa` for the chooser lookup above, which
		// is specific to the ChangeZone target's TargetingPlayer$ parameter.
		resumeSA = c.TargetAskResume
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "opp_pick", ResumeSA: resumeSA,
		Prompt: "Choose which opponent answers the target ask for " + e.targetName(c.Source)}
	for _, p := range e.livingOpponents(c.Controller) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "player", Label: e.G.Players[p].Name, Player: p})
	}
	e.Ask(d)
	return 0, true
}

// askTarget offers every legal target for a spell or ability. It deliberately
// retains the post-push insufficient-target backstop: modal and dynamic target
// counts are not rejected by the earlier cast-offer census.
func (e *Engine) askTarget(p state.PlayerID, source state.ObjID, sa *cards.SA) {
	min, max := e.resolvedTargetBounds(p, source, sa, 0)
	if max == 0 {
		// A dynamic bound RESOLVED to zero: this stage takes no targets, so
		// pose no ask (the effects-side askTargets has the same max <= 0 arm).
		return
	}
	candidates := e.legalTargetCandidates(p, source, source, sa)
	// MaxTotalTargetPower$ (Reunion of the House): prune the candidates that
	// can provably join no legal selection (individually over the cap unless
	// a negative-power candidate could offset them) and carry the running
	// cap as the decision's cumulative budget. Read BEFORE the per-controller
	// bounds so `distinct` counts only candidates a legal selection can
	// still take.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, p, source, sa, 0)
	candidates, cmcCap, cmcCapped := e.totalCMCCappedCandidates(candidates, p, source, sa, 0)
	min, max, exclusive, distinct := e.oneEachTargetBounds(sa, candidates, min, max)
	min, max, sameCapacity, sameController := e.sameControllerTargetBounds(sa, candidates, min, max)
	min, max, setCapacity, setMode, setKind := e.setPropTargetBounds(sa, candidates, min, max)
	chooser := p
	pickOwed := false
	if who, ok, pick := e.targetAskChooser(p, source, sa); pick {
		pickOwed = true
	} else if ok {
		chooser = who
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KTarget, Min: min, Max: max,
		Prompt: "Choose a target for " + e.targetName(source),
		Source: source, TargetEffect: e.describeTargetEffect(p, source, sa, 0),
		TargetsWithSameController: sameController, SetPropMode: setMode, ResumeSA: sa}
	for _, candidate := range candidates {
		// targetOptionLabel tolerates the Face-less ability object a
		// TargetType$ Activated/Triggered spec now offers: targetName falls
		// back to the source permanent's name.
		label := e.targetOptionLabel(candidate)
		o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: label, Obj: candidate.obj, Player: candidate.player}
		o.Group = e.targetControllerGroup(sa, candidate)
		o.Controller = e.candidateControllerSeat(candidate)
		o.SetProps = e.setPropTokensFor(setKind, candidate)
		// Option.Value is omitempty and read only under a budget
		// (Decision.HasBudget), so a budget-less target ask keeps its wire
		// payload byte-identical. Every present cap -- zero and negative
		// included, via Decision.Budgeted -- rides the wire, so
		// Decision.Validate enforces the total on every submitted answer.
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(e.Power(candidate.obj))
			}
		}
		if cmcCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				if powerCapped {
					o.Value2 = int(co.Face().ManaValue())
				} else {
					o.Value = int(co.Face().ManaValue())
				}
			}
		}
		d.Options = append(d.Options, o)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	if cmcCapped {
		if powerCapped {
			d.MaxSum2, d.Budgeted2 = cmcCap, true
		} else {
			d.MaxSum, d.Budgeted = cmcCap, true
		}
	}
	if min == 0 {
		// Requirement N2 / totality: a target-hungry subject whose minimum
		// is zero resolves untargeted when NO legal target exists. When at
		// least one exists it is still offered (Min 0 lets the chooser take
		// none); a zero-option decision would strand the cast, never asked.
		if len(d.Options) == 0 {
			return
		}
	} else if len(d.Options) < min || (exclusive && min > distinct) || (sameController && min > sameCapacity) || (setMode != decision.SetPropNone && min > setCapacity) {
		// A target-hungry subject with fewer legal targets than Min -- or one
		// whose per-controller constraint admits fewer distinct controllers
		// than Min (exclusive && min > distinct: two mandatory targets, both
		// controlled by one player) -- uses CR
		// 608.2b's existing counter/fizzle exit: an immediate move to its
		// normal resting place (exile instead of the graveyard for a
		// Flashback cast, and for a triggered ability object -- which has no
		// graveyard -- exile per CR 608.2m, same as every ability fizzle in
		// resolveTop).
		rest := spellFizzleZone(e.G.Obj(source))
		if o := e.G.Obj(source); o != nil && o.Ability != nil {
			rest = state.ZExile
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: source,
			From: state.ZStack, To: rest, Text: "countered: no legal targets"})
		e.ensureLeftTheStack(source, rest, "a replacement fully discarded this "+
			"spell's 'countered: no legal targets' move without relocating it anywhere; sent "+
			"to its resting zone instead of re-resolving forever")
		// Ruling T14-e: p, the casting player, not e.G.Active -- CR 117.3c,
		// the caster keeps priority even when it fizzles. Only a SPELL keeps
		// priority this way: an ability fizzling is parked in exile (ceases
		// to exist) mid-drain, and handing out priority from inside the
		// trigger drain would double-emit against the drain's own tail.
		if o := e.G.Obj(source); o == nil || o.Ability == nil {
			e.emit(events.Event{Kind: events.Priority, Player: p, Amount: 0})
		}
		return
	}
	if pickOwed {
		// The multi-opponent Opponent form: the controller first names WHICH
		// opponent answers. The selection ask is posed only here, after the
		// feasibility census has established the target ask would be posted
		// (a fizzle above never owed a selection); answerOppPick re-poses
		// this very ask with the named seat.
		e.poseOpponentPick(p, source, sa, oppPickTarget)
		return
	}
	// TargetsAtRandom$: the engine draws the targets (effects.RandomTargetsAsk
	// owns the class for every target ask site).
	effects.RandomTargetsAsk(e, d, sa)
	e.ask(d)
}

// handleTarget records the chosen target(s) via TargetsChosen events (Ruling
// T14-b) rather than writing state.Object.Targets directly: apply.go clears
// Targets on a zone change but nothing else ever set it, so a direct write
// here would leave a replayed game with no targets while the live game had
// them. N targets record as TargetMin$ asks for: the first option replaces
// with targets (TargetsChosen shape 0 for an object, 1 for a player), each
// further option appends one (shape 2 object, 3 player) -- see apply.go's
// TargetsChosen case and its test TestTargetsChosenAppendShapes.
func (e *Engine) handleTarget(d *decision.Decision, in decision.Intent) {
	chosen := d.Chosen(in)
	if d.ResumeKind == "copy_targets" {
		return
	}
	if d.ResumeKind == "charm_targets" {
		groups, ordered := charmTargetGroups(d, chosen)
		if e.cast != nil {
			pc := e.cast
			pc.charmTargets = groups
			stageBase := len(pc.targets)
			pc.targets = append(pc.targets, targetOptions(ordered)...)
			e.repriceForTargets(pc)
			if !pc.isAbility() {
				if pc.stackObj != 0 {
					e.recordChosenTargets(pc.stackObj, ordered, stageBase > 0)
				}
				e.payCast()
			} else {
				pc.rootOpts = append([]decision.Option(nil), ordered...)
				e.payCast()
				if pc.stackObj != 0 {
					e.recordChosenTargets(pc.stackObj, ordered, false)
					e.cast = pc
					e.fireManaSpentTriggers(pc.activationPushEvent(e), nil)
					e.cast = nil
				}
			}
		} else {
			if e.charmTargets == nil {
				e.charmTargets = make(map[state.ObjID][][]state.Target)
			}
			e.charmTargets[d.Source] = groups
			e.recordChosenTargets(d.Source, ordered, false)
		}
		if e.drainAwaitsTarget {
			e.drainAwaitsTarget = false
			e.resumeTriggerDrain()
		} else if e.pending == nil {
			e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
		}
		return
	}
	if d.ResumeKind == "charm_mode_seq" {
		e.charmSeqAnswer(d, in, chosen)
		return
	}
	if d.ResumeKind == "trig_sub" {
		// A triggered ability's chain-link target, announced at placement
		// (CR 603.3d, rules/trigger_subtargets.go).
		e.answerTriggerSubTarget(in, chosen)
		return
	}
	// A cast-flow target decision (CR 601.2c, asked by targetAsk after the
	// object was pushed by pushCast but BEFORE any cost is paid): completing
	// it means recording the chosen targets onto the stack object and then
	// committing the transaction -- paying the costs and firing the cast
	// trigger -- via payCast. For a spell the stack object is the card
	// itself, already on the stack (pushCast), so targets are recorded before
	// payment (601.2c before 601.2h); for an activated ability the stack
	// object is minted by payCast's AbilityPush, so targets are recorded
	// AFTER it. Targets are never written directly (they go through
	// TargetsChosen events) and always after the push, because a zone change
	// clears them.
	if e.cast != nil {
		pc := e.cast
		// A chained sub-ability's cast-time pre-ask answer (alltargeted1): the
		// KTarget decision posed by subTargetAsk. Record against the stage it
		// was asked for (subStage indexes both), re-price (the union grew, so
		// a target-dependent ReduceCost$ may now apply), then pose the next
		// outstanding post-target ask or finish the payment.
		if d.ResumeKind == "cast_sub" {
			e.answerCastSubTarget(pc, chosen)
			if e.postTargetAsks(pc) {
				return
			}
			e.finishTargetedCast(pc, pc.player)
			return
		}
		// A Fuse cast may ask targets twice (front, then alternate). Append
		// rather than replace so the stack object's flat target list carries
		// both halves, and stageBase records whether an earlier half's
		// targets are already recorded (its first target must then APPEND).
		stageBase := len(pc.targets)
		pc.targets = append(pc.targets, targetOptions(chosen)...)
		// A Fuse cast records the stage's own slice (review MAJOR 1): the flat
		// list on the stack object cannot express which half chose which
		// target, and re-deriving the split through each half's ValidTgts spec
		// mis-assigns every target that half's spec merely overlaps (Turn //
		// Burn's Creature vs Any). Indexed by pc.targetStage, so a targetless
		// stage the ask loop skipped never misaligns the slices.
		if pc.mode == "fuse" {
			for len(pc.stageTargets) <= pc.targetStage {
				pc.stageTargets = append(pc.stageTargets, nil)
			}
			pc.stageTargets[pc.targetStage] = append(pc.stageTargets[pc.targetStage], targetOptions(chosen)...)
		}
		e.repriceForTargets(pc)
		if !pc.isAbility() {
			if pc.stackObj != 0 {
				e.recordChosenTargets(pc.stackObj, chosen, stageBase > 0)
			}
			// CR 702.101b: after the front half's targets, ask the alternate
			// half's before payment. targetAsk skips a targetless half and the
			// cast pays once every target stage is settled.
			if e.castHasNextTargetStage(pc, e.G.Obj(pc.card)) {
				pc.targetStage++
				if e.targetAsk() {
					return
				}
			}
		} else {
			// The ability object does not exist until payCast's AbilityPush, so
			// the root answer's options ride pc.rootOpts for the payment tail's
			// recordChosenTargets; the post-target stages (alltargeted1's sub
			// pre-asks, the CollectEvidence ask) run BEFORE payment (CR 601.2c).
			pc.rootOpts = append([]decision.Option(nil), chosen...)
		}
		// alltargeted1: the post-target announcement stages -- the chain sub
		// pre-asks first, then the CollectEvidence ask whose amount reads the
		// union -- park the same way the root ask did, before any cost is paid.
		if e.postTargetAsks(pc) {
			return
		}
		e.finishTargetedCast(pc, pc.player)
		// tpc1: pin the answering seat against the resolving object. For a
		// spell pc.stackObj is already the pushed object; for an activation
		// payCast's AbilityPush has just minted it. The CR 608.2b recheck
		// (legalTargets) reads this record off the resolving object.
		if po := e.G.Obj(pc.card); po != nil {
			if tsa := e.castStageSA(pc, po, po.Face()); tsa != nil {
				e.recordTpControlsChooser(pc.stackObj, tsa, in.Player)
			}
		}
		return
	}
	e.recordTpControlsChooser(d.Source, d.ResumeSA, in.Player)
	e.recordChosenTargets(d.Source, chosen, false)
	// The root's answer opens a placed trigger's chain-link announcement
	// (CR 603.3d); the last link's answer resumes the drain instead.
	if e.trigSub != nil && e.trigSub.obj == d.Source && e.askTriggerSubTargets() {
		return
	}
	// A target decision asked by a trigger drain (putTriggersOnStack's
	// pushTrigger, immediately after the trigger's TriggerPush -- Task 20's
	// checkTriggers never asked targets, so only a spell's cast-time ask
	// reached here before) does not hand out priority: the drain's own tail
	// does, through the SAME continuation handleTriggerOrder uses
	// (resumeTriggerDrain, turn.go), so a second, later trigger is still
	// placed before any player acts. A spell's cast-time target decision
	// keeps its caster's priority (CR 117.3c) via the emit below.
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
		return
	}
	// Ruling T14-e: the submitting player, not e.G.Active -- CR 117.3c, the
	// player who chose the target (the caster) keeps priority.
	e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
}

// targetOptions converts a target decision's selected options to the
// proposal-local target representation used while its cost is still being
// assembled. Events remain the source of truth once the stack object exists;
// this short-lived copy is only what lets an activated ability evaluate a
// ValidTarget$ cost modifier before its AbilityPush object is minted.
func targetOptions(chosen []decision.Option) []state.Target {
	out := make([]state.Target, 0, len(chosen))
	for _, opt := range chosen {
		if opt.Kind == "player" {
			out = append(out, state.Target{Player: opt.Player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: opt.Obj})
		}
	}
	return out
}

// recordChosenTargets emits the TargetsChosen events for a set of chosen
// target options onto the given object. Amount discriminates the target shape
// (Ruling T14-b, extended by Task 4): 0 replace-with-object, 1
// replace-with-player, 2 append-object, 3 append-player. It is the shared
// recording path for both a cast-flow target decision (onto the stack object,
// after the push) and a triggered ability's own post-TriggerPush ask.
// appendFirst makes even the first option an APPEND: a Fuse cast records the
// front half's targets first and must not have the alternate half's first
// target replace them on the stack object.
func (e *Engine) recordChosenTargets(targetObj state.ObjID, chosen []decision.Option, appendFirst bool) {
	// One target answer is one BATCH for Mode$ BecomesTargetOnce (Forge fires
	// TriggerBecomesTargetOnce once per targeting action, not once per
	// target): the latch is open for exactly the events this loop emits, so a
	// second matching target of the same answer is absorbed rather than
	// queueing a second instance. The bracket never spans a drain -- emit only
	// appends to pendingTriggers -- so the queue stays append-only across it.
	// Batch identity is this CALL, not the ability: a (hypothetical) ability
	// that records its targets in two separate recordChosenTargets calls opens
	// two batches and fires the watcher twice, where Forge fires once per
	// ability. No corpus card takes a multi-call path with a BecomesTargetOnce
	// watcher today; if one arrives the bracket must move up to the ability.
	e.openTargetBatch()
	defer e.closeTargetBatch()
	for i, opt := range chosen {
		ev := events.Event{Kind: events.TargetsChosen, Obj: targetObj}
		appendThis := i > 0 || appendFirst
		if opt.Kind == "player" {
			// shape 1 replace / shape 3 append a single player target.
			ev.Amount = 1
			if appendThis {
				ev.Amount = 3
			}
			ev.Player = opt.Player
		} else {
			// shape 0 replace / shape 2 append one object target.
			if appendThis {
				ev.Amount = 2
			}
			ev.IDs = []state.ObjID{opt.Obj}
		}
		e.emit(ev)
	}
}

// resolveTop resolves the object on top of the stack and moves it to
// wherever it goes next.
//
// CR 608.2b: before anything else runs, every target recorded when this
// spell or ability was put on the stack is rechecked against legalTargets
// below -- a target legal when chosen can stop being legal by the time its
// spell reaches the top of the stack, most commonly a creature that died to
// something else in the meantime. With no legal target left, this does not
// resolve at all: it leaves the stack (a spell to its owner's graveyard, an
// ability to exile per CR 608.2m just below) with no effect -- not even a
// SubAbility chained onto it that names no target of its own (Defined$ You
// and the like). That distinguishes this from the per-target nil-checks
// primitives like effDealDamage already had (Task 18, effects/damage.go):
// those already skip a target that individually vanished, but nothing
// before this stopped the OTHER, untargeted parts of the same spell's
// script from running anyway once every target it had was gone. With only
// some targets still legal, resolution proceeds against exactly that
// narrowed set -- CR 608.2b's "resolves, doing as much as possible".
func cloneCharmTargetGroups(in [][]state.Target) [][]state.Target {
	if in == nil {
		return nil
	}
	out := make([][]state.Target, len(in))
	for i, group := range in {
		out[i] = append([]state.Target(nil), group...)
	}
	return out
}

// recheckCharmTargets applies CR 608.2b independently to each selected
// distinct mode's target declaration. The flat target list remains available
// for legacy consumers, while the returned groups are what effCharm binds to
// each mode.
func (e *Engine) recheckCharmTargets(o *state.Object) ([][]state.Target, []state.Target, bool) {
	if o == nil || e.charmTargets == nil || e.charmTargets[o.ID] == nil || len(o.ChosenModes) == 0 {
		return nil, nil, false
	}
	var root *cards.SA
	var svars map[string]string
	if o.Ability != nil {
		src := e.G.Obj(o.Source)
		if src == nil || src.Face() == nil {
			return nil, nil, false
		}
		root, svars = o.Ability, src.Face().SVars
	} else {
		f := o.Face()
		if f == nil {
			return nil, nil, false
		}
		root, svars = f.SpellAbility(), f.SVars
	}
	slots := charmTargetSlots(svars, root, o.ChosenModes)
	groups := e.charmTargets[o.ID]
	if len(slots) != len(groups) {
		return nil, nil, false
	}
	checked := make([][]state.Target, len(groups))
	var flat []state.Target
	anyLegal := false
	source := o.ID
	if o.Ability != nil {
		source = o.Source
	}
	for i, name := range slots {
		sub := cards.ResolveSVar(svars, name)
		legal := e.legalTargets(groups[i], sub, targetZones(sub), o.Controller, source, o.ID)
		checked[i] = legal
		if len(legal) > 0 {
			anyLegal = true
		}
		flat = append(flat, legal...)
	}
	return checked, flat, anyLegal
}

// offeredTargetSA is the SA whose ValidTgts$ targeting the placement or
// announcement ask covered for this stack object, derived exactly as the
// TargetsOffered marker's derivation in resolveTop: the ability SA itself
// for a non-modal trigger or activated ability (pushTrigger's askTarget),
// the first target-bearing CHOSEN MODE's sub for a modal one (handleModes'
// placement branch asks the mode sub and skips the outer ask entirely), and
// the spell's target declaration (targetAsk's targetSA -- the announced
// mode's for a modal spell) for a spell. nil when none of those declares
// targets. Shared by resolveTop's two branches and resumeResolution so the
// generic ValidTgts$ pre-ask (effects' chosenTargetsFor) skips exactly the
// covered SA on the first pass AND on every resume re-entry -- an optional
// trigger's yes re-enters through resumeResolution, where the first pass's
// bool marker alone is not carried (task mvts1).
func offeredTargetSA(o *state.Object, svars map[string]string) *cards.SA {
	if o.Ability != nil {
		if len(o.ChosenModes) > 0 && effects.CharmOf(o.Ability).HasChoices {
			for _, name := range o.ChosenModes {
				if sub := cards.ResolveSVar(svars, name); sub != nil &&
					effects.TargetsOf(sub).Targeted() {
					return sub
				}
			}
			return nil
		}
		if effects.TargetsOf(o.Ability).Targeted() {
			return o.Ability
		}
		return nil
	}
	f := o.Face()
	if f == nil {
		return nil
	}
	sa := f.SpellAbility()
	if sa == nil {
		return nil
	}
	targetSA := modalTargetSA(f, sa, o.ChosenModes)
	if targetSA != nil && effects.TargetsOf(targetSA).Targeted() {
		return targetSA
	}
	return nil
}

// resolvedAbilityTally is the Count$ResolvedThisTurn read for a resolving
// stack object: the per-ability tally events.Apply folded on this resolution's
// Resolve event, keyed by (source permanent, root Ability$ body) content. It is
// the ONE home resolveTop's ability branch and resumeResolution's Ctx rebuild
// call, so a suspended-then-resumed chain cannot read a different ordinal than
// its first pass. Returns 0 for a spell (no Ability) and for a source that has
// already left, the modelled-head zero the effects case gives.
func (e *Engine) resolvedAbilityTally(o *state.Object) int32 {
	if o == nil {
		return 0
	}
	return e.resolvedAbilityTallyFor(o.Source, o.Ability)
}

// resolvedAbilityTallyFor is resolvedAbilityTally's core, shared with
// resolveAbility (which resolves an SA directly and so has no stack object to
// hand it). A nil SA is a spell or a synthetic resolution with no ability
// identity -- a zero.
func (e *Engine) resolvedAbilityTallyFor(source state.ObjID, sa *cards.SA) int32 {
	if sa == nil {
		return 0
	}
	return events.ResolvedThisTurnOf(e.G, source, sa)
}

// activationsThisTurnFor is the ConditionActivationLimit$ read
// (Ctx.ActivationsThisTurn): how many times the activated ability sa on
// source has been activated this turn, INCLUDING the resolving one -- its
// AbilityPush (a stack ability) or ManaActivate marker (a mana ability,
// emitted for a chain carrying the gate) is already in the log. sa is located
// in the source's pile by identity, else by its script line (a mana
// ability's colour-pinned or Produced$-rewritten copy keeps Line). Zero --
// the unbound value the effects gate fails open on -- for a nil SA, a
// source that has left, or an SA that is not one of the source's own
// activated abilities (a trigger, a spell, a granted body).
func (e *Engine) activationsThisTurnFor(source state.ObjID, sa *cards.SA) int32 {
	if sa == nil {
		return 0
	}
	o := e.G.Obj(source)
	if o == nil {
		return 0
	}
	idx, _, found := pileAbilityRefOf(o, sa)
	if !found && sa.Line != "" {
		for i, n := 0, o.PileAbilityCount(); i < n; i++ {
			if pa, ok := o.PileAbilityAt(i); ok && pa.SA != nil && pa.SA.Line == sa.Line {
				idx, found = i, true
				break
			}
		}
	}
	if !found {
		return 0
	}
	return int32(e.activationUsedCount(source, idx, "", true))
}
