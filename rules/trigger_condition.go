// The CR 603.4 intervening-if gate, shared by every trigger mode.
//
// triggerMatches applies this to every mode after its matcher, so the same T:
// line grammar -- a LifeAmount$ or IsPresent$/PresentCompare$ clause -- is
// honoured wherever it appears.
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
	"github.com/adams-shaun/gorge/state"
)

// noResolvingCheck reports whether the trigger carries NoResolvingCheck$ True:
// Forge's marker that its condition clause (an intervening-if, an
// IsPresent$/PresentCompare$ or a CheckSVar$/SVarCompare$ gate) is checked
// ONLY when the trigger would fire, never again as it resolves. Measured over
// the corpus: 88 raw T: lines across 87 files, every one True -- transient
// fire-time battlefield counts (pack tactics' attacking power, battalion's
// other attackers, Valakut's Mountains, Vengevine's second creature spell,
// Love on the Battlefield's exactly-two) whose re-evaluation at resolution
// would wrongly fizzle the ability after the state moved. The read sits in
// the ONE resolution-time recheck site (resolveTop's CR 603.4 half, via
// triggerResolvingCheckHolds); every fire-time site (triggerMatches,
// the event-matched delayed walker, the AttackerBlocked hook) keeps its own
// check.
func noResolvingCheck(t cards.Trigger) bool {
	return strings.EqualFold(strings.TrimSpace(t.Params["NoResolvingCheck"]), "True")
}

// triggerResolvingCheckHolds is the resolution-time half of the CR 603.4
// condition check, beside triggerMatches' fire-time half: a triggered
// ability's condition is checked when it triggers AND as it resolves, and a
// false answer at resolution removes the ability from the stack (CR 603.4).
// NoResolvingCheck$ True opts the trigger out of this second check.
//
// tc is the ability's captured event roles (Engine.triggerContexts[id] at the
// resolveTop call site), which is what lets the event-RELATIVE clauses below
// be re-evaluated here: an intervening-if that names the attacked player has
// no event at resolution time, but the role was captured when the trigger
// queued and travels with the ability (effects.TriggerContext.DefendingPlayer).
// A nil tc leaves those clauses to their fire-time matcher, so this function
// is safe to call with no context for a trigger whose mode evaluates them
// itself (the fire-time general gate passes nil for exactly that reason).
func (e *Engine) triggerResolvingCheckHolds(t cards.Trigger, source state.ObjID, controller state.PlayerID, tc *effects.TriggerContext, ownedSVars map[string]string) bool {
	if noResolvingCheck(t) {
		return true
	}
	// The ability's controller was fixed when it went on the stack. A delayed
	// Effect can have another owner than its source card; an ordinary trigger's
	// source can also change control while the ability waits to resolve.
	return e.triggerConditionHoldsWithSVars(t, source, controller, tc, ownedSVars)
}

// triggerConditionHolds evaluates the CR 603.4 intervening-if clause (and the
// CR 603.8 state-trigger condition) carried on a T: line. Two clause shapes
// are recognised, the two the corpus uses on the trigger lines this engine
// routes through the modes above:
//
//   - LifeAmount$ <op><n> against LifeTotal$ (<who>): the named player's life
//     total compared to n.
//   - IsPresent$ <spec> with PresentCompare$ <op><n>: the count of objects
//     matching <spec> compared to n.
//   - Metalcraft$ True (and the bare-Condition$ Metalcraft spelling): the
//     controller controls three or more artifacts -- the trigger-side named
//     condition the ability word uses (6 corpus files: Vedalken Humiliator's
//     attack pump, Blade-Tribe Berserkers' and Bleak Coven Vampires' ETBs,
//     Lumengrid Drake's bounce, Inventors' Fair's upkeep lifegain, Screeching
//     Silcaw's mill), read through the SAME metalcraftHolds census
//     costConditionHolds' Condition$ Metalcraft case and the Continuous
//     static gate read, so the three cannot drift apart.
//
// An absent clause is vacuously true. A clause whose shape this build cannot
// evaluate FAILS CLOSED -- a false condition means the trigger simply does not
// fire, never that an unreadable life/creature count is presumed large
// enough to let a win or counter trigger slip through.
func (e *Engine) triggerConditionHolds(t cards.Trigger, source state.ObjID) bool {
	return e.triggerConditionHoldsCtx(t, source, e.controllerOf(source), nil)
}

