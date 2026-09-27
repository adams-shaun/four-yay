// The remaining single-event trigger modes.
//
// Mode$ CommitCrime, Vote, FlippedCoin, Attached, RingTemptsYou, BecomesTarget,
// LandPlayed and Phase.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// commitCrimeMatches implements CR 700.13: targeting an opponent, a
// permanent they control, or a card in their graveyard commits one crime.
// A multi-target spell produces one TargetsChosen event per target. After
// Apply, append events can inspect the prior targets already on the stack;
// only the first criminal target may fire this trigger.
func (e *Engine) commitCrimeMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.TargetsChosen {
		return false
	}
	actor := e.controllerOf(ev.Obj)
	if !e.targetEventCommitsCrime(ev, actor) {
		return false
	}
	if o := e.G.Obj(ev.Obj); o != nil && (ev.Amount == 2 || ev.Amount == 3) {
		for _, target := range o.Targets[:len(o.Targets)-1] {
			if e.targetCommitsCrime(target, actor) {
				return false
			}
		}
	}
	if v := t.Params["ValidPlayer"]; v != "" {
		return effects.MatchesPlayerSpec(e.G, v, actor, e.controllerOf(source))
	}
	return true
}

func (e *Engine) targetEventCommitsCrime(ev events.Event, actor state.PlayerID) bool {
	if ev.Amount == 1 || ev.Amount == 3 {
		return e.targetCommitsCrime(state.Target{Player: ev.Player, IsPlayer: true}, actor)
	}
	return len(ev.IDs) == 1 && e.targetCommitsCrime(state.Target{Obj: ev.IDs[0]}, actor)
}

// targetCommitsCrime is CR 700.13's list, and only that list: an opponent; a
// permanent or a spell or ability on the stack an opponent controls; or a card
// in an opponent's graveyard, which is judged by its owner (CR 108.4a: a card
// that is not a permanent or spell has no controller). A card in exile, a
// hand or a library is none of these, whoever owns it.
func (e *Engine) targetCommitsCrime(target state.Target, actor state.PlayerID) bool {
	if target.IsPlayer {
		return target.Player != actor && int(target.Player) < len(e.G.Players)
	}
	o := e.G.Obj(target.Obj)
	if o == nil {
		return false
	}
	switch o.Zone {
	case state.ZBattlefield, state.ZStack:
		return o.Controller != actor
	case state.ZGraveyard:
		return o.Owner != actor
	}
	return false
}

// eventCardAndPlayerMatch applies the shared ValidCard$/ValidPlayer$ clauses
// on action triggers. The player is the player who performed the action.
func (e *Engine) eventCardAndPlayerMatch(t cards.Trigger, source, card state.ObjID, player state.PlayerID) bool {
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && !e.matchesSpec(v, card, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, player, ctrl) {
		return false
	}
	return true
}

// voteMatches implements Mode$ Vote (trig:Vote): the trigger fires on the
// canonical vote-finished Note both api:Vote shapes emit (effects/vote.go),
// the same carrier-event shape trig:FlippedCoin fires on. List$ is the
// REFERENT SCOPE, not the firing condition: "Whenever players finish voting"
// (Erestor of the Council, Model of Unity, Grudge Keeper -- the whole corpus
// population) has no intervening-if, so the trigger fires whenever a vote
// finishes, with an empty List$ set simply meaning its referents resolve to
// nobody (Grudge Keeper stacks, its diff set is empty, and its body acts on
// nobody). The capture side (triggerReferents' Vote case) applies List$ when
// it binds the sets, so a body reading a spelling its own List$ does not name
// gets the empty set -- the parameter is read, never silently inert.
func (e *Engine) voteMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if _, _, _, ok := effects.VoteFinishedResult(ev); !ok {
		return false
	}
	return true
}

func (e *Engine) flippedCoinMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	flipper, win, ok := effects.FlipNoteResult(ev)
	if !ok {
		return false
	}
	// ValidResult$ gates the side: Win = heads (Amount 1), Lose = tails
	// (Amount 0). Every corpus FlippedCoin line carries one; an absent
	// ValidResult$ (no such line measured) would fire on both sides.
	if res := strings.TrimSpace(t.Params["ValidResult"]); res != "" {
		if strings.EqualFold(res, "Win") && !win {
			return false
		}
		if strings.EqualFold(res, "Lose") && win {
			return false
		}
	}
	// ValidPlayer$ names the FLIPPER ("whenever YOU win a coin flip"): the
	// Note's Player, through the shared player-spec grammar with the trigger's
	// own controller as You. The two "whenever a player wins" lines carry no
	// ValidPlayer$ and fire on any flipper.
	if v, ok := t.Params["ValidPlayer"]; ok {
		if !effects.MatchesPlayerSpecFrom(e.G, v, flipper, e.controllerOf(source), source) {
			return false
		}
	}
	return true
}

