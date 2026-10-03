package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cyclingCauseKeywords is the CR 702.28 cycling family: the plain Cycling
// keyword and its typed form (TypeCycling), both of which Forge's `Cycling`
// stack spec names. Exact membership -- a substring test would let
// TypeCycling's own "Cycling" suffix admit the plain spec through a false
// positive, and vice versa is impossible because the values are distinct.
var cyclingCauseKeywords = map[string]bool{"Cycling": true, "TypeCycling": true}

// drawCauseAdmits evaluates a ValidCause$ stack spec against the spell or
// ability that caused Draw event ev, from source's controller's perspective.
// It serves the Draw replacement arm (Unpredictable Cyclone, the corpus's
// only Draw ValidCause$ carrier: "If a cycling ability of another nonland
// card would cause you to draw a card, instead ...").
//
// The base kind and the controller / instant-sorcery restrictions are read
// through state's shared classifier (StackKindTokenOf + StackKindAdmits), so
// this cannot drift from TargetType$/ValidStack. But that classifier
// DELIBERATELY ignores every other qualifier (its doc comment records the
// widening), which is fine for a target offer but wrong here: a replacement
// scoped by `Activated.Cycling+nonLand` must not apply to a draw caused by
// ANY activated ability. The qualifiers this helper adds are the ones the one
// Draw carrier needs -- `Cycling` (the cause ability carries the keyword) and
// a card predicate such as `nonLand` (matched on the cause's SOURCE card
// through the ordinary filter grammar). Any qualifier this helper does not
// recognise FAILS CLOSED: a cause spec it cannot evaluate must never admit
// the replacement (the repo's standing filter contract).
//
// Comma-separated alternatives are OR, matching ValidTgts$/ValidCause$
// semantics elsewhere.
func (e *Engine) drawCauseAdmits(spec string, source state.ObjID, ev events.Event) bool {
	cause := e.actionCause()
	if cause == 0 {
		return false
	}
	o := e.G.Obj(cause)
	if o == nil {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		if e.drawCauseTokenAdmits(strings.TrimSpace(alt), o, source) {
			return true
		}
	}
	return false
}

// drawCauseTokenAdmits evaluates ONE comma-separated token of a Draw
// ValidCause$ spec (drawCauseAdmits's per-alternative worker). It recognises
// a valid stack base, the shared controller / instant-sorcery qualifiers, the
// `Cycling` keyword qualifier and a card-predicate qualifier (evaluated
// against the cause's source card). Every other qualifier fails closed.
func (e *Engine) drawCauseTokenAdmits(token string, o *state.Object, source state.ObjID) bool {
	tok, ok := state.StackKindTokenOf(token)
	if !ok {
		return false
	}
	_, rest, _ := strings.Cut(strings.TrimSpace(token), ".")
	for q := range strings.SplitSeq(rest, "+") {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		switch q {
		case "YouCtrl", "OppCtrl", "Instant", "Sorcery":
			// Read by StackKindAdmits below.
		case "Cycling":
			if o.Ability == nil || !cyclingCauseKeywords[o.Ability.ParamStr(cards.PKKeyword)] {
				return false
			}
		default:
			// A card-predicate qualifier (nonLand, a colour, a type word,
			// ...) on the cause's SOURCE card. Classify it with the SAME
			// shared recognizer UnknownPredicates uses, so an unrecognised
			// token fails closed rather than widening the match.
			src := e.G.Obj(o.Source)
			if src == nil {
				return false
			}
			pred := "Card." + q
			if len(effects.UnknownPredicates(pred)) != 0 {
				return false
			}
			if !e.matchesSpec(pred, src.ID, e.withNames(effects.NewSpecContext(e.controllerOf(source), source))) {
				return false
			}
		}
	}
	return state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindOf(e.G, o), o,
		o.Controller, e.controllerOf(source))
}

