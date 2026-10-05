// Combat trigger modes.
//
// Mode$ Attacks, AttackersDeclared(OneTarget), Blocks, AttackerBlocked(ByCreature),
// AttackerUnblockedOnce, Exerted, DamageDone/DamageDealtOnce/DamageDoneOnce and
// DamagePreventedOnce.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package trigmatch

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// AttacksMatches implements Mode$ Attacks against a DeclareAttackers event.
// DeclareAttackers carries the attackers declared against ONE defending
// player in one event (IDs; Task m34 emits one event per defender), so like
// every other mode here it fires at most once per event -- a creature
// attacking a single opponent therefore fires exactly once, in its own
// defender's event.
//
// Alone$ True (Exalted's expansion, cards/keywords.go -- Ruling FL-48, which
// was previously a known approximation here) gates the trigger to "exactly
// one attacker declared this combat": Exalted must pump only a single lone
// attacker, and with several declared it must not fire at all. Because this
// fires once per event, the lone-attacker check is len(IDs)==1 and the rest
// of the matching selects that one attacker against ValidCard.
func AttacksMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.DeclareAttackers {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKMyriad)), "True") {
		// Myriad$ True is the myriad keyword expansion's own marker
		// (cards/keywords.go addKeywordTrigger): the per-other-opponent token
		// copies are the DB$ Myriad body, so the marker requires the
		// trigger's Execute sub to resolve to exactly that body -- a
		// mismatched or unresolvable expansion must not fire.
		src := e.Game().Obj(source)
		if src == nil || src.Face() == nil {
			return false
		}
		sa := cards.ResolveSVar(src.Face().SVars, t.ParamStr(cards.PKExecute))
		if sa == nil || sa.API != "Myriad" {
			return false
		}
	}
	if v, ok := t.Param(cards.PKAlone); ok && strings.EqualFold(v, "True") && len(ev.IDs) != 1 {
		return false
	}
	// Attacked$ scopes "whenever a creature attacks <defender>" (CR 508.1c's
	// declared-defender half): Forge puts the ATTACKED player in this
	// trigger-level param (Revenge of Ravens' "attacks you or a planeswalker
	// you control" = Attacked$ You,Planeswalker.YouCtrl), and without reading
	// it a Mode$ Attacks trigger fires on every DeclareAttackers event at the
	// table -- over-broad in every multiplayer game. DeclareAttackers already
	// carries the defender in ev.Player, one event per defender, so the gate
	// is evaluated against that seat with the trigger's own controller as the
	// perspective. The spec goes through the shared player filter, so You /
	// Opponent / Player.withMostLife / Player.isMonarch / Player.Chosen / the
	// life-comparison qualifiers resolve and every unmodelled alternative
	// (Planeswalker.YouCtrl, Battle.ProtectedBy, Player.EnchantedBy, ...)
	// fails closed. Because the value is a comma list, a rule naming both a
	// player and an unmodellable permanent ("You,Planeswalker.YouCtrl") still
	// fires on the player half -- the engine models players-only defenders,
	// so that is the whole of the attack it can represent.
	if v := t.ParamStr(cards.PKAttacked); v != "" {
		if !effects.MatchesPlayerSpecFrom(e.Game(), v, ev.Player, e.ControllerOf(source), source) {
			return false
		}
	}
	// Dethrone (CR 702.105) fires only when the attacked player has the
	// greatest life total (tied is enough) among ALL players. Comparing only
	// the attacker and its defender is wrong in multiplayer: a third player
	// with more life prevents the trigger even though it was not attacked.
	if v, ok := t.Param(cards.PKDethrone); ok && strings.EqualFold(v, "True") {
		if !PlayerHasMostLife(e, ev.Player) {
			return false
		}
	}
	// Condition$ AttackedPlayerWithMostLife (Scourge of the Throne, the
	// corpus's one carrier of this spelling on a trigger line): the
	// "if it's attacking the player with the most life or tied for most
	// life" intervening-if. It is the SAME gate Dethrone reads -- one shared
	// PlayerHasMostLife helper, so the two cannot drift -- evaluated on the
	// event's declared defender, which is why it lives in the per-mode
	// matcher beside Dethrone and not in the shared triggerConditionHolds
	// walk (no event there to name the defender). Mirroring Dethrone, the
	// gate is fire-time only: the resolution-time CR 603.4 recheck cannot
	// re-derive the attacked player from the event, the same scope every
	// other event-relative matcher gate here keeps.
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKCondition)), "AttackedPlayerWithMostLife") {
		if !PlayerHasMostLife(e, ev.Player) {
			return false
		}
	}
	// Training (CR 702.70) fires only when the attacking source attacks
	// alongside ANOTHER creature with strictly greater power. The declaration
	// is spread across one DeclareAttackers event per defender, so the other
	// attackers are read from Engine.declaredAttackers (the whole chosen set)
	// rather than ev.IDs -- two creatures attacking different opponents still
	// attack "with" each other. Power is the derived value, so a lord or a
	// counter moves the comparison exactly as it moves the creature. An empty
	// scratch (a synthetic event, or a helper that emits DeclareAttackers
	// directly without a declaration) falls back to ev.IDs, which is the
	// declaration itself in every single-defender case.
	if v, ok := t.Param(cards.PKTraining); ok && strings.EqualFold(v, "True") {
		attackers := e.Facts().DeclaredAttackers
		if len(attackers) == 0 {
			attackers = ev.IDs
		}
		power := e.Power(source)
		bigger := false
		for _, id := range attackers {
			if id == source {
				continue
			}
			if e.Power(id) > power {
				bigger = true
				break
			}
		}
		if !bigger {
			return false
		}
	}
	// FirstAttack$ True (Aurelia the Warleader, Godo Bandit Warlord, Scourge
	// of the Throne, Fear of Missing Out -- the four corpus carriers, all the
	// plain True spelling) gates the trigger to the attacker's FIRST attack
	// this turn (CR 603.2e's "for the first time each turn"). The count is
	// event-folded state (Object.AttacksThisTurn, reset at TurnChange -- an
	// extra combat inside the same turn does not reset it), and trigger
	// matching runs on the FOLDED event, so the test is count == 1, never 0.
	// A non-first attacker must not veto the match either: an event may name
	// several attackers and another one may still be first.
	spec, ok := t.Param(cards.PKValidCard)
	if !ok {
		for _, id := range ev.IDs {
			if id == source {
				return firstAttackOK(e, t, id)
			}
		}
		return false
	}
	ctrl := e.ControllerOf(source)
	for _, id := range ev.IDs {
		// The matched attacker's DERIVED types ride ExtraTypes (the mechanism
		// the layer walk binds): the printed-face-only filter read would miss
		// an animated manland's Creature grant -- Raging Ravine's own
		// "Whenever this creature attacks" trigger names Creature.Self and
		// must fire on the animated land. ExtraTypes is checked BEFORE the
		// printed face and is a superset of it (Derived.Types includes every
		// printed type), so ordinary creatures are unchanged, and the
		// bestowed exclusion survives (the layer-4 switch drops Creature
		// from a bestowed card's derived types, and hasType's
		// BestowedAttached gate still answers below).
		opts := SpecOpts{ExtraTypes: e.Chars(id).Types}
		// The compiled sidecar is ExtraTypes-aware (its type predicates route
		// through hasTypeCtx, effects/compiled_predicate.go), so it stays
		// installed here and answers the same derived types the textual oracle
		// does -- the fast path is restored rather than discarded.
		if e.MatchesSpec(spec, id, source, ctrl, opts) && firstAttackOK(e, t, id) {
			return true
		}
	}
	return false
}