// triggerConditionHoldsAs is triggerConditionHolds with "you" supplied
// explicitly rather than derived from source's controller. An event-matched
// delayed trigger's "you" is the registration's effect owner (dt.Controller),
// which can differ from the source card's own controller -- see
// checkEventDelayedTriggers.
func (e *Engine) triggerConditionHoldsAs(t cards.Trigger, source state.ObjID, you state.PlayerID) bool {
	return e.triggerConditionHoldsCtx(t, source, you, nil)
}

// triggerConditionHoldsCtx is the shared condition walk with the ability's
// captured event roles supplied (nil at fire time, since the general gate has
// no event to name a role and the per-mode matcher -- attacksMatches for
// Condition$ AttackedPlayerWithMostLife -- owns the fire-time answer).
func (e *Engine) triggerConditionHoldsCtx(t cards.Trigger, source state.ObjID, you state.PlayerID, tc *effects.TriggerContext) bool {
	return e.triggerConditionHoldsWithSVars(t, source, you, tc, nil)
}

// triggerConditionHoldsAsWithDelayedRemembered is triggerConditionHoldsAs with
// a delayed registration's OWN captured set (state.DelayedTrigger.Remembered)
// bound as the IsTriggerRemembered referent. An event-matched delayed
// registration whose inline trigger body carries an IsPresent$ clause named
// after the capture (Stolen Uniform's "if it's attached to a creature you
// control") must evaluate that clause against the REGISTRATION's remembered
// objects, not against an unbound context: the generic walk otherwise fails
// closed, because effects' Card.IsTriggerRemembered predicate needs
// SpecContext.DelayedRemembered bound to answer at all. The binding rides the
// caller's TriggerContext, so the clause keeps ONE evaluation site with
// unchanged semantics for every other caller.
func (e *Engine) triggerConditionHoldsAsWithDelayedRemembered(t cards.Trigger, source state.ObjID, you state.PlayerID, remembered []state.Target) bool {
	tc := effects.TriggerContext{DelayedRemembered: remembered}
	return e.triggerConditionHoldsWithSVars(t, source, you, &tc, nil)
}