// pendingDrawIsFirstInDrawStep reports whether a Draw about to be logged for
// p is the first p draws since this turn entered its draw step. It is the
// pre-emit twin of trigmatch.firstCardInDrawStep: replacement matching runs from
// Engine.emit BEFORE the proposed Draw is appended to e.L.Events, so the
// pending event itself is the "next" draw (draw count 0) rather than a
// logged one (draw count 1). It requires p to be the ACTIVE player as well,
// because the exempt draw CR 504.1 grants is that player's own turn-based
// draw: a non-active player drawing during someone else's draw step is not
// the first one they draw in each of their own draw steps, so Notion Thief
// and Hullbreacher must still replace it. trigmatch.firstCardInDrawStep deliberately
// omits that active-player test (a trigger reads whoever drew); the two
// cannot share a body, so they are kept adjacent with identical log-scan
// shapes to stop the pair drifting.
func (e *Engine) pendingDrawIsFirstInDrawStep(p state.PlayerID) bool {
	if e.G.Step != state.StepDraw || p != e.G.Active {
		return false
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.Draw && ev.Player == p {
			return false
		}
		if ev.Kind == events.StepChange {
			return ev.Step == state.StepDraw
		}
	}
	return false
}

// extraDrawsThisTurn counts p's draws in the current turn that are NOT the
// CR 504.1 turn-based draw -- the first card p draws in p's OWN draw step
// while p is the active player. That is the draw Reed Richards' "except the
// first card you draw during each of your draw steps" clause exempts, so a
// FirstExtraCardDrawnThisTurn$ True replacement must apply only when this
// count is zero (CR 614.1a: one replacement per occasion, and only the first
// such occasion each turn).
//
// It runs from replacement matching, which is PRE-emit: the pending Draw is
// not yet in e.L.Events, so the caller adds the pending draw's own
// applicability separately (see pendingDrawIsFirstInDrawStep, the pre-emit
// twin of the exempt-draw test). The log -- not a mutable counter -- is the
// source of this per-turn fact, so cloning and replay rebuild it without an
// event-schema change.
func (e *Engine) extraDrawsThisTurn(p state.PlayerID) int {
	n := 0
	start := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if e.L.Events[i].Kind == events.TurnChange {
			start = i
			break
		}
	}
	// step is the step the scan is currently inside; 255 is the uint8 sentinel
	// for "before any StepChange this turn", which no real step equals.
	step := state.Step(255)
	drawsInStep := 0
	for i := start; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		switch ev.Kind {
		case events.StepChange:
			step = ev.Step
			if ev.Step == state.StepDraw {
				drawsInStep = 0
			}
		case events.Draw:
			if ev.Player != p {
				continue
			}
			if step == state.StepDraw && p == e.G.Active && drawsInStep == 0 {
				drawsInStep++
				continue
			}
			n++
		}
	}
	return n
}

// actionCause is the stack object whose resolving effect caused a synchronous
// action event. Costs are paid before an activated ability exists on the stack,
// so they deliberately have no cause and cannot satisfy ValidCause$. This is
// replay-safe: action triggers are checked synchronously inside emit, while
// the resolving object is still at the top of the replayed stack.
func (e *Engine) actionCause() state.ObjID {
	// causePin is an entry-settle preview's stand-in for the live action
	// cause (rules/entry_counters.go): the preview's Apply folds the entry
	// move, which takes the entrant off the cloned stack, so the bare read
	// would report no cause for a cast spell's own entry. Only a preview
	// engine ever carries one; live engines read the stack.
	if e.causePin != 0 {
		return e.causePin
	}
	if len(e.G.Stack) == 0 {
		return 0
	}
	return e.G.Stack[len(e.G.Stack)-1]
}