// PlayerHasMostLife reports whether p is alive and their life total is
// greater than or equal to every other LIVING player's (tied is enough). The
// Dethrone gate (CR 702.105) and Scourge of the Throne's Condition$
// AttackedPlayerWithMostLife share this one read so the two cannot drift: a
// p outside the player slice or already lost answers false, and only living
// seats count against the comparison (a dead larger total is no larger).
func PlayerHasMostLife(e Board, p state.PlayerID) bool {
	if int(p) >= len(e.Game().Players) || e.Game().Players[p].Lost {
		return false
	}
	life := e.Game().Players[p].Life
	for i := range e.Game().Players {
		if !e.Game().Players[i].Lost && e.Game().Players[i].Life > life {
			return false
		}
	}
	return true
}

// firstAttackOK reports whether the matched attacker passes the trigger's
// FirstAttack$ gate (nil-safe: a trigger without the param always passes).
func firstAttackOK(e Board, t cards.Trigger, id state.ObjID) bool {
	if v, ok := t.Param(cards.PKFirstAttack); !ok || !strings.EqualFold(strings.TrimSpace(v), "True") {
		return true
	}
	o := e.Game().Obj(id)
	return o != nil && o.AttacksThisTurn == 1
}

// AttackersDeclaredBatch reports whether a trig:AttackersDeclared line is the
// BATCH shape ("whenever you attack" / "whenever one or more creatures
// attack"): Mode$ AttackersDeclared with no per-defender AttackedTarget$.
// Only that shape draws its triggering condition from the whole declaration
// and latches once per declare step; Mode$ AttackersDeclaredOneTarget and an
// AttackersDeclared line that names an AttackedTarget$ are keyed to one
// defending player and keep firing once per that defender's event.
func AttackersDeclaredBatch(t cards.Trigger) bool {
	return t.Mode == "AttackersDeclared" && strings.TrimSpace(t.ParamStr(cards.PKAttackedTarget)) == ""
}

