// Combat trigger modes.
//
// Mode$ Attacks, AttackersDeclared(OneTarget), Blocks, AttackerBlocked(ByCreature),
// AttackerUnblockedOnce, Exerted, DamageDone/DamageDealtOnce/DamageDoneOnce and
// DamagePreventedOnce.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attacksMatches implements Mode$ Attacks against a DeclareAttackers event.
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
func (e *Engine) attacksMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.DeclareAttackers {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(t.Params["Myriad"]), "True") {
		// Myriad$ True is the myriad keyword expansion's own marker
		// (cards/keywords.go addKeywordTrigger): the per-other-opponent token
		// copies are the DB$ Myriad body, so the marker requires the
		// trigger's Execute sub to resolve to exactly that body -- a
		// mismatched or unresolvable expansion must not fire.
		src := e.G.Obj(source)
		if src == nil || src.Face() == nil {
			return false
		}
		sa := cards.ResolveSVar(src.Face().SVars, t.Params["Execute"])
		if sa == nil || sa.API != "Myriad" {
			return false
		}
	}
	if v, ok := t.Params["Alone"]; ok && strings.EqualFold(v, "True") && len(ev.IDs) != 1 {
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
	if v := t.Params["Attacked"]; v != "" {
		if !effects.MatchesPlayerSpecFrom(e.G, v, ev.Player, e.controllerOf(source), source) {
			return false
		}
	}
	// Dethrone (CR 702.105) fires only when the attacked player has the
	// greatest life total (tied is enough) among ALL players. Comparing only
	// the attacker and its defender is wrong in multiplayer: a third player
	// with more life prevents the trigger even though it was not attacked.
	if v, ok := t.Params["Dethrone"]; ok && strings.EqualFold(v, "True") {
		if !e.playerHasMostLife(ev.Player) {
			return false
		}
	}
	// Condition$ AttackedPlayerWithMostLife (Scourge of the Throne, the
	// corpus's one carrier of this spelling on a trigger line): the
	// "if it's attacking the player with the most life or tied for most
	// life" intervening-if. It is the SAME gate Dethrone reads -- one shared
	// playerHasMostLife helper, so the two cannot drift -- evaluated on the
	// event's declared defender, which is why it lives in the per-mode
	// matcher beside Dethrone and not in the shared triggerConditionHolds
	// walk (no event there to name the defender). Mirroring Dethrone, the
	// gate is fire-time only: the resolution-time CR 603.4 recheck cannot
	// re-derive the attacked player from the event, the same scope every
	// other event-relative matcher gate here keeps.
	if strings.EqualFold(strings.TrimSpace(t.Params["Condition"]), "AttackedPlayerWithMostLife") {
		if !e.playerHasMostLife(ev.Player) {
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
	if v, ok := t.Params["Training"]; ok && strings.EqualFold(v, "True") {
		attackers := e.declaredAttackers
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
	spec, ok := t.Params["ValidCard"]
	if !ok {
		for _, id := range ev.IDs {
			if id == source {
				return e.firstAttackOK(t, id)
			}
		}
		return false
	}
	ctrl := e.controllerOf(source)
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
		sc := e.specCtx(source, ctrl)
		sc.ExtraTypes = e.Derived(id).Types
		// The compiled sidecar is ExtraTypes-aware (its type predicates route
		// through hasTypeCtx, effects/compiled_predicate.go), so it stays
		// installed here and answers the same derived types the textual oracle
		// does -- the fast path is restored rather than discarded.
		if e.matchesSpec(spec, id, sc) && e.firstAttackOK(t, id) {
			return true
		}
	}
	return false
}

// playerHasMostLife reports whether p is alive and their life total is
// greater than or equal to every other LIVING player's (tied is enough). The
// Dethrone gate (CR 702.105) and Scourge of the Throne's Condition$
// AttackedPlayerWithMostLife share this one read so the two cannot drift: a
// p outside the player slice or already lost answers false, and only living
// seats count against the comparison (a dead larger total is no larger).
func (e *Engine) playerHasMostLife(p state.PlayerID) bool {
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost {
		return false
	}
	life := e.G.Players[p].Life
	for i := range e.G.Players {
		if !e.G.Players[i].Lost && e.G.Players[i].Life > life {
			return false
		}
	}
	return true
}

// firstAttackOK reports whether the matched attacker passes the trigger's
// FirstAttack$ gate (nil-safe: a trigger without the param always passes).
func (e *Engine) firstAttackOK(t cards.Trigger, id state.ObjID) bool {
	if v, ok := t.Params["FirstAttack"]; !ok || !strings.EqualFold(strings.TrimSpace(v), "True") {
		return true
	}
	o := e.G.Obj(id)
	return o != nil && o.AttacksThisTurn == 1
}

// attackersDeclaredBatch reports whether a trig:AttackersDeclared line is the
// BATCH shape ("whenever you attack" / "whenever one or more creatures
// attack"): Mode$ AttackersDeclared with no per-defender AttackedTarget$.
// Only that shape draws its triggering condition from the whole declaration
// and latches once per declare step; Mode$ AttackersDeclaredOneTarget and an
// AttackersDeclared line that names an AttackedTarget$ are keyed to one
// defending player and keep firing once per that defender's event.
func attackersDeclaredBatch(t cards.Trigger) bool {
	return t.Mode == "AttackersDeclared" && strings.TrimSpace(t.Params["AttackedTarget"]) == ""
}

// attackersDeclaredOneTargetMatches implements the "whenever [one or more]
// creatures attack a player" trigger (Forge Mode$ AttackersDeclaredOneTarget)
// and, routed to the same matcher, the batch "whenever you attack" trigger
// (Forge Mode$ AttackersDeclared).
//
// The two shapes differ in what one triggering condition is. A OneTarget line
// (and an AttackersDeclared line carrying AttackedTarget$) is keyed to a
// defending player: handleAttackers emits one DeclareAttackers event per
// defender, so it fires once for each attacked player -- correct as it stands.
// A BATCH line (attackersDeclaredBatch) is keyed to the DECLARATION, which
// CR 508.1 makes a single turn-based action however many defenders are
// attacked, so its attacker set and its ValidAttackersAmount$ count are read
// from the WHOLE declaration (Engine.declaredAttackers, set by finishAttackers
// before the per-defender events are emitted) rather than from one event's
// ev.IDs; the once-per-declare-step latch itself lives at the queue point in
// checkFaceTriggers (Engine.attackersDeclaredFired). A direct synthetic emit
// with no declaration scratch falls back to ev.IDs, which is the declaration
// itself in every single-defender case.
func (e *Engine) attackersDeclaredOneTargetMatches(t cards.Trigger, source state.ObjID, ev events.Event, remembered ...[]state.Target) bool {
	if ev.Kind != events.DeclareAttackers || len(ev.IDs) == 0 {
		return false
	}
	ctrl := e.controllerOf(source)
	ids := ev.IDs
	if attackersDeclaredBatch(t) && len(e.declaredAttackers) > 0 {
		ids = e.declaredAttackers
	}
	attacker := e.controllerOf(ids[0])
	ctx := &effects.Ctx{Source: source, Controller: ctrl}
	if src := e.G.Obj(source); src != nil && src.Face() != nil {
		ctx.SVars = src.Face().SVars
	}
	if v := t.Params["AttackingPlayer"]; v != "" && !effects.MatchesPlayerSpecWithSVars(e, ctx, v, attacker, ctrl) {
		return false
	}
	if v := t.Params["AttackedTarget"]; v != "" && !effects.MatchesPlayerSpecWithSVars(e, ctx, v, ev.Player, ctrl) {
		return false
	}
	matches := 0
	var capture []state.Target
	if len(remembered) != 0 {
		capture = remembered[0]
	}
	for _, id := range ids {
		sc := delayedSpecCtx(e.specCtx(source, ctrl), capture)
		if v := t.Params["ValidAttackers"]; v == "" || e.matchesSpec(v, id, sc) {
			matches++
		}
	}
	if matches == 0 {
		return false
	}
	if v := t.Params["ValidAttackersAmount"]; v != "" && !comparePresent(matches, v) {
		return false
	}
	return true
}

// attackerBlockedCandidates lists the attackers one become-blocked trigger
// fires for (Forge Mode$ AttackerBlocked; She-Hulk, Wallbreaker's "Whenever
// a Hero you control becomes blocked"). A DeclareBlockers event's Pairs
// name exactly the attacker-blocker assignments this defender's declaration
// just made -- an attacker already carrying blockers is never re-paired, so
// the declared pairs ARE the became-blocked transition, and a trigger fires
// once per DISTINCT matching attacker (two Heroes blocked by one
// declaration are two trigger instances, CR 603.2c). Deterministic order:
// the event's own pair order, deduplicated.
func (e *Engine) attackerBlockedCandidates(t cards.Trigger, source state.ObjID, ev events.Event) []state.ObjID {
	if ev.Kind != events.DeclareBlockers || len(ev.Pairs) == 0 {
		return nil
	}
	ctrl := e.controllerOf(source)
	seen := map[state.ObjID]bool{}
	var out []state.ObjID
	for _, pr := range ev.Pairs {
		a := pr[0]
		if seen[a] {
			continue
		}
		seen[a] = true
		if v := t.Params["ValidCard"]; v != "" && !e.matchesSpec(v, a, e.specCtx(source, ctrl)) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// attackerBlockedByPairCandidates lists the (attacker, blocker) pairs one
// Forge Mode$ AttackerBlockedByCreature trigger fires for (kw:Flanking's
// expansion, CR 702.25a: "whenever this creature becomes blocked by a
// creature without flanking"). Each declared pair whose ATTACKER is the
// trigger's own source, matches ValidCard$, and whose blocker matches
// ValidBlocker$ yields one instance; a blocker WITH flanking matches nothing,
// so it debuffs nobody. ValidCard$ Card.Self works because the trigger's
// source IS the flanking attacker. The sibling "blocks" half of Forge's mode
// names the BLOCKER as its source and never reaches here (see the loop).
func (e *Engine) attackerBlockedByPairCandidates(t cards.Trigger, source state.ObjID, ev events.Event) [][2]state.ObjID {
	if ev.Kind != events.DeclareBlockers || len(ev.Pairs) == 0 {
		return nil
	}
	ctrl := e.controllerOf(source)
	sc := e.specCtx(source, ctrl)
	var out [][2]state.ObjID
	for _, pr := range ev.Pairs {
		// The trigger's SOURCE must be the pair's ATTACKER. This hook binds the
		// blocker as the remembered object, so it is only correct for the
		// "becomes blocked" half of Forge's mode (kw:Flanking is its only live
		// carrier). The sibling "blocks" half spells its source as the BLOCKER
		// (ValidCard$ Creature | ValidBlocker$ Card.Self) and names the attacker
		// in its body (Defined$ TriggeredAttackerLKICopy); queueing it here would
		// resolve that referent to the remembered BLOCKER -- the source itself --
		// and make the creature damage/lose life to itself. That half stays inert
		// (role-correct referents need a second remembered slot, a separate task).
		if pr[0] != source {
			continue
		}
		if v := t.Params["ValidCard"]; v != "" {
			asc := sc
			asc.ExtraKeywords = e.Derived(pr[0]).Keywords
			asc.PredicatePrograms = nil
			if !e.matchesSpec(v, pr[0], asc) {
				continue
			}
		}
		if v := t.Params["ValidBlocker"]; v != "" {
			// The blocker's DERIVED keyword list is what `withoutFlanking` must
			// read: a blocker granted flanking by a layer-6 AddKeyword$ (Agility,
			// Flanking Licid, Sidewinder Sliver, Cavalry Master) HAS flanking and
			// takes no -1/-1. The object-alone read the filter would otherwise
			// use sees only the printed face plus marker counters.
			bsc := sc
			bsc.ExtraKeywords = e.Derived(pr[1]).Keywords
			bsc.PredicatePrograms = nil
			if !e.matchesSpec(v, pr[1], bsc) {
				continue
			}
		}
		out = append(out, pr)
	}
	return out
}

// flankingPumpSA is kw:Flanking's resolution body (CR 702.25a): the blocked
// creature gets -1/-1 until end of turn through the ordinary pump/continuous
// path, naming the blocker the hook remembered. It is a package-level value so
// a printed K:Flanking expansion and a granted flanking instance resolve to
// the SAME body.
var flankingPumpSA = &cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{
	"Defined": "TriggeredBlockerLKICopy", "NumAtt": "-1", "NumDef": "-1",
}}

// flankingTrigger is the trigger shape flanking instances share; only its
// ValidBlocker$ spec matters (the hook supplies the source and the pairs).
func flankingTrigger() cards.Trigger {
	return cards.Trigger{Mode: "AttackerBlockedByCreature", Params: map[string]string{
		"Mode": "AttackerBlockedByCreature", "ValidCard": "Card.Self",
		"ValidBlocker": "Creature.withoutFlanking", "TriggerZones": "Battlefield",
		"Keyword": "Flanking",
	}, Effect: flankingPumpSA}
}

// isFlankingMarker reports whether a face trigger is kw:Flanking's own
// expansion (addKeywordTrigger tags it Keyword$ Flanking). Such a trigger is a
// MARKER: the derived-keyword flanking walk (queueGrantedFlanking and the
// in-loop multiplication) owns the instance count, so the marker fires once
// per DERIVED instance, not once per line -- and a creature granted flanking
// with no printed line fires through the synthesized path instead.
func isFlankingMarker(t cards.Trigger) bool {
	return t.Mode == "AttackerBlockedByCreature" && strings.EqualFold(strings.TrimSpace(t.Params["Keyword"]), "Flanking")
}

// flankingInstances is the number of DERIVED flanking instances an object has
// (CR 702.25b: each instance triggers separately). The derived keyword list
// already folds the printed K:Flanking line, marker-counter grants and
// layer-6 AddKeyword$ grants, and it preserves duplicates -- so Cavalry
// Master's "other creatures you control with flanking have flanking" gives an
// already-flanking creature a second instance and it triggers twice.
func (e *Engine) flankingInstances(id state.ObjID) int {
	n := 0
	for _, k := range e.Derived(id).Keywords {
		if strings.EqualFold(cards.KeywordHead(k), "Flanking") {
			n++
		}
	}
	return n
}

// queueGrantedFlanking fires the flanking trigger for a creature that has the
// keyword DERIVED but does NOT print a K:Flanking line -- the layer-6
// AddKeyword$ carriers (Agility, Flanking Licid, Sidewinder Sliver, Cavalry
// Master). A granted keyword has no face trigger for the ordinary loop to
// index, so its instance rides a KeywordTriggerPush whose __kwFlanking:
// payload events.Apply rebuilds into the same pump body (pushTrigger's
// Flanking branch, the Ward/Afflict shape). Printed flanking is handled by the
// in-loop marker branch, which reuses the face trigger's own TriggerPush path;
// running both would double-count a printed flanking creature, so the helper
// defers whenever a marker exists. One trigger per NON-FLANKING blocker per
// derived instance (CR 702.25a/b).
func (e *Engine) queueGrantedFlanking(id state.ObjID, o *state.Object, ev events.Event) {
	f := o.Face()
	if f == nil {
		return
	}
	for _, t := range f.Triggers {
		if isFlankingMarker(t) {
			return // printed path owns this creature's flanking
		}
	}
	instances := e.flankingInstances(id)
	if instances == 0 {
		return
	}
	tr := flankingTrigger()
	for _, pr := range e.attackerBlockedByPairCandidates(tr, id, ev) {
		bid := pr[1]
		for i := 0; i < instances; i++ {
			key := triggerKey{Source: id, Idx: -1}
			if e.triggerFireCount == nil {
				e.triggerFireCount = map[triggerKey]int32{}
			}
			if e.triggerFireCount[key] >= maxTriggerFires {
				return
			}
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     id,
				Controller: o.Controller,
				Idx:        -1,
				Flanking:   true,
				Ctx: effects.Ctx{
					Source:     id,
					Controller: o.Controller,
					Remembered: []state.Target{{Obj: bid}},
					Captured:   []state.Target{{Obj: bid}},
					TriggerContext: effects.TriggerContext{
						TriggerCard:    bid,
						TriggerSource:  pr[0],
						TriggerBlocker: bid,
					},
				},
			})
		}
	}
}

// checkAttackerBlockedTriggers queues trigger instances off a DeclareBlockers
// event for the two become-blocked modes the ordinary face scan cannot express
// (it queues at most one entry per trigger per event, and both referents are
// per-attacker or per-pair): Mode$ AttackerBlocked fires once per DISTINCT
// matching blocked attacker (She-Hulk's counter count is each Hero's OWN
// blocker count), and Forge Mode$ AttackerBlockedByCreature -- kw:Flanking's
// expansion (CR 702.25a) is its only live carrier -- fires once per matching
// (attacker, blocker) PAIR, the blocker remembered as the
// TriggeredBlockerLKICopy referent. The same-scan-hook precedent is
// checkChapterTriggers (rules/saga.go). The gates mirror the ordinary scan's
// per-trigger sequence (zone, phase, fire-count bound, ActivationLimit$);
// Secondary$ and the Once damage-batch gates do not exist on these modes.
// Each per-instance ctx carries the triggering objects as Remembered and as
// TriggerCard, so Count$Valid Creature.blockingTriggeredAttacker counts that
// Hero's blockers and Defined$ TriggeredBlockerLKICopy names the blocker.
func (e *Engine) checkAttackerBlockedTriggers(ev events.Event) {
	if ev.Kind != events.DeclareBlockers {
		return
	}
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		// Flanking instances come from the DERIVED keyword list, not the
		// printed trigger: a creature granted flanking (Agility, Sidewinder
		// Sliver, Cavalry Master) has no face trigger to fire. This runs
		// BEFORE the faceMayTrigger early return because a granted-only
		// creature has no printed trigger line to make that gate true, and it
		// defers to the in-loop marker branch when a printed K:Flanking
		// exists (that branch owns the printed instance count).
		if o.Zone == state.ZBattlefield && o.IsAttacking {
			e.queueGrantedFlanking(id, o, ev)
		}
		if !o.Unlocked && !e.faceMayTrigger(f, ev.Kind) {
			return
		}
		for ti, t := range f.Triggers {
			if t.Mode != "AttackerBlocked" && t.Mode != "AttackerBlockedByCreature" {
				continue
			}
			e.queueAttackerBlockedTrigger(t, id, o.Controller, ti, false, 0, ev)
		}
	})
	e.checkGrantedAttackerBlockedTriggers(ev)
}