// The resolution-time caller passes the recorded line owner's table; at
// fire time a printed trigger's own face is the owner so ownedSVars stays nil,
// while the granted-trigger paths pass the GRANTOR's table through
// triggerMatchesWithSVars (source is the affected recipient, whose same-named
// SVar must not answer the condition).
func (e *Engine) triggerConditionHoldsWithSVars(t cards.Trigger, source state.ObjID, you state.PlayerID, tc *effects.TriggerContext, ownedSVars map[string]string) bool {
	// A kw:Class level band is an independent AND gate beside every clause
	// below (and beside the body's own IsPresent$/IsPresent2$ clauses).
	if !e.classBandGateHolds(t.Params, source) {
		return false
	}
	// LifeLost's and LifeGained's LifeAmount$ are matched against the causing
	// loss/gain by their matchers (lifeLostMatches/lifeGainedMatches), rather
	// than against a player's current life total.
	if v, ok := t.Params["LifeAmount"]; ok && t.Mode != "LifeLost" && t.Mode != "LifeLostAll" && t.Mode != "LifeGained" {
		if !e.lifeConditionHoldsAs(t, you, v) {
			return false
		}
	}
	// IsPresent$ and IsPresent2$ are two INDEPENDENT present clauses that
	// must BOTH hold (Forge's meetsCommonRequirements reads each with its own
	// PresentCompare/PresentZone/PresentDefined suffix set): Hidden
	// Predators' "an opponent controls a creature with power 4 or greater,
	// if CARDNAME is an enchantment", the meld pairs' "you control X and a
	// creature named Y", "Name Sticker" Goblin's "if it's on the battlefield
	// and you control 9 or fewer ...". The earlier UNION reading made Hidden
	// Predators' Mode$ Always trigger fire while it was merely an
	// enchantment and, once animated, re-fire forever on the opponent's
	// creature alone, stacking one Permanent Animate effect per resolution
	// (cardfuzz batch3 lines 4/21: 500+ continuous effects, every Derived
	// walking all of them -- the "hang").
	if _, ok := t.Params["IsPresent"]; ok &&
		!e.presentClauseHolds(t, source, you, tc, "IsPresent", "PresentCompare", "PresentDefined", "PresentZone") {
		return false
	}
	if _, ok := t.Params["IsPresent2"]; ok &&
		!e.presentClauseHolds(t, source, you, tc, "IsPresent2", "PresentCompare2", "PresentDefined2", "PresentZone2") {
		return false
	}
	if v, ok := t.Params["Metalcraft"]; ok {
		// The trigger-side named condition (task trig-attacks-metalcraft):
		// a value this build cannot read as True is an unreadable clause
		// shape and fails closed like the other clauses above.
		if !strings.EqualFold(strings.TrimSpace(v), "True") || !e.metalcraftHolds(you) {
			return false
		}
	}
	if strings.EqualFold(strings.TrimSpace(t.Params["Condition"]), "Metalcraft") {
		// The bare-Condition$ spelling of the same gate. The trigger path
		// reads no OTHER bare Condition$ value (LifePaid, Evolve,
		// Sacrificed and friends are matched by their own per-kind helpers
		// or stay unread), and no corpus trigger carries this spelling
		// today -- the bare Condition$ Metalcraft carriers are S: statics
		// the Continuous gate already reads -- but the spelling is kept
		// beside Metalcraft$ so the two cannot drift apart.
		if !e.metalcraftHolds(you) {
			return false
		}
	}
	if v, ok := t.Params["Blessing"]; ok {
		// Blessing$ True is the trigger-side city's-blessing gate (CR
		// 702.131). It is evaluated for the trigger's "you" player, including
		// delayed triggers whose controller is supplied explicitly. A malformed
		// value fails closed, as do all other unreadable trigger conditions.
		if !strings.EqualFold(strings.TrimSpace(v), "True") || int(you) >= len(e.G.Players) || e.G.Players[you].Lost || !e.G.Players[you].Blessing {
			return false
		}
	}
	if v, ok := t.Params["Revolt"]; ok {
		// Revolt$ (the CR 702.38 ability word, "if a permanent you
		// controlled left the battlefield this turn"): the SAME
		// revoltThisTurn scan the replacement path's Revolt$ clause reads
		// (rules/replacement.go), the one Engine.RevoltHolds -- effects'
		// bare Condition$ Revolt gate and Count$Revolt branch head --
		// delegates to, so the three spellings cannot drift apart. Measured
		// over the corpus: 29 files carry Revolt$ True; 24 of its lines are
		// triggers -- 16 Mode$ Phase end-step shapes (Aid from the Cowl,
		// Hidden Stockpile, Krang) and 8 Mode$ ChangesZone ETBs (Airdrop
		// Aeronauts, Vengeful Rebel, Deadeye Harpooner); the rest are a
		// replacement line (Aether Revolt, already read on the replacement
		// path) and non-trigger text. A value this build cannot read as
		// True is an unreadable clause shape and fails closed like the
		// Metalcraft$ clause above.
		if !strings.EqualFold(strings.TrimSpace(v), "True") || !e.revoltThisTurn(you) {
			return false
		}
	}
	if strings.EqualFold(strings.TrimSpace(t.Params["Condition"]), "Revolt") {
		// The bare-Condition$ spelling of the same gate. No corpus trigger
		// carries it today (the one bare Condition$ Revolt carrier,
		// Decommission, is a DB$ GainLife sub the effects condition gate
		// reads), kept beside the bare Condition$ Metalcraft spelling so
		// the two cannot drift apart.
		if !e.revoltThisTurn(you) {
			return false
		}
	}
	if v, ok := t.Params["Delirium"]; ok {
		// Delirium$ True (the CR 207.2c ability word, "four or more card
		// types among cards in your graveyard"): the trigger-side gate,
		// read through the SAME graveyardCardTypeCount census the
		// replacement path's Delirium$ clause (rules/replacement.go), the
		// Continuous static gate (rules/layers.go), the ability-offer gate
		// (rules/legal.go's activationConditionOK) and the effects bare
		// Condition$ Delirium gate (Host.DeliriumHolds) read, so every
		// spelling answers identically and the two directions cannot
		// disagree. Measured over the corpus at this pin: 22 raw T: lines
		// across 21 files (Winter, Cynical Opportunist; Ishkanah,
		// Grafwidow; Demolisher Spawn; Wickerfolk Thresher and the rest of
		// the Death Toll family) -- before this clause the trigger fired
		// UNCONDITIONALLY, the over-fire direction. A value this build
		// cannot read as True is an unreadable clause shape and fails
		// closed like the Metalcraft$ and Revolt$ clauses above.
		if !strings.EqualFold(strings.TrimSpace(v), "True") || e.graveyardCardTypeCount(you) < 4 {
			return false
		}
	}
	if strings.EqualFold(strings.TrimSpace(t.Params["Condition"]), "AttackedPlayerWithMostLife") {
		// Scourge of the Throne's intervening-if ("if it's attacking the
		// player with the most life or tied for most life"): an
		// event-RELATIVE clause, so it is re-evaluated here only when the
		// ability's captured roles name the defender. A nil tc is the
		// fire-time general gate, whose caller attacksMatches already
		// evaluated the clause against the DeclareAttackers event against
		// the SAME playerHasMostLife read -- skipping it here is what keeps
		// the two halves from double-gating a passing trigger. A non-nil tc
		// (resolution) with no defender binding is a condition this build
		// cannot confirm and fails closed, the convention every clause above
		// documents (an unreadable condition must never let a trigger slip
		// through).
		if tc != nil {
			if !tc.DefendingPlayer.IsPlayer || !e.playerHasMostLife(tc.DefendingPlayer.Player) {
				return false
			}
		}
	}
	if name, ok := t.Params["CheckSVar"]; ok {
		// CheckSVar$/SVarCompare$ (Kozilek, the Great Distortion's cast
		// trigger: "if you have fewer than seven cards in hand"): the
		// CR 603.4 intervening-if the shared SVar-compare evaluator reads,
		// evaluated with the source face's SVar table and the trigger's
		// "you" -- the same wrapper rules' static gate uses. A condition
		// this build cannot evaluate fails closed (the trigger does not
		// fire), the same convention triggerConditionHolds' other clauses
		// document above.
		src := e.G.Obj(source)
		if (src == nil || src.Face() == nil) && ownedSVars == nil {
			return false
		}
		// A mutated pile's under-card trigger (CR 702.140d) is gated by the
		// UNDER-CARD's own SVar table: Face() on a pile is always its top
		// card, and the top card of a mutate pile can be any creature, so
		// reading its table would evaluate the gate against a body it never
		// defined (failing closed, i.e. silently never firing). An ordinary
		// trigger's owning face IS the top face, so nothing else moves.
		svars := ownedSVars
		if svars == nil {
			svars = src.Face().SVars
			if mf := e.faceOwningTrigger(source, t); mf != nil {
				svars = mf.SVars
			}
		}
		ctx := &effects.Ctx{Source: source, Controller: you, SVars: svars}
		holds, evaluated := effects.CheckSVarHolds(e, ctx, name, strings.TrimSpace(t.Params["SVarCompare"]))
		if !evaluated || !holds {
			return false
		}
	}
	if spec, ok := t.Params["CheckDefinedPlayer"]; ok {
		holds, supported := e.checkDefinedPlayerHolds(spec, you)
		// A supported predicate is evaluated for every mode. An unsupported
		// one fails closed only for actionTriggerModes; other modes keep
		// firing as they did before the predicate was read at all.
		if (supported && !holds) || (!supported && actionTriggerModes[t.Mode]) {
			return false
		}
	}
	return true
}