// causeSpecAdmits evaluates a CantSacrifice static's ValidCause$ stack spec
// (task vc-static1) against the in-flight sacrifice cause -- actionCause(),
// the resolving wrapper at the top of the stack for the whole effect-driven
// call. The rules COST sites never reach it: SacrificeBlocked's forCost
// callers skip every ValidCause-carrying static before this helper runs,
// because a cost payment has no causing object (actionCause would name
// whatever unrelated spell was already on the stack when the player paid --
// the exact misattribution trigmatch.DiscardCauseAdmits guards against with its
// IsDiscardCost check).
//
// The classifier is the shared StackKindTokenOf + StackKindAdmits pair the
// Discarded/Drawn cause matchers use, so the static path cannot drift from
// the trigger path. Two deliberate departures from the plural StackKindTokens
// helper, both fail closed: a comma alternative naming no stack kind (e.g.
// `Creature`) contributes nothing rather than falling into the plural
// helper's Spell-only default, and an alternative carrying a qualifier the
// classifier silently ignores (singleTarget, numTargets, ...) admits nothing
// rather than the widening the target-offer path documents -- a cause spec
// this build cannot evaluate exactly must never blanket-block a sacrifice.
// Comma alternatives are OR, matching ValidCause$ semantics elsewhere; the
// spec matches when SOME alternative admits the cause.
func (e *Engine) causeSpecAdmits(spec string, source state.ObjID) bool {
	cause := e.actionCause()
	if cause == 0 {
		return false
	}
	o := e.G.Obj(cause)
	if o == nil {
		return false
	}
	you := e.controllerOf(source)
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		tok, ok := state.StackKindTokenOf(alt)
		if !ok {
			continue // a non-stack alternative never contributes (fail closed)
		}
		if !causeSpecQualifiersKnown(alt) {
			continue // an unmodelled qualifier admits nothing (fail closed)
		}
		if state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindOf(e.G, o),
			o, o.Controller, you) {
			return true
		}
	}
	return false
}

// causeSpecQualifiersKnown reports whether every dot qualifier of one stack
// spec alternative is one the shared classifier actually reads. The
// classifier's own qualifier loop (state/stackkind.go StackKindTokenOf)
// silently DROPS an unknown qualifier -- sound for a target offer (the
// documented widening) but wrong for a cause restriction, where the dropped
// qualifier would widen the block. The known set is exactly what that loop
// consumes: YouCtrl/OppCtrl (controller scoping) and Instant/Sorcery (the
// Spell kind's card-type restriction).
func causeSpecQualifiersKnown(alt string) bool {
	_, rest, _ := strings.Cut(alt, ".")
	for q := range strings.SplitSeq(rest, ".") {
		switch q {
		case "", "YouCtrl", "OppCtrl", "Instant", "Sorcery":
		default:
			return false
		}
	}
	return true
}

// causeCostAdmits evaluates a CantSacrifice static's ValidCause$ spec on the
// COST path (task cantsac1) against the pending activation's cause. It is
// causeSpecAdmits' cost-side sibling and shares its fail-closed discipline,
// but not its input: a cost payment has no resolving stack object to
// classify (pushCast pays an ability's costs before its AbilityPush, and a
// mana ability never reaches the stack at all), so the cause comes from
// sacrificeBlockedForCost's caller, which knows what the payer is
// casting/activating.
//
// A cost site's cause is the ability the payment is made to (the cantsac1
// r2 semantics table on costCause): a spell cast (Spell), an ability
// activation (Activated), a ward or upkeep trigger's demand (Triggered) or
// an unless resolution election (Resolution). Those four -- None stays
// inadmissible, a no-cause payment names nothing -- are the readable
// grammar; every corpus ForCost$ True carrier is a bare `Spell,Activated`
// (angel_of_jubilation, yasharn_implacable_earth) and therefore scopes to
// the cast/activation sites only, never to a ward, unless or upkeep
// payment. A qualified base (Spell.Instant, Spell.OppCtrl) or any other
// base (SpellAbility, Ability) names a cause this path cannot exactly
// evaluate, so it fails closed -- the permissive direction for a
// restriction, and no corpus line is affected.
func causeCostAdmits(spec string, cause costCause) bool {
	if cause == costCauseNone || cause == costCauseResolution {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		if rest != "" {
			continue // a qualified cost cause is not modelled (fail closed)
		}
		switch base {
		case "Spell":
			if cause == costCauseSpell {
				return true
			}
		case "Activated":
			if cause == costCauseActivated {
				return true
			}
		case "Triggered":
			if cause == costCauseTriggered {
				return true
			}
		}
	}
	return false
}