// queueAttackerBlockedTrigger queues the instances one become-blocked trigger
// fires for a DeclareBlockers event. Printed triggers and AddTrigger$-GRANTED
// ones share this helper so their zone/phase gates, referent capture and
// limit discipline cannot drift apart (queueAttackerUnblockedTrigger's
// precedent). t.Mode is either Mode$ AttackerBlocked (one instance per
// DISTINCT matching blocked attacker) or Mode$ AttackerBlockedByCreature (one
// instance per matching (attacker, blocker) pair). source is the permanent
// carrying the trigger (its own face for a printed line, the AFFECTED
// recipient for a grant) and idx is its face Triggers index, or -1 for a
// grant. granted/grantor/Execute carry the GrantTriggerPush provenance a
// granted instance needs and are left zero for a printed one.
func (e *Engine) queueAttackerBlockedTrigger(t cards.Trigger, source state.ObjID, controller state.PlayerID, idx int, granted bool, grantor state.ObjID, ev events.Event) {
	if t.Mode != "AttackerBlocked" && t.Mode != "AttackerBlockedByCreature" {
		return
	}
	// t.Effect is nil for a printed trigger with no body; the queue gate below
	// keeps the pre-existing behaviour (nothing queued, no limit consumed).
	if t.Effect == nil {
		return
	}
	if !e.zoneGate(t, source, ev) || !e.phaseGate(t) {
		return
	}
	key := triggerKey{Source: source, Idx: idx}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	if e.triggerFireCount[key] >= maxTriggerFires {
		return // cascade bound: see maxTriggerFires.
	}
	if !e.triggerGameActivationLimitAllows(t, key) {
		return // GameActivationLimit$: already triggered enough this game.
	}
	if actionTriggerModes[t.Mode] && !e.triggerActivationLimitAllows(t, key) {
		return
	}
	// The two limit gates above are READ-ONLY: a DeclareBlockers event whose
	// pairs match nothing queues nothing and must not consume a use of either
	// limit. The counts commit below, at the first instance actually appended.
	reserved := false
	reserve := func() {
		if reserved {
			return
		}
		reserved = true
		e.reserveTriggerLimits(t, key)
	}
	pt := func(p state.PlayerID) state.Target { return state.Target{Player: p, IsPlayer: true} }
	if t.Mode == "AttackerBlockedByCreature" {
		// CR 702.25a: one instance per (attacker, non-flanking blocker) pair;
		// the trigger's controller is the ATTACKER's controller, which the
		// Source/Controller pair already are (the source is the flanking
		// attacker itself). A kw:Flanking marker fires its trigger once per
		// DERIVED flanking instance (CR 702.25b), so a creature with a printed
		// instance plus a granted one -- Cavalry Master's lord -- debuffs a
		// blocker twice; a marker whose keyword has since been removed fires
		// not at all. A granted instance has no printed marker, so its derived
		// flanking instances are its own to fire (any Keyword$ Flanking param
		// on the grant is not consulted: the grant itself is the instance).
		instances := 1
		if isFlankingMarker(t) {
			instances = e.flankingInstances(source)
		}
		for _, pr := range e.attackerBlockedByPairCandidates(t, source, ev) {
			bid := pr[1]
			for i := 0; i < instances; i++ {
				if e.triggerFireCount[key] >= maxTriggerFires {
					break
				}
				reserve()
				e.triggerFireCount[key]++
				e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
					Source:     source,
					Controller: controller,
					Idx:        idx,
					SA:         t.Effect,
					Granted:    granted,
					Grantor:    grantor,
					Execute:    t.Params["Execute"],
					Ctx: effects.Ctx{
						Source:     source,
						Controller: controller,
						Remembered: []state.Target{{Obj: bid}},
						Captured:   []state.Target{{Obj: bid}},
						TriggerContext: effects.TriggerContext{
							TriggerCard:   bid,
							TriggerSource: pr[0],
						},
					},
				})
			}
		}
		return
	}
	for _, aid := range e.attackerBlockedCandidates(t, source, ev) {
		defender := pt(0)
		if ao := e.G.Obj(aid); ao != nil {
			defender = pt(ao.Attacking)
		}
		reserve()
		e.triggerFireCount[key]++
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     source,
			Controller: controller,
			Idx:        idx,
			SA:         t.Effect,
			Granted:    granted,
			Grantor:    grantor,
			Execute:    t.Params["Execute"],
			Ctx: effects.Ctx{
				Source:     source,
				Controller: controller,
				Remembered: []state.Target{{Obj: aid}},
				Captured:   []state.Target{{Obj: aid}},
				TriggerContext: effects.TriggerContext{
					TriggerCard:     aid,
					TriggerSource:   aid,
					AttackingPlayer: pt(e.controllerOf(aid)),
					DefendingPlayer: defender,
				},
			},
		})
	}
}