// checkDefinedPlayerHolds evaluates the player-state predicate class used by
// event-trigger conditions. isMonarch, with the controller, opponent and any-
// player selectors, is the supported form; supported is false for any other
// predicate (hasInitiative, withMost*, committedCrimeThisTurn, ...).
func (e *Engine) checkDefinedPlayerHolds(spec string, you state.PlayerID) (holds, supported bool) {
	base, property, ok := strings.Cut(strings.TrimSpace(spec), ".")
	if !ok || property != "isMonarch" {
		return false, false
	}
	switch base {
	case "You":
		return e.G.IsMonarch(you), true
	case "Opponent", "Other":
		for _, p := range e.G.AliveFrom(you) {
			if p != you && e.G.IsMonarch(p) {
				return true, true
			}
		}
		return false, true
	case "Player", "Any":
		for _, p := range e.G.AliveFrom(0) {
			if e.G.IsMonarch(p) {
				return true, true
			}
		}
		return false, true
	}
	return false, false
}

// lifeConditionHoldsAs evaluates the LifeTotal$/LifeAmount$ intervening-if.
// The "you" for a You-qualified LifeTotal$ is the caller's chosen player --
// normally the source's controller, but an event-matched delayed trigger
// passes its registration's effect owner instead (see
// checkEventDelayedTriggers).
func (e *Engine) lifeConditionHoldsAs(t cards.Trigger, you state.PlayerID, amount string) bool {
	who := you
	if v, ok := t.Params["LifeTotal"]; ok {
		v = strings.TrimSpace(v)
		switch v {
		case "", "You":
			// the controller, which who already is
		case "ActivePlayer":
			who = e.G.Active
		default:
			return false // unevaluable player selector: fail closed
		}
	}
	if int(who) >= len(e.G.Players) || e.G.Players[who].Lost {
		return false
	}
	return compareLife(e.G.Players[who].Life, amount)
}