// attachedMatches implements Mode$ Attached: the trigger fires when an Aura,
// Equipment or other attachment becomes attached to a permanent (CR
// 701.3a's "becomes attached" -- the event the engine's one shared attach
// emit site, effects/attach.go's effAttach, publishes for the cast, equip
// and ETB-attached shapes alike). events.Attach with len(ev.IDs) > 0 carries
// the attachment in ev.Obj and the bearer in ev.IDs[0]; the no-IDs emits are
// the detach state-based actions (rules/attach.go), which are NOT "becomes
// attached" and never match, and an emit with no attachment object (Obj == 0)
// has nothing to bind ValidSource$ against, so it never matches either.
// Forge's ValidSource$ names the ATTACHING
// object (Siona's Aura.YouCtrl, Enormous Energy Blade's Card.Self) and
// ValidTarget$ names the BEARER (Brood Keeper's Card.Self reads Self as the
// trigger's source through the same specCtx becomesTargetMatches uses).
// A trigger with NO ValidTarget$ never fires: Eriette's line names only
// TargetRelativeToSource$, a parameter this build does not read anywhere,
// so firing without the bearer restriction would over-fire on every Aura
// attach. The Static$ True guard keeps Forge's "static effect expressed as
// a trigger" lines (Metamorphic Alteration, Paleontologist's Pick-Axe --
// Execute$ DBClone continuous shapes with no static-trigger machinery here)
// from firing a clone on every attach.
func (e *Engine) attachedMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Attach || len(ev.IDs) == 0 || ev.Obj == 0 {
		return false
	}
	if t.Params["Static"] == "True" {
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidSource"]; ok {
		if !e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
			return false
		}
	}
	if v, ok := t.Params["ValidTarget"]; ok {
		return e.matchesSpec(v, ev.IDs[0], e.specCtx(source, ctrl))
	}
	return false
}

// ringTemptsMatches implements Mode$ RingTemptsYou (CR 701.54d): the trigger
// fires when the Ring tempts its controller, "when the actions complete,
// even if some were impossible" — an event with Obj 0 (no creature was
// designated) still counts as a temptation. The tempted player is ev.Player;
// ValidPlayer$ gates on the tempted player against the source's controller
// (the corpus's only spelling, `ValidPlayer$ You`). ValidCard$ gates on the
// chosen Ring-bearer with the source's controller as "you" and the source as
// Other — the corpus's `Creature.YouCtrl+Other` shape ("a creature other than
// CARDNAME") and the plain `Creature.YouCtrl` shape both resolve through it;
// with no designated bearer a ValidCard$ trigger never fires.
func (e *Engine) ringTemptsMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.RingTemptsYou {
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidPlayer"]; ok {
		if !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
			return false
		}
	}
	if v := t.Params["ValidCard"]; v != "" {
		if ev.Obj == 0 {
			return false // no creature became the Ring-bearer
		}
		if !e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
			return false
		}
	}
	return true
}