// checkGrantedAttackerBlockedTriggers is the AddTrigger$ half of the
// become-blocked walk. The ordinary granted-trigger event matcher
// (checkGrantedStaticTriggersUsing) cannot dispatch these two modes: neither
// has an entry in trigMatchers (they are dedicated hooks), so triggerMatches
// rejects every DeclareBlockers event for them and a granted instance would
// never fire. Instead each live grant whose Mode$ is AttackerBlocked or
// AttackerBlockedByCreature queues through the same per-instance helper a
// printed trigger uses, preserving its grantor so GrantTriggerPush can
// rebuild the Execute$ body during replay (checkGrantedAttackerUnblockedTriggers'
// precedent). Stormsurge Kraken, Retaliation and Mirror Shield are the
// corpus carriers.
func (e *Engine) checkGrantedAttackerBlockedTriggers(ev events.Event) {
	for _, ce := range e.active() {
		if ce.AddTrigger == nil {
			continue
		}
		if ce.AddTrigger.Mode != "AttackerBlocked" && ce.AddTrigger.Mode != "AttackerBlockedByCreature" {
			continue
		}
		grantorID := ce.Source
		if ce.TriggerGrantor != 0 {
			grantorID = ce.TriggerGrantor
		}
		grantor := e.G.Obj(grantorID)
		if grantor == nil || grantor.Face() == nil {
			continue
		}
		t := *ce.AddTrigger
		t.Effect = grantedTriggerExecute(grantor, t.Params["Execute"])
		if t.Effect == nil {
			continue
		}
		e.forEachObject(func(id state.ObjID) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				return
			}
			if !e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
				return
			}
			e.queueAttackerBlockedTrigger(t, id, o.Controller, -1, true, grantorID, ev)
		})
	}
}