// presentClauseHolds evaluates one present clause -- IsPresent$ or
// IsPresent2$, each with its own suffixed parameter set named by the key
// arguments: the compare (absent = "at least one match", Forge's reading of a
// bare present clause -- Mana Vault's draw-step damage), the defined set and
// the zone. Counts are taken relative to the trigger's source and the
// caller's chosen "you".
//
//   - PresentDefined$ Self (Mana Vault's "if this artifact is tapped")
//     counts the source object alone, and only while it is on the
//     battlefield; any other defined set fails closed.
//   - PresentZone$ (Jocasta, Automaton Avenger's "if this card is in your
//     graveyard") counts the named zone in every living seat's copy of it,
//     in deterministic seat/zone order; an unknown zone word fails closed.
//   - otherwise the battlefield is counted.
//
// The caller's TriggerContext, when it names a delayed registration's capture
// through DelayedRemembered, is bound into the SpecContext the spec matches
// resolve against, so a capture-aware present spec (Card.IsTriggerRemembered
// +...) counts against the REGISTRATION's own remembered set -- the same
// binding delayedPresentGateHolds gives the Phase arm. A nil tc binds
// nothing, exactly the pre-existing read.
//
// A comparison this build cannot fold (PresentCompare$ EQX) fails closed.
func (e *Engine) presentClauseHolds(t cards.Trigger, source state.ObjID, you state.PlayerID, tc *effects.TriggerContext, isKey, cmpKey, definedKey, zoneKey string) bool {
	spec := strings.TrimSpace(t.Params[isKey])
	cmp, hasCmp := t.Params[cmpKey]
	if !hasCmp {
		cmp = "GE1"
	}
	cmp = e.presentCompareFor(cmp, source, you)
	sc := e.specCtx(source, you)
	if tc != nil {
		sc.DelayedRemembered = tc.DelayedRemembered
	}
	if pd := strings.TrimSpace(t.Params[definedKey]); pd != "" {
		if pd != "Self" {
			return false
		}
		if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield ||
			!e.matchesSpec(spec, source, sc) {
			return comparePresent(0, cmp)
		}
		return comparePresent(1, cmp)
	}
	if pz := strings.TrimSpace(t.Params[zoneKey]); pz != "" {
		zone, known := effects.ParseZoneWord(pz)
		if !known {
			return false
		}
		return comparePresent(e.presentZoneCountCtx(zone, spec, source, you, sc), cmp)
	}
	return comparePresent(e.countPresentCtx(spec, source, you, sc), cmp)
}

// presentZoneCount counts spec matches over one zone across every living
// seat, the deterministic walk countPresent makes over the battlefield.
func (e *Engine) presentZoneCount(zone state.Zone, spec string, source state.ObjID, you state.PlayerID) int {
	return e.presentZoneCountCtx(zone, spec, source, you, e.specCtx(source, you))
}

// presentZoneCountCtx is presentZoneCount with the caller's own SpecContext,
// so a capture-aware spec resolves against a binding the generic constructor
// does not carry (see presentClauseHolds).
func (e *Engine) presentZoneCountCtx(zone state.Zone, spec string, source state.ObjID, you state.PlayerID, sc effects.SpecContext) int {
	n := 0
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(zone, p) {
			if e.matchesSpec(spec, id, sc) {
				n++
			}
		}
	}
	return n
}