// becomesTargetMatches implements Mode$ BecomesTarget: the trigger fires
// when one of the chosen targets recorded by a TargetsChosen event
// (rules.handleTarget -- "the target decision being answered") matches its
// ValidTarget$ -- or, with no ValidTarget$, when its own source is among the
// targets. Forge's ValidTarget$ names the TARGETED object: the self-shapes
// (ValidTarget$ Card.Self, the ward family, Reality Smasher) match their own
// source that way, and the "a Dragon you control becomes the target" shapes
// (Thunderbreak Regent) match a target their source merely watches -- the
// 50 non-self corpus lines of the 132-line mode.
func (e *Engine) becomesTargetMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.TargetsChosen {
		return false
	}
	if v, ok := t.Params["ValidSource"]; ok {
		// ValidSource$ names the spell or ability doing the targeting: the
		// TargetsChosen event's Obj, the stack object whose target decision
		// this event answers (commitCrimeMatches reads the same field as the
		// targeting actor). Reality Smasher's "spell an opponent controls"
		// (Spell.OppCtrl) and Thunderbreak Regent's "spell or ability"
		// (SpellAbility.OppCtrl) both resolve against that stack object; an
		// event carrying no targeting object can never match.
		if ev.Obj == 0 || !e.matchesSpec(v, ev.Obj, e.specCtx(source, e.controllerOf(source))) {
			return false
		}
	}
	if v, ok := t.Params["ValidTarget"]; ok {
		for _, id := range ev.IDs {
			if e.matchesSpec(v, id, e.specCtx(source, e.controllerOf(source))) {
				// CR 702.21a compares the Ward permanent's controller with the
				// controller of the targeting spell or ability ON THE STACK
				// (the same ev.Obj ValidSource$ reads above). For an ability,
				// protectionSource would unwrap ev.Obj to its source permanent,
				// whose controller may have changed since activation. Every
				// ward trigger targets only itself (the synthesized shape,
				// trigger_match.go's ward expansion), so this gate runs on the
				// self match.
				if t.Params["Ward"] == "True" &&
					(ev.Obj == 0 || e.controllerOf(ev.Obj) == e.controllerOf(source)) {
					return false
				}
				return true
			}
		}
		return false
	}
	targeted := false
	for _, id := range ev.IDs {
		if id == source {
			targeted = true
			break
		}
	}
	if targeted && t.Params["Ward"] == "True" &&
		(ev.Obj == 0 || e.controllerOf(ev.Obj) == e.controllerOf(source)) {
		// The ValidTarget$ branch's ward gate, applied to the bare
		// self-targeted fallback: a ward trigger never fires for its own
		// controller's targeting (CR 702.21a compares the ward permanent's
		// controller with the targeting spell or ability's, the same ev.Obj
		// ValidSource$ reads above). Unreachable in the current corpus --
		// every ward trigger is keyword-synthesized with ValidTarget$
		// Card.Self (cards/keywords.go, 0 raw Ward$ True lines) -- kept so
		// a future ward trigger without ValidTarget$ cannot fire for its
		// own controller.
		return false
	}
	return targeted
}

// becomesTargetOnceMatches implements Mode$ BecomesTargetOnce (Forge's
// TriggerBecomesTargetOnce): the BATCH reading of Mode$ BecomesTarget, "whenever
// one or more creatures you control become the target ...". Forge fires it once
// per targeting ACTION -- after the whole spell/ability has chosen its targets
// -- carrying the target SET in AbilityKey.Targets and the causing card in
// AbilityKey.Cause. This engine emits one TargetsChosen event per chosen target,
// so the once-per-action cadence rides the target batch
// (openTargetBatch/closeTargetBatch around recordChosenTargets, and the
// queue-time gate in trigger_match.go's checkFaceTriggers); this matcher answers
// only whether ONE event's own target/source/cause clause holds.
//
// ValidTarget$: any one of the action's targets -- a permanent (ev.IDs) or a
// player (ev.Player, shapes 1/3) -- matching the spec (Professor Hojo's
// Creature.YouCtrl+inZoneBattlefield, Leyline's You,Permanent.YouCtrl...).
// ValidSource$: the targeting spell/ability; the corpus's `Activated` spelling
// is Forge's activated-ability predicate and is answered directly, every other
// spelling through the ordinary filter grammar (SpellAbility.OppCtrl). An
// event with no targeting object fails closed. ValidCause$: the host card of
// the targeting ability (Forge's sp.getHostCard()), which protectionSource
// resolves for a spell (itself) and an ability (its source permanent);
// Psychic Battle excludes itself by name that way.
func (e *Engine) becomesTargetOnceMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.TargetsChosen {
		return false
	}
	you := e.controllerOf(source)
	sc := e.specCtx(source, you)
	if v, ok := t.Params["ValidSource"]; ok {
		if !e.becomesTargetSourceMatches(v, ev.Obj, sc) {
			return false
		}
	}
	if v, ok := t.Params["ValidTarget"]; ok {
		matched := false
		for _, id := range ev.IDs {
			if e.matchesSpec(v, id, sc) {
				matched = true
				break
			}
		}
		if !matched && (ev.Amount == 1 || ev.Amount == 3) {
			matched = effects.MatchesPlayerSpecCtx(e.G, v, ev.Player, you, e.playerSpecCtx(source))
		}
		if !matched {
			return false
		}
	}
	if v, ok := t.Params["ValidCause"]; ok {
		cause := e.protectionSource(ev.Obj)
		if cause == 0 || !e.matchesSpec(v, cause, sc) {
			return false
		}
	}
	return true
}