// checkAttackerUnblockedTriggers queues one Mode$ AttackerUnblocked instance
// for every matching unblocked attacker at declare-blockers round completion.
// Unlike AttackerUnblockedOnce, this mode matches ValidCard$ against the
// attacker and ValidDefender$ against that attacker's actual defender.
func (e *Engine) checkAttackerUnblockedTriggers() {
	ev := events.Event{Kind: events.DeclareBlockers}
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		f := o.Face()
		if !o.Unlocked && !e.faceMayTrigger(f, ev.Kind) {
			return
		}
		for ti, t := range f.Triggers {
			e.queueAttackerUnblockedTrigger(t, id, o.Controller, ti, false, 0, nil, ev)
		}
	})
	e.checkGrantedAttackerUnblockedTriggers(ev)
}

// checkGrantedAttackerUnblockedTriggers is the AddTrigger$ half of the
// round-complete unblocked-attacker walk. The ordinary granted-trigger event
// matcher cannot dispatch this mode: it has no synthetic DeclareBlockers event
// carrying all unblocked attackers. Instead each live grant queues through the
// same per-attacker helper as a printed trigger, preserving its grantor so
// GrantTriggerPush can rebuild the Execute$ body during replay.
func (e *Engine) checkGrantedAttackerUnblockedTriggers(ev events.Event) {
	for _, ce := range e.active() {
		if ce.AddTrigger == nil || ce.AddTrigger.Mode != "AttackerUnblocked" {
			continue
		}
		grantorID := ce.Source
		if ce.TriggerGrantor != 0 {
			grantorID = ce.TriggerGrantor
		}
		grantor := e.G.Obj(grantorID)
		if grantor == nil || grantor.Face() == nil {
			continue
		}
		t := *ce.AddTrigger
		grantFace := grantedTriggerFace(grantor, t.Params["Execute"])
		if grantFace == nil {
			continue
		}
		t.Effect = cards.ResolveSVar(grantFace.SVars, t.Params["Execute"])
		if t.Effect == nil {
			continue
		}
		e.forEachObject(func(id state.ObjID) {
			o := e.G.Obj(id)
			if o == nil || !e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
				return
			}
			e.queueAttackerUnblockedTrigger(t, id, o.Controller, -1, true, grantorID, grantFace.SVars, ev)
		})
	}
}