// countPresent walks every object on the battlefield once and counts those
// matching spec, excluding the source where the spec's own Other/StrictlyOther
// predicate already handles it (Emperor Crocodile's Creature.Other+YouCtrl).
func (e *Engine) countPresent(spec string, source state.ObjID, you state.PlayerID) int {
	return e.countPresentCtx(spec, source, you, e.specCtx(source, you))
}

// countPresentCtx is countPresent with the caller's own SpecContext, so a
// capture-aware spec resolves against a binding the generic constructor does
// not carry (see presentClauseHolds).
func (e *Engine) countPresentCtx(spec string, source state.ObjID, you state.PlayerID, sc effects.SpecContext) int {
	n := 0
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			return
		}
		if e.matchesSpec(spec, id, sc) {
			n++
		}
	})
	return n
}

// compareLife compares a life total against a Forge comparison literal such as
// GE40, EQ0, LE3. Any shape this build cannot fold (a non-numeric rhs, a
// missing operator) is false, so an unreadable condition never fires a
// trigger.
func compareLife(have int32, cmp string) bool {
	op, n, ok := splitCompare(strings.TrimSpace(cmp))
	if !ok {
		return false
	}
	return applyCompare(int(have), op, n)
}

// presentCompareFor folds a PresentCompare$ whose right-hand side names an
// SVar (Zealots en-Dal's and Mob Mentality's EQX, X a Count$Valid over the
// same set -- "if ALL nonland permanents you control are white") into the
// numeric literal comparePresent reads: the SVar is looked up on the
// source's own face and evaluated with effects.EvalCountOK from you's
// perspective. A numeric rhs, an unknown name or an unresolvable body is
// returned unchanged, so comparePresent keeps failing it closed.
func (e *Engine) presentCompareFor(cmp string, source state.ObjID, you state.PlayerID) string {
	cmp = strings.TrimSpace(cmp)
	if len(cmp) < 3 {
		return cmp
	}
	rhs := cmp[2:]
	if _, err := strconv.Atoi(rhs); err == nil {
		return cmp
	}
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return cmp
	}
	body, ok := o.Face().SVars[rhs]
	if !ok {
		return cmp
	}
	v, ok := effects.EvalCountOK(e, &effects.Ctx{Source: source, Controller: you, SVars: o.Face().SVars}, body)
	if !ok {
		return cmp
	}
	return cmp[:2] + strconv.Itoa(int(v))
}

// comparePresent compares a present-count against the same comparison literal
// grammar. A non-numeric rhs (PresentCompare$ EQX) fails closed; callers
// that hold a source fold an SVar rhs first (presentCompareFor).
func comparePresent(have int, cmp string) bool {
	op, n, ok := splitCompare(strings.TrimSpace(cmp))
	if !ok {
		return false
	}
	return applyCompare(have, op, n)
}

// splitCompare separates a Forge comparison literal ("GE40", "EQ0") into its
// two-character operator and its numeric rhs. ok is false for anything that is
// not a recognised operator followed by an integer.
func splitCompare(cmp string) (op string, n int, ok bool) {
	if len(cmp) < 3 {
		return "", 0, false
	}
	op = cmp[:2]
	num, err := strconv.Atoi(cmp[2:])
	if err != nil {
		return "", 0, false
	}
	return op, num, true
}

func applyCompare(have int, op string, n int) bool {
	switch op {
	case "GE":
		return have >= n
	case "LE":
		return have <= n
	case "EQ":
		return have == n
	case "GT":
		return have > n
	case "LT":
		return have < n
	case "NE":
		return have != n
	}
	return false
}

// stateTriggerOutstanding reports whether a state trigger (Mode$ Always)
// already has an instance where one would be enqueued -- either still in the
// pending queue or already placed on the stack. This is the CR 603.8 latch:
// it is what stops a continuously-true condition from enqueuing an unbounded
// run of the same trigger.
func (e *Engine) stateTriggerOutstanding(source state.ObjID, idx int) bool {
	for _, pt := range e.pendingTriggers {
		if pt.Source == source && pt.Idx == idx {
			return true
		}
	}
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	f := o.Face()
	if f == nil || idx < 0 || idx >= len(f.Triggers) {
		return false
	}
	sa := f.Triggers[idx].Effect
	for _, sid := range e.G.Stack {
		so := e.G.Obj(sid)
		if so != nil && so.Source == source && so.Ability == sa {
			return true
		}
	}
	return false
}