// becomesTargetSourceMatches answers a Mode$ BecomesTargetOnce ValidSource$
// clause over the targeting stack object. Forge's `Activated` predicate is an
// ability-shape test, not a card filter, so it is answered directly: an
// activated ability's stack object carries an Ability and is not a stored
// trigger (the same split counterValidSA makes for R:Event$ Counter). Every
// other comma alternative -- SpellAbility.OppCtrl, Spell.OppCtrl, a plain
// object spec -- goes through the ordinary object-filter grammar, the sibling
// becomesTargetMatches' ValidSource$ read. An absent targeting object fails
// closed.
func (e *Engine) becomesTargetSourceMatches(spec string, stackObj state.ObjID, sc effects.SpecContext) bool {
	if stackObj == 0 {
		return false
	}
	o := e.G.Obj(stackObj)
	if o == nil {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		if alt == "Activated" {
			if o.Ability != nil && !isTriggered(e.G, o) {
				return true
			}
			continue
		}
		if e.matchesSpec(alt, stackObj, sc) {
			return true
		}
	}
	return false
}

// openTargetBatch/closeTargetBatch bracket ONE targeting action's TargetsChosen
// events for the Mode$ BecomesTargetOnce latch. recordChosenTargets
// (rules/stack.go) opens the bracket, emits one event per chosen target, and
// closes it, so every target of one target answer is one batch. It is NOT the
// only emitter of TargetsChosen: effects/choose_control.go's recordTargets (a
// ChangeTargets redirect, CR 114.6) also emits, with NO bracket open, so each
// such event is its own batch-of-one and a multi-target redirect fires a
// BecomesTargetOnce watcher once per redirected target rather than once per
// redirect action (Forge fires once per ability). No corpus carrier exercises
// that path today (Psychic Battle, the only ValidCause$ card, excludes itself;
// Leyline/Hojo never watch a redirect), so the divergence is latent. The latch
// map is per-batch scratch (the damage/zone/mill/discard batches' shape) and
// never survives the close. A hand-built emit outside any bracket is its own
// batch-of-one.
func (e *Engine) openTargetBatch() {
	e.targetBatchOpen = true
	e.targetBatchFired = nil
}

func (e *Engine) closeTargetBatch() {
	e.targetBatchOpen = false
	e.targetBatchFired = nil
}

// landPlayedMatches implements Mode$ LandPlayed. This fires on the MoveZone
// hand->battlefield of a land specifically -- not on the separate LandPlayed
// event legal.go's "play_land" case also emits, which carries only a Player
// (no Obj), and so has nothing ValidCard$ could ever match against.
func (e *Engine) landPlayedMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.MoveZone || ev.From != state.ZHand || ev.To != state.ZBattlefield {
		return false
	}
	obj := e.G.Obj(ev.Obj)
	if obj == nil || obj.Face() == nil || !obj.Face().IsLand() {
		return false
	}
	if v, ok := t.Params["ValidCard"]; ok {
		return e.matchesSpec(v, ev.Obj, e.specCtx(source, e.controllerOf(source)))
	}
	return true
}

type parsedPhase struct {
	set   state.StepSet
	valid bool
}

// parsedPhaseSpec caches syntax only, never whether the current step matches.
// Diagnostic scans and live/look-back matchers use the same parse semantics;
// only the live scan emits Notes, tracked separately in phaseUnknownNoted.
func (e *Engine) parsedPhaseSpec(spec string) parsedPhase {
	if p, ok := e.phaseSpecs[spec]; ok {
		return p
	}
	set, unknown := state.ParsePhases(spec)
	p := parsedPhase{set: set, valid: len(unknown) == 0}
	if e.phaseSpecs == nil {
		e.phaseSpecs = make(map[string]parsedPhase)
	}
	e.phaseSpecs[spec] = p
	return p
}