// queueAttackerUnblockedTrigger queues one instance for every matching
// unblocked attacker. Printed and AddTrigger$-granted instances share this
// path so their ValidCard$/ValidDefender$ gates, captured attacker roles and
// action-trigger limits cannot drift apart. ownedSVars is the trigger LINE's
// owning SVar table, supplied for a granted instance (the GRANTOR's table) and
// nil for a printed one; it is what a CheckSVar$/SVarCompare$ clause on the
// line is evaluated against at fire time.
func (e *Engine) queueAttackerUnblockedTrigger(t cards.Trigger, source state.ObjID, controller state.PlayerID, idx int, granted bool, grantor state.ObjID, ownedSVars map[string]string, ev events.Event) {
	if t.Mode != "AttackerUnblocked" || t.Effect == nil ||
		!e.zoneGate(t, source, ev) || !e.phaseGate(t) ||
		!e.triggerConditionHoldsWithSVars(t, source, controller, nil, ownedSVars) {
		return
	}
	key := triggerKey{Source: source, Idx: idx}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	if e.triggerFireCount[key] >= maxTriggerFires || !e.triggerGameActivationLimitAllows(t, key) ||
		(actionTriggerModes[t.Mode] && !e.triggerActivationLimitAllows(t, key)) {
		return
	}
	pt := func(p state.PlayerID) state.Target { return state.Target{Player: p, IsPlayer: true} }
	// The read-only limit gate consumes a use only after the first matching
	// attacker actually queues an instance. Multiple unblocked attackers still
	// produce their required individual triggers.
	reserved := false
	for _, p := range e.G.AliveFrom(0) {
		for _, aid := range e.G.Zone(state.ZBattlefield, p) {
			a := e.G.Obj(aid)
			if a == nil || !a.IsAttacking || len(a.BlockedBy) != 0 {
				continue
			}
			if v := t.Params["ValidCard"]; v != "" {
				// The IsGoaded static route (staticgoad1), bound inline -- the
				// same shape matchesSpec keeps (this walk runs per attacker per
				// Attacks event, so the context must not escape through a
				// helper call).
				sc := e.specCtx(source, controller)
				if e.goadProbe == 0 && strings.Contains(v, "IsGoaded") {
					sc.StaticGoads = e.staticallyGoaded()
				}
				if !effects.MatchesSpecCtx(e.G, v, aid, sc) {
					continue
				}
			}
			if v := t.Params["ValidDefender"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, a.Attacking, controller) {
				continue
			}
			if !reserved {
				reserved = true
				e.reserveTriggerLimits(t, key)
			}
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source: source, Controller: controller, Idx: idx, SA: t.Effect,
				Granted: granted, Grantor: grantor, Execute: t.Params["Execute"],
				Ctx: effects.Ctx{
					Source: source, Controller: controller,
					Remembered: []state.Target{{Obj: aid}}, Captured: []state.Target{{Obj: aid}},
					TriggerContext: effects.TriggerContext{
						TriggerCard: aid, TriggerSource: aid,
						AttackingPlayer: pt(e.controllerOf(aid)), DefendingPlayer: pt(a.Attacking),
					},
				},
			})
			if e.triggerFireCount[key] >= maxTriggerFires {
				return
			}
		}
	}
}