// AttackersDeclaredOneTargetMatches implements the "whenever [one or more]
// creatures attack a player" trigger (Forge Mode$ AttackersDeclaredOneTarget)
// and, routed to the same matcher, the batch "whenever you attack" trigger
// (Forge Mode$ AttackersDeclared).
//
// The two shapes differ in what one triggering condition is. A OneTarget line
// (and an AttackersDeclared line carrying AttackedTarget$) is keyed to a
// defending player: handleAttackers emits one DeclareAttackers event per
// defender, so it fires once for each attacked player -- correct as it stands.
// A BATCH line (AttackersDeclaredBatch) is keyed to the DECLARATION, which
// CR 508.1 makes a single turn-based action however many defenders are
// attacked, so its attacker set and its ValidAttackersAmount$ count are read
// from the WHOLE declaration (Engine.declaredAttackers, set by finishAttackers
// before the per-defender events are emitted) rather than from one event's
// ev.IDs; the once-per-declare-step latch itself lives at the queue point in
// checkFaceTriggers (Engine.attackersDeclaredFired). A direct synthetic emit
// with no declaration scratch falls back to ev.IDs, which is the declaration
// itself in every single-defender case.
func AttackersDeclaredOneTargetMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, remembered ...[]state.Target) bool {
	if ev.Kind != events.DeclareAttackers || len(ev.IDs) == 0 {
		return false
	}
	ctrl := e.ControllerOf(source)
	ids := ev.IDs
	if batch := e.Facts().DeclaredAttackers; AttackersDeclaredBatch(t) && len(batch) > 0 {
		ids = batch
	}
	attacker := e.ControllerOf(ids[0])
	ctx := effects.NewCtxPtr(source, ctrl, effects.CtxInit{})
	if src := e.Game().Obj(source); src != nil && src.Face() != nil {
		ctx.SVars = src.Face().SVars
	}
	if v := t.ParamStr(cards.PKAttackingPlayer); v != "" && !effects.MatchesPlayerSpecWithCounts(e.Game(), e.EvalCount, ctx, v, attacker, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKAttackedTarget); v != "" && !effects.MatchesPlayerSpecWithCounts(e.Game(), e.EvalCount, ctx, v, ev.Player, ctrl) {
		return false
	}
	matches := 0
	var capture []state.Target
	if len(remembered) != 0 {
		capture = remembered[0]
	}
	for _, id := range ids {
		if v := t.ParamStr(cards.PKValidAttackers); v == "" || e.MatchesSpec(v, id, source, ctrl, SpecOpts{DelayedRemembered: capture}) {
			matches++
		}
	}
	if matches == 0 {
		return false
	}
	if v := t.ParamStr(cards.PKValidAttackersAmount); v != "" && !ComparePresent(matches, v) {
		return false
	}
	return true
}

// ExertedMatches is the trig:Exerted half of CR 702.100 (task exert1 built the
// election and the static's own Trigger$ rider; this is the separate "whenever
// you exert a creature" listener a different script line carries). The event is
// events.Exert: Obj is the permanent the controller exerted and Amount >= 0 is
// the exert itself, while Amount == -1 is the untap-step consume marker
// rules/turn.go's scan emits -- bookkeeping, never an exert, so it must not
// fire. The exerted permanent is still on the battlefield at match time, so
// ValidCard$ reads the live object against the trigger source's controller
// ("you" = the listener's controller; all five corpus carriers write
// Creature.YouCtrl). ValidPlayer$/ValidSource$ are not read: measured, none of
// the five corpus lines carries either.
func ExertedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Exert || ev.Amount < 0 {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

// DamageMatches implements Mode$ DamageDone, DamageDealtOnce, DamageDoneOnce
// and DamageAll (the once-per-damage-batch gate itself lives in
// checkTriggers, alongside the cascade bound; this is purely the per-event
// parameter match, shared by all four modes). DamageAll additionally requires
// ValidSource$ and ValidTarget$ to NAME the same event's source and
// recipient (see the ValidSource$/ValidTarget$ reads below): the per-event
// match is the "both halves match" test, and checkTriggers' all-latch turns
// the first such event in a batch into the single "one or more" instance.
func DamageMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	return DamageMatchesWithCapture(e, t, source, ev, nil)
}