// phaseGate applies Forge's Phase$ (validPhases) uniformly to every trigger
// mode. It is deliberately before the mode switch in triggerMatches: a
// ChangesZone or SpellCast trigger with Phase$ Main1 must not fire during an
// upkeep, and an unresolvable name fails closed. checkFaceTriggers reports
// that invalid name once as a Note; this bool-only matcher does not emit
// while it may be walking a scratch look-back observer. An absent Phase$
// remains ungated, matching Forge's null validPhases.
//
// PhaseCount$ narrows a Phase$ set to the Nth member of that set in turn
// order: `Phase$ Main | PhaseCount$ 2` is the SECOND main phase, so the gate
// fails at the first. A non-positive or non-numeric value fails closed (the
// conservative direction -- the trigger then fires at no step rather than
// every matching one).
func (e *Engine) phaseGate(t cards.Trigger) bool {
	spec := t.Params["Phase"]
	if strings.TrimSpace(spec) == "" {
		return true
	}
	p := e.parsedPhaseSpec(spec)
	if !p.valid || !p.set.Has(e.G.Step) {
		return false
	}
	// gorge has one combat-damage step, while Forge distinguishes the
	// first-strike damage step. Until the turn walk has that separate step,
	// only let this mapping match when a first/double striker is actually in
	// combat; otherwise the named phase does not occur at all.
	for phase := range strings.SplitSeq(spec, ",") {
		phase = strings.TrimSpace(phase)
		if strings.EqualFold(phase, "First Strike Damage") ||
			strings.EqualFold(phase, "COMBAT_FIRST_STRIKE_DAMAGE") {
			if e.G.Step != state.StepCombatDamage || !e.anyFirstStrike() {
				return false
			}
		}
	}
	count := strings.TrimSpace(t.Params["PhaseCount"])
	if count == "" {
		return true
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 1 {
		return false
	}
	return p.set.Ordinal(e.G.Step) == n
}

// phaseMatches implements Mode$ Phase after phaseGate has already checked
// its Phase$ parameter. The mode itself is only a StepChange event plus its
// optional ValidPlayer$ restriction.
func (e *Engine) phaseMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.StepChange {
		return false
	}
	if v, ok := t.Params["ValidPlayer"]; ok {
		// StepChange carries no Player of its own -- a step always belongs
		// to the current active player.
		if !effects.MatchesPlayerSpecCtx(e.G, v, e.G.Active, e.controllerOf(source), e.playerSpecCtx(source)) {
			return false
		}
	}
	return true
}

// rolledDieMatches implements Mode$ RolledDie (trig:RolledDie): the trigger
// fires on the canonical per-die roll Note effRollDice emits (effects/dice.go's
// DieRollNote, decoded by DieRollResult), so every "whenever you roll a die /
// roll a 4 or higher" line fires exactly as the roll happens. The trigger
// fires ONCE PER DIE, each with that die's own result -- Forge's RolledDie
// cadence, and the reading Natural$ True ("a die's highest natural result")
// and Number$ 3 ("your third die each turn") require.
//
//   - ValidResult$ is the result filter: a literal ("4"), a comma list
//     ("1,2"), an <OP><N> comparison (GE4, EQ1, LE3, GT/LT/NE) over the
//     matched value, or "Highest" (the natural roll is the die's maximum,
//     "a die's highest natural result"). An unparseable value fails closed.
//   - Natural$ True matches against the UNMODIFIED die rather than the
//     Modifier$-adjusted result ("when you roll a natural 20").
//   - ValidSides$ scopes to a die size ("on a six-sided die").
//   - ValidPlayer$ names the roller ("whenever YOU roll"), through the shared
//     player-spec grammar with the trigger's own controller as You.
//   - Number$ N ("your third die each turn") fires only on the Nth die,
//     gated at the QUEUE site (dieRollNumberAllows, in trigger_match.go's
//     checkFaceTriggers beside ActivationLimit$) -- the count can only
//     advance when the trigger is really queued, never from a speculative
//     matcher call, and the per-turn counter lives per trigger line.
//   - Static$ True lines are Forge's continuous-effect-expressed-as-a-trigger
//     (the two Attraction static lines), which this build does not run as
//     triggers -- the attachedMatches guard.
//   - RolledToVisitAttractions$ True scopes to a roll made to visit
//     Attractions; no Attraction deck exists here, so no roll is one and the
//     line never fires (fail closed, never over-fires).
func (e *Engine) rolledDieMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	roller, sides, natural, result, ok := effects.DieRollResult(ev)
	if !ok {
		return false
	}
	return e.rolledDieCommon(t, source, roller, sides, natural, result)
}