// checkAttackerUnblockedOnceTriggers queues Mode$ AttackerUnblockedOnce
// (Coveted Jewel's "Whenever one or more creatures an opponent controls attack
// you and aren't blocked, that player draws three cards and gains control of
// CARDNAME. Untap it."). It is a dedicated hook, like checkAttackerBlockedTriggers,
// because the ordinary per-face scan cannot express it and the mode has no
// triggerMatches case.
//
// It runs at the DECLARE-BLOCKERS ROUND COMPLETE instant (rules/turn.go's
// StepDeclareBlockers completion branch), NOT per DeclareBlockers event: those
// events are per-defender, and a defender with attackers but no legal blockers
// is skipped with no event at all, so a per-event scan of battlefield-wide
// unblocked attackers would see a later defender's not-yet-blocked attackers
// as unblocked and latch the trigger wrongly early on a split attack. At the
// completion instant every defender has answered (or been skipped), so the
// battlefield's IsAttacking && no-BlockedBy objects are exactly the unblocked
// attackers. The condition is evaluated once, here: an attacker that BECOMES
// unblocked later (its blocker leaves combat, or a stat:AssignCombatDamageAsUnblocked
// election) does not fire this trigger -- Forge checks at the end of declare
// blockers too.
//
// Fire semantics (Forge's AttackerUnblockedOnce): ONE instance per trigger per
// combat when at least one matching unblocked attacker exists -- "one or more
// creatures ... and aren't blocked" -- even when several attackers match. The
// latch (Engine.unblockedOnceFired) stamps (Turn, CombatsThisTurn) so an extra
// combat re-arms it. The matching AttackingPlayer is the first matching
// attacker's controller in the battlefield walk order.
//
// Gates mirror the AttackerBlocked hook's sequence (zone, phase, fire-count
// bound, ActivationLimit$); ValidDefenders$ and ValidAttackingPlayer$ are the
// two player specs this mode carries, both base-Player/You shapes
// effects.MatchesPlayerSpec already evaluates. Secondary$ needs no yield: a
// paired primary can never match a declare-blockers-derived condition.
// OptionalDecider$ is not read -- no corpus carrier of the Once mode carries
// it (the only carrier is Coveted Jewel).
func (e *Engine) checkAttackerUnblockedOnceTriggers() {
	pt := func(p state.PlayerID) state.Target { return state.Target{Player: p, IsPlayer: true} }
	stamp := combatFires{Turn: e.G.Turn, Combat: e.G.CombatsThisTurn}
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		if !o.Unlocked && !e.faceMayTrigger(f, events.DeclareBlockers) {
			return
		}
		for ti, t := range f.Triggers {
			if t.Mode != "AttackerUnblockedOnce" {
				continue
			}
			if t.Effect == nil {
				continue
			}
			if !e.zoneGate(t, id, events.Event{Kind: events.DeclareBlockers}) || !e.phaseGate(t) {
				continue
			}
			key := triggerKey{Source: id, Idx: ti}
			if e.triggerFireCount == nil {
				e.triggerFireCount = map[triggerKey]int32{}
			}
			if e.triggerFireCount[key] >= maxTriggerFires {
				continue // cascade bound: see maxTriggerFires.
			}
			if !e.triggerGameActivationLimitAllows(t, key) {
				continue // GameActivationLimit$: already triggered enough this game.
			}
			if actionTriggerModes[t.Mode] && !e.triggerActivationLimitAllows(t, key) {
				continue
			}
			// The limit gates above are READ-ONLY: a combat with no matching
			// unblocked attacker queues nothing and must not consume a use.
			// The counts commit below, when the instance is actually appended.
			if e.unblockedOnceFired == nil {
				e.unblockedOnceFired = map[triggerKey]combatFires{}
			}
			if e.unblockedOnceFired[key] == stamp {
				continue // already fired this combat.
			}

			// Scan the battlefield for the unblocked attackers this trigger's
			// defender is being attacked by, whose controller is an opponent of
			// the trigger controller. The first match decides the fire; the
			// matching attackers (in battlefield walk order) are remembered.
			defenderSpec := t.Params["ValidDefenders"]
			attackerSpec := t.Params["ValidAttackingPlayer"]
			var attackerIDs []state.ObjID
			for _, p := range e.G.AliveFrom(0) {
				for _, bid := range e.G.Zone(state.ZBattlefield, p) {
					b := e.G.Obj(bid)
					if b == nil || !b.IsAttacking || len(b.BlockedBy) != 0 {
						continue
					}
					if defenderSpec != "" && !effects.MatchesPlayerSpec(e.G, defenderSpec, b.Attacking, o.Controller) {
						continue
					}
					if attackerSpec != "" && !effects.MatchesPlayerSpec(e.G, attackerSpec, e.controllerOf(bid), o.Controller) {
						continue
					}
					attackerIDs = append(attackerIDs, bid)
				}
			}
			if len(attackerIDs) == 0 {
				continue
			}
			firstCtrl := e.controllerOf(attackerIDs[0])

			remembered := make([]state.Target, 0, len(attackerIDs))
			for _, aid := range attackerIDs {
				remembered = append(remembered, state.Target{Obj: aid})
			}
			e.reserveTriggerLimits(t, key)
			e.unblockedOnceFired[key] = stamp
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     id,
				Controller: o.Controller,
				Idx:        ti,
				SA:         t.Effect,
				Ctx: effects.Ctx{
					Source:     id,
					Controller: o.Controller,
					Remembered: remembered,
					Captured:   remembered,
					TriggerContext: effects.TriggerContext{
						TriggerCard:     attackerIDs[0],
						TriggerSource:   attackerIDs[0],
						AttackingPlayer: pt(firstCtrl),
						DefendingPlayer: pt(o.Controller),
					},
				},
			})
		}
	})
}