func DamageMatchesWithCapture(e Board, t cards.Trigger, source state.ObjID, ev events.Event, remembered []state.Target) bool {
	if ev.Kind != events.Damage {
		return false
	}
	// CombatDamage$ splits the mode between combat and noncombat damage
	// (CR 702.1x: combat damage is what the combat damage step's attackers
	// and blockers assign -- a DealDamage cast during that step is still not
	// combat damage). events.Event deliberately carries no such flag -- its
	// binary encoding is hash-chained and replayed -- so the distinction is
	// e.combatDamaging (engine.go), set only around dealCombatDamage's
	// assignment loop (combat.go) and read here synchronously inside emit's
	// checkTriggers; replay rebuilds it by re-executing the same setter.
	// CombatDamage$ False is the complement (16 corpus trigger lines): only
	// noncombat damage, so an in-flight combat assignment fails it. Before
	// the flag existed True returned false unconditionally (978 dead corpus
	// trigger lines, Umezawa's Jitte among them) and False fell through and
	// matched everything.
	switch cd := t.ParamStr(cards.PKCombatDamage); {
	case strings.EqualFold(cd, "True") && !e.Facts().CombatDamaging:
		return false
	case strings.EqualFold(cd, "False") && e.Facts().CombatDamaging:
		return false
	}
	ctrl := e.ControllerOf(source)
	if v, ok := t.Param(cards.PKValidSource); ok {
		// The damage's source, through the ONE shared dealer resolution
		// (DamageEventSource, whose doc carries the full priority rationale:
		// the published override, e.damaging during combat's assignment loop,
		// else the resolving stack object).
		src := DamageEventSource(e)
		if src == 0 || !e.MatchesSpec(v, src, source, ctrl, SpecOpts{DelayedRemembered: remembered}) {
			return false
		}
	}
	if v, ok := t.Param(cards.PKValidTarget); ok {
		if ev.Obj != 0 {
			if !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{DelayedRemembered: remembered}) {
				return false
			}
		} else if !effects.MatchesPlayerSpecCtx(e.Game(), v, ev.Player, ctrl, effects.PlayerSpecCtx{
			Source: source, DefendingPlayer: damageDefendingPlayer(e, ev), DelayedRemembered: remembered,
			Layers: e.BoardLayers(),
		}) {
			return false
		}
	}
	// FirstTime$ True on DamageDoneOnce (task agent-20261001T020044Z; only
	// Hercules, Olympian Hero carries the combination corpus-wide) admits
	// exactly the first Damage event on that recipient this turn, mirroring
	// the life-gain/life-loss gates in trigmatch_life.go. Scoped to
	// DamageDoneOnce's own mode -- no DamageDone, DamageDealtOnce or DamageAll
	// trigger in the corpus carries FirstTime$, so the other three modes are
	// byte-identical. The per-batch once-latch in trigger_match.go already
	// deduplicates within one batch; this gate kills the LATER batches, whose
	// events the latch never sees. emit logs and folds the event into state
	// before checkTriggers runs, so a state field cannot distinguish the
	// first hit -- the replay-stable log scan below can, counting the current
	// event as one.
	if t.Mode == "DamageDoneOnce" && strings.EqualFold(t.ParamStr(cards.PKFirstTime), "True") && !firstDamageToThisTurn(e, ev) {
		return false
	}
	return true
}

// firstDamageToThisTurn is the damage mirror of firstLifeLossThisTurn
// (trigmatch_life.go): scanning the log backwards, true only for the newest
// Damage event on this recipient (same Obj/Player pair: object recipients
// carry nonzero Obj, player recipients Obj == 0 plus a Player) in the current
// turn, stopping at TurnChange. Same replay-stable log scan -- no new event
// kind, no engine field, survives Clone.
//
// Non-positive Amounts are NOT damage and are skipped, exactly as
// lifeLoss requires Amount > 0 and the DamageDoneOnce batch latch
// (trigger_match.go) states: the cleanup/regeneration repair path emits a
// zero-or-negative Damage event to clear marked damage, and a 0-power
// combat assignment emits Amount == 0. Neither is a hit for FirstTime$.
func firstDamageToThisTurn(e Board, ev events.Event) bool {
	seenCurrent := false
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		le := e.Log().Events[i]
		if le.Kind == events.TurnChange {
			return seenCurrent
		}
		if le.Kind != events.Damage || le.Amount <= 0 || le.Obj != ev.Obj || le.Player != ev.Player {
			continue
		}
		if seenCurrent {
			return false
		}
		seenCurrent = true
	}
	return seenCurrent
}