// rolledDieOnceMatches implements Mode$ RolledDieOnce (trig:RolledDieOnce),
// Forge's "whenever you roll one or more dice" mode: the trigger fires ONCE
// per DB$ RollDice resolution on the canonical batch roll Note effRollDice
// emits (effects/dice.go's DieRollBatchNote, decoded by DieRollBatchResult),
// however many dice the action rolled. The per-die Mode$ RolledDie matcher
// cannot express this cadence -- it would fire once per die -- and the batch
// Note is the resolution boundary the per-die Notes do not carry.
//
// The result filter runs against the batch's HIGHEST modified result
// (Pairs[0][1]): a ValidResult$ line fires if any die of the batch satisfied
// it, which is what Farideh's "if any of those results was 10 or higher"
// means and the only multi-die Once reader in the corpus. Every other
// RolledDieOnce carrier (Vexing Puzzlebox, Brazen Dwarf, Feywild Trickster,
// Vrondiss, Wyll, Barbarian Class) rolls exactly one die, where the highest
// result IS the result. Natural$ True on a Once line is likewise read against
// the highest modified result (0 corpus carriers; the natural-max is not
// carried on the batch Note). No Number$ rides this mode.
func (e *Engine) rolledDieOnceMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	roller, _, maxResult, _, ok := effects.DieRollBatchResult(ev)
	if !ok {
		return false
	}
	return e.rolledDieCommon(t, source, roller, 0, maxResult, maxResult)
}

// rolledDieCommon applies the parameters Mode$ RolledDie and Mode$ RolledDieOnce
// share, so the two cadences can never disagree about Static$/ValidSides$/
// ValidResult$/ValidPlayer$. sides is 0 when the carrier (the batch Note) does
// not name a die size, in which case a ValidSides$ line fails closed rather
// than matching any size.
func (e *Engine) rolledDieCommon(t cards.Trigger, source state.ObjID, roller state.PlayerID, sides, natural, result int32) bool {
	if strings.EqualFold(strings.TrimSpace(t.Params["Static"]), "True") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(t.Params["RolledToVisitAttractions"]), "True") {
		return false
	}
	if v := strings.TrimSpace(t.Params["ValidSides"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || int32(n) != sides {
			return false
		}
	}
	matched := result
	if strings.EqualFold(strings.TrimSpace(t.Params["Natural"]), "True") {
		matched = natural
	}
	if v, present := t.Params["ValidResult"]; present && !dieResultMatches(v, matched, natural, sides) {
		return false
	}
	if v, present := t.Params["ValidPlayer"]; present {
		if !effects.MatchesPlayerSpecFrom(e.G, v, roller, e.controllerOf(source), source) {
			return false
		}
	}
	return true
}

// dieResultMatches reports whether a die roll's matched value (the modified
// result, or the natural roll when Natural$ True) satisfies a trigger's
// ValidResult$ spec: the comma-list of literals and <OP><N> comparisons the
// corpus carries, plus the special "Highest" (the natural roll is the die's
// maximum face). An unrecognised token fails closed.
func dieResultMatches(spec string, matched, natural, sides int32) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	if strings.EqualFold(spec, "Highest") {
		return sides > 0 && natural == sides
	}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			if int32(n) == matched {
				return true
			}
			continue
		}
		if compareIntCount(matched, part) {
			return true
		}
	}
	return false
}

func init() {
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.commitCrimeMatches(t, source, ev)
	}, "CommitCrime")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.rolledDieMatches(t, source, ev)
	}, "RolledDie")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.rolledDieOnceMatches(t, source, ev)
	}, "RolledDieOnce")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.voteMatches(t, source, ev)
	}, "Vote")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.flippedCoinMatches(t, source, ev)
	}, "FlippedCoin")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.attachedMatches(t, source, ev)
	}, "Attached")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.ringTemptsMatches(t, source, ev)
	}, "RingTemptsYou")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.becomesTargetMatches(t, source, ev)
	}, "BecomesTarget")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.becomesTargetOnceMatches(t, source, ev)
	}, "BecomesTargetOnce")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.landPlayedMatches(t, source, ev)
	}, "LandPlayed")
	registerTrigMatcher((*Engine).phaseTriggerMatches, "Phase")
}

// phaseTriggerMatches is Mode$ Phase plus the echo gate.
//
// kw:Echo (CR 702.35a) rides the Echo$ True marker on its generated keyword
// trigger the way Annihilator$ rides its own: the intervening-if must suppress
// the trigger BEFORE it stacks (a stacked-but-owed-nothing echo is an
// observable divergence). The gate reads the object's control-acquisition
// tuple (rules/echo.go) against the controller's most recent upkeep.
//
// Named rather than a closure in init() so the param census attributes the
// Echo$ read to a matcher it can reach from the registration, not to init.
func (e *Engine) phaseTriggerMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if !e.phaseMatches(t, source, ev) {
		return false
	}
	if t.Params["Echo"] == "True" {
		return e.echoGateHolds(source)
	}
	return true
}