// blocksCandidates lists the (attacker, blocker) pairs one Forge Mode$ Blocks
// trigger fires for (task trig:Blocks; Savvy Hunter's "Whenever Savvy Hunter
// attacks or blocks", Heat of Battle's "Whenever a creature blocks", Wand of
// Orcus' bearer half). Each declared pair is evaluated per pair -- one
// instance per matching pair, exactly Forge's per-block-event firing --
// because the trigger's matching object is the pair's BLOCKER, not the
// trigger's own source: ValidCard$ is read against the blocker (Card.Self
// names the source-as-blocker; Card.AttachedBy/EquippedBy/EnchantedBy name
// the bearer via the existing attachedBy predicate; the bare Creature spec
// is the global-enchantment shape that fires for a blocker that is NOT the
// source), and ValidBlocked$ is read against the pair's ATTACKER (Goblin
// Cadets' becomes-blocked spelling ValidCard$ Creature | ValidBlocked$
// Card.Self). A blocker appears in exactly one pair per event (CR 509.1a's
// one-blocker-one-attacker pairing; Submit's validateBlockers rejects the
// same ordinary blocker against multiple attackers), so no dedup is needed.
func (e *Engine) blocksCandidates(t cards.Trigger, source state.ObjID, ev events.Event) [][2]state.ObjID {
	if ev.Kind != events.DeclareBlockers || len(ev.Pairs) == 0 {
		return nil
	}
	ctrl := e.controllerOf(source)
	var out [][2]state.ObjID
	for _, pr := range ev.Pairs {
		if v := t.Params["ValidCard"]; v != "" && !e.matchesSpec(v, pr[1], e.specCtx(source, ctrl)) {
			continue
		}
		if v := t.Params["ValidBlocked"]; v != "" && !e.matchesSpec(v, pr[0], e.specCtx(source, ctrl)) {
			continue
		}
		out = append(out, pr)
	}
	return out
}

// checkBlocksTriggers queues trigger instances off a DeclareBlockers event
// for Forge Mode$ Blocks (trig:Blocks): "whenever [this creature] blocks" and
// its enchantment/equipment/global shapes. The ordinary per-face scan cannot
// express it -- it queues at most one entry per trigger per event, and the
// mode's matching object is the pair's BLOCKER while its referents split
// between the blocker and the attacker (Godsend's Blocks half reads
// DefinedCards$ TriggeredAttackers; Wand of Orcus' half pumps
// TriggeredBlockerLKICopy) -- so it rides the same dedicated hook as
// checkAttackerBlockedTriggers, with one instance per matching PAIR. The
// gates mirror the ordinary scan's per-trigger sequence (zone, phase,
// fire-count bound, the actionTriggerModes guard shape kept so a future
// ActivationLimit$/PlayerTurn$ carrier joins with a one-word mode-row
// change -- measured, no Blocks line carries either today) PLUS the shared
// condition gate triggerConditionHoldsAs, which the AttackerBlocked hook
// omits but the corpus's IsPresent$/PresentCompare$ Blocks lines need.
// Each per-instance ctx: the ATTACKER as Remembered/Captured (Godsend's
// TriggeredAttackers pool), the attacker as TriggerCard/TriggerSource, the
// blocker in the new TriggerBlocker role (TriggeredBlockerLKICopy), both
// combat players, and Source/Controller = the trigger face's own
// object/controller (the enchantment/equipment, not the blocker).
// Secondary$ needs no yield here: a Blocks half's paired primary is an
// Attacks trigger, which can never match the same DeclareBlockers event, so
// the secondary always fires on its own (the AttackerBlocked hook skips
// secondaryYields for the same reason).
func (e *Engine) checkBlocksTriggers(ev events.Event) {
	if ev.Kind != events.DeclareBlockers {
		return
	}
	pt := func(p state.PlayerID) state.Target { return state.Target{Player: p, IsPlayer: true} }
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		if !o.Unlocked && !e.faceMayTrigger(f, ev.Kind) {
			return
		}
		for ti, t := range f.Triggers {
			if t.Mode != "Blocks" {
				continue
			}
			if !e.zoneGate(t, id, ev) || !e.phaseGate(t) {
				continue
			}
			if !e.triggerConditionHoldsAs(t, id, o.Controller) {
				continue
			}
			key := triggerKey{Source: id, Idx: ti}
			if e.triggerFireCount == nil {
				e.triggerFireCount = map[triggerKey]int32{}
			}
			if e.triggerFireCount[key] >= maxTriggerFires {
				continue // cascade bound: see maxTriggerFires.
			}
			if !e.triggerGameActivationLimitAllows(t, key) {
				continue // GameActivationLimit$: already triggered enough this game.
			}
			if actionTriggerModes[t.Mode] && !e.triggerActivationLimitAllows(t, key) {
				continue
			}
			// The two limit gates above are READ-ONLY: a DeclareBlockers event
			// whose pairs match nothing queues nothing and must not consume a
			// use of either limit. The counts commit below, at the first pair
			// instance actually appended.
			reserved := false
			for _, pr := range e.blocksCandidates(t, id, ev) {
				if t.Effect == nil {
					break
				}
				attacker := pr[0]
				defender := pt(ev.Player)
				if !reserved {
					reserved = true
					e.reserveTriggerLimits(t, key)
				}
				e.triggerFireCount[key]++
				e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
					Source:     id,
					Controller: o.Controller,
					Idx:        ti,
					SA:         t.Effect,
					Ctx: effects.Ctx{
						Source:     id,
						Controller: o.Controller,
						Remembered: []state.Target{{Obj: attacker}},
						Captured:   []state.Target{{Obj: attacker}},
						TriggerContext: effects.TriggerContext{
							TriggerCard:     attacker,
							TriggerSource:   attacker,
							TriggerBlocker:  pr[1],
							AttackingPlayer: pt(e.controllerOf(attacker)),
							DefendingPlayer: defender,
						},
					},
				})
			}
		}
	})
}