// damageDefendingPlayer binds the combat defending-player role only during
// the combat damage assignment window and only for player recipients. A Damage
// event's recipient alone is not evidence of that role: noncombat damage also
// carries Player, and combat damage may be assigned to a planeswalker.
func damageDefendingPlayer(e Board, ev events.Event) state.Target {
	if !e.Facts().CombatDamaging || ev.Obj != 0 {
		return state.Target{}
	}
	return state.Target{Player: ev.Player, IsPlayer: true}
}

// DamagePreventedMatches implements Mode$ DamagePreventedOnce (task dponce1):
// the trigger fires on a STORED prevention Note -- the re-entrant Note the
// full-prevention replacement arm (rules/replacement.go
// applyNonMoveReplacements) and the ReplaceDamage/protection siblings emit
// when damage is prevented. The Note carries the prevented damage in Amount
// (0 for Fog's whole-pass statement, which is deliberately excluded -- a
// whole-turn statement is not "damage that would be dealt to you is
// prevented") and names the damaged side in Obj/Player exactly like the
// DamageDone trigger's event does, so ValidTarget$ reads the same grammar:
// the damaged object when the hit was object-directed, the damaged player
// otherwise. There is no Once latch: each stored prevention Note is one
// occurrence, so two prevented hits in one turn fire twice, each with its
// own amount.
func DamagePreventedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Note || ev.Amount <= 0 {
		return false
	}
	if !strings.Contains(strings.ToLower(ev.Text), "prevent") {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v, ok := t.Param(cards.PKValidTarget); ok {
		if ev.Obj != 0 {
			if !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
				return false
			}
		} else if !effects.MatchesPlayerSpecCtx(e.Game(), v, ev.Player, ctrl, effects.PlayerSpecCtx{
			Source:          source,
			DefendingPlayer: damageDefendingPlayer(e, ev),
			Layers:          e.BoardLayers(),
		}) {
			return false
		}
	}
	return true
}

// damageSource identifies who dealt a just-emitted Damage event, for
// ValidSource$ matching. events.Event carries no explicit source field for
// Damage -- every Damage event this build emits (effects/damage.go's
// DealDamage/DamageAll) comes from a primitive running inside Resolve,
// called only from resolveTop while the resolving spell or ability is still
// the top of the stack (resolveTop pops it only after Resolve returns), so
// the current stack top is that source for every code path this build has
// today. Two overrides win over the stack top, both rebuilt by replay
// because replay re-executes the same setter: the published damage-source
// override (rules.Engine.SetDamageSource -- DamageSource$ and the unwrapped
// ability source, so a ValidSource$ trigger matches the PERMANENT that dealt
// it, never the ability wrapper the stack top names) and the dealing
// creature during combat's assignment loop (e.damaging). Any Damage emission
// outside ability resolution would need Event to carry an explicit source
// instead of relying on this.
func damageSource(e Board) state.ObjID {
	if o := e.Facts().DmgSrcOverride; o != 0 {
		return o
	}
	stack := e.Game().Stack
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1]
}

// DamageEventSource is the ONE dealer resolution for a just-emitted Damage
// event, shared by the ValidSource$ match (DamageMatches), the DamageDealtOnce
// latch and the DamageAll batch-set capture, so a captured batch set can never
// name a source the matcher would not have matched. The priority is the
// DamageMatches comment's three: an explicit published override
// (rules.Engine.SetDamageSource -- DamageSource$ names the PERMANENT that
// dealt it, never the ability wrapper resolving it) wins over the dealing
// creature during combat's assignment loop (e.damaging -- the stack is
// USUALLY empty during combat but not always: the between-passes priority
// round can leave a first-strike trigger on the stack while the regular pass
// deals), and otherwise the resolving spell or ability while it is the stack
// top (damageSource).
func DamageEventSource(e Board) state.ObjID {
	f := e.Facts()
	if f.DmgSrcOverride != 0 {
		return f.DmgSrcOverride
	}
	if f.CombatDamaging {
		return f.Damaging
	}
	return damageSource(e)
}

func init() {
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return AttacksMatches(e, t, source, ev)
	}, "Attacks")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return AttackersDeclaredOneTargetMatches(e, t, source, ev)
	}, "AttackersDeclared", "AttackersDeclaredOneTarget")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return ExertedMatches(e, t, source, ev)
	}, "Exerted")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return DamageMatches(e, t, source, ev)
	}, "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll", "ExcessDamageAll")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return DamagePreventedMatches(e, t, source, ev)
	}, "DamagePreventedOnce")
}