// exertedMatches is the trig:Exerted half of CR 702.100 (task exert1 built the
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
func (e *Engine) exertedMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Exert || ev.Amount < 0 {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	return true
}

func blockedAttackerIn(pairs [][2]state.ObjID, id state.ObjID) bool {
	for _, pr := range pairs {
		if pr[0] == id {
			return true
		}
	}
	return false
}

// damageMatches implements Mode$ DamageDone, DamageDealtOnce, DamageDoneOnce
// and DamageAll (the once-per-damage-batch gate itself lives in
// checkTriggers, alongside the cascade bound; this is purely the per-event
// parameter match, shared by all four modes). DamageAll additionally requires
// ValidSource$ and ValidTarget$ to NAME the same event's source and
// recipient (see the ValidSource$/ValidTarget$ reads below): the per-event
// match is the "both halves match" test, and checkTriggers' all-latch turns
// the first such event in a batch into the single "one or more" instance.
func (e *Engine) damageMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	return e.damageMatchesWithCapture(t, source, ev, nil)
}

func (e *Engine) damageMatchesWithCapture(t cards.Trigger, source state.ObjID, ev events.Event, remembered []state.Target) bool {
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
	switch cd := t.Params["CombatDamage"]; {
	case strings.EqualFold(cd, "True") && !e.combatDamaging:
		return false
	case strings.EqualFold(cd, "False") && e.combatDamaging:
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidSource"]; ok {
		// The damage's source, through the ONE shared dealer resolution
		// (damageEventSource, whose doc carries the full priority rationale:
		// the published override, e.damaging during combat's assignment loop,
		// else the resolving stack object).
		src := e.damageEventSource()
		if src == 0 || !e.matchesSpec(v, src, delayedSpecCtx(e.specCtx(source, ctrl), remembered)) {
			return false
		}
	}
	if v, ok := t.Params["ValidTarget"]; ok {
		if ev.Obj != 0 {
			if !e.matchesSpec(v, ev.Obj, delayedSpecCtx(e.specCtx(source, ctrl), remembered)) {
				return false
			}
		} else if !effects.MatchesPlayerSpecCtx(e.G, v, ev.Player, ctrl, effects.PlayerSpecCtx{
			Source: source, DefendingPlayer: e.damageDefendingPlayer(ev), DelayedRemembered: remembered,
			EffectiveNames: e.renames, DerivedTypes: e.layer4Types,
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
	return true
}

// damageDefendingPlayer binds the combat defending-player role only during
// the combat damage assignment window and only for player recipients. A Damage
// event's recipient alone is not evidence of that role: noncombat damage also
// carries Player, and combat damage may be assigned to a planeswalker.
func (e *Engine) damageDefendingPlayer(ev events.Event) state.Target {
	if !e.combatDamaging || ev.Obj != 0 {
		return state.Target{}
	}
	return state.Target{Player: ev.Player, IsPlayer: true}
}

// damagePreventedMatches implements Mode$ DamagePreventedOnce (task dponce1):
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
func (e *Engine) damagePreventedMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Note || ev.Amount <= 0 {
		return false
	}
	if !strings.Contains(strings.ToLower(ev.Text), "prevent") {
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidTarget"]; ok {
		if ev.Obj != 0 {
			if !e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
				return false
			}
		} else if !effects.MatchesPlayerSpecCtx(e.G, v, ev.Player, ctrl, effects.PlayerSpecCtx{
			Source:          source,
			DefendingPlayer: e.damageDefendingPlayer(ev),
			EffectiveNames:  e.renames,
			DerivedTypes:    e.layer4Types,
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
func (e *Engine) damageSource() state.ObjID {
	if e.dmgSrcOverride != 0 {
		return e.dmgSrcOverride
	}
	if len(e.G.Stack) == 0 {
		return 0
	}
	return e.G.Stack[len(e.G.Stack)-1]
}

// damageEventSource is the ONE dealer resolution for a just-emitted Damage
// event, shared by the ValidSource$ match (damageMatches), the DamageDealtOnce
// latch and the DamageAll batch-set capture, so a captured batch set can never
// name a source the matcher would not have matched. The priority is the
// damageMatches comment's three: an explicit published override
// (rules.Engine.SetDamageSource -- DamageSource$ names the PERMANENT that
// dealt it, never the ability wrapper resolving it) wins over the dealing
// creature during combat's assignment loop (e.damaging -- the stack is
// USUALLY empty during combat but not always: the between-passes priority
// round can leave a first-strike trigger on the stack while the regular pass
// deals), and otherwise the resolving spell or ability while it is the stack
// top (damageSource).
func (e *Engine) damageEventSource() state.ObjID {
	if e.dmgSrcOverride != 0 {
		return e.dmgSrcOverride
	}
	if e.combatDamaging {
		return e.damaging
	}
	return e.damageSource()
}

func init() {
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.attacksMatches(t, source, ev)
	}, "Attacks")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.attackersDeclaredOneTargetMatches(t, source, ev)
	}, "AttackersDeclared", "AttackersDeclaredOneTarget")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.exertedMatches(t, source, ev)
	}, "Exerted")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.damageMatches(t, source, ev)
	}, "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.damagePreventedMatches(t, source, ev)
	}, "DamagePreventedOnce")
}
