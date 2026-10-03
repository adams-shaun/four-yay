package effects

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("ChangeCombatants", effChangeCombatants)
}

// effChangeCombatants implements api:ChangeCombatants's Attacking$ True shape
// (Misleading Signpost, Portal Mage, Windshaper Planetar, and the conjure-and-attack
// Kari Zev, Crew of Two / Stormforged Armor pair): reselect which player each
// defined attacking creature is attacking. Forge's ChangeCombatantsEffect +
// SpellAbilityEffect.addToCombat condensed to what gorge models:
//
//   - only a creature actually attacking, and only in its controller's OWN
//     combat (CR 506.3b — Forge's combat.getAttackingPlayer().equals(
//     c.getController()); gorge combat is always the active player's, so the
//     guard is Controller == g.Active),
//   - the candidate defenders are ALL living players the attacker may be
//     pointed at — every living seat except its controller, the same
//     enumeration askAttackers offers the KAttackers decision (Forge's
//     getAllPossibleDefenders; planeswalker defenders are out of scope —
//     walkers are not attackable in this build),
//   - the resolving controller (Ctx.Controller) picks one per attacker; the
//     answered re-entry emits one CombatRetarget per attacker ONLY when the
//     answer moves the attack — Forge's addToCombat acts only when the
//     chosen defender does not already have this attacker
//     (defender != nil && !combat.getAttackersOf(defender).contains(c)), so
//     answering the attacker's CURRENT defender is a true no-op that keeps
//     the existing block list intact — and the re-pointed attack is
//     unblocked (the event's Apply case clears BlockedBy — Forge's
//     removeFromCombat + addAttacker + setBlocked(false)),
//   - the re-pointing fires NO new trigger and does not touch
//     AttacksThisTurn: it is not a declaration (which is exactly why the new
//     Kind exists instead of reusing DeclareAttackers/TokenAttacks).
//
// The ask is the "choice" KChoose transport (ChooseCard/ChoosePlayer/
// ChangeTargets' shared seam, reuse rather than a new resume arm), with the
// ask's ResumeTarget carrying the asking attacker's index in the
// deterministic Defined$ walk so the re-entry skips attackers already
// answered (effDig's per-target cursor discipline). The R-9 no-host stand-in
// keeps the original defender and records one Note — never a guessed
// defender. An attacker whose only candidate defender is the one it already
// attacks asks nothing: the decision's every answer is the same no-op, so a
// decision nobody could answer differently is never emitted (the
// effDiscard strict-supersets rule).
//
// Out of scope, each LOUD (a Note naming the shape, then no move — the
// registered equivalent of the unimplemented-API fallback these SAs used to
// hit): every other Attacking$ value (Player.OpponentOf CardController
// capricopian, TargetedPlayer
// portal_manipulator — needs parent-target/trigger-referent plumbing that
// does not exist, and portal_manipulator's ask rides a DEEPER sub the
// mvts1 pre-ask never reaches; `.Defending & Valid Planeswalker.Defending`
// tahngarth_first_mate — needs planeswalker defenders, which do not exist).
// Optional$ True (Windshaper Planetar) is read as mandatory with one Note
// naming the stand-in: a per-attacker may-reselect election is not built.
// Forge's re-point of pending trigger roles (OriginalDefender/
// DefendingPlayer on already-queued stack objects) is out of scope too.
func effChangeCombatants(h Host, c *Ctx, sa *cards.SA) {
	mode := strings.TrimSpace(sa.Params["Attacking"])
	if mode == "RememberedPlayer" {
		// Midnight Crusader Shuttle's stolen creature joins the current combat
		// attacking the villainous-choice victim. It was not declared as an
		// attacker; TokenAttacks records that distinction and is replayable.
		// RememberedPlayer names player entries only: Defined()'s spelling
		// "RememberedPlayer" is not an object referent and would lose them.
		var players []state.PlayerID
		for _, t := range c.Remembered {
			if t.IsPlayer && !slices.Contains(players, t.Player) {
				players = append(players, t.Player)
			}
		}
		g := h.Game()
		if len(players) != 1 || g.Step < state.StepDeclareAttackers || g.Step > state.StepEndCombat {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "ChangeCombatants RememberedPlayer has no combat defender"})
			return
		}
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				continue
			}
			if o := g.Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield && o.Controller == g.Active && !o.IsAttacking && players[0] != o.Controller && !g.Players[players[0]].Lost {
				h.Emit(events.Event{Kind: events.TokenAttacks, Obj: o.ID, Player: o.Controller, IDs: []state.ObjID{state.ObjID(players[0])}})
			}
		}
		return
	}
	if mode != "True" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChangeCombatants Attacking$ " + mode + " is not implemented; no combatant changed"})
		return
	}
	// fx42 scoping: capture and clear the answered defender pick BEFORE the
	// attacker loop. The ask's ResumeTarget carries the asking attacker's
	// index in the deterministic Defined$ walk (resumeResolution's ctx
	// rebuild sets Ctx.ChoiceTarget = rp.target for every resume), so
	// earlier attackers completed before suspension and must be skipped,
	// that attacker consumes the answer, and later attackers pose fresh
	// asks of their own.
	answer := c.Choice
	answerDone := c.ChoiceDone
	answerIndex := c.ChoiceTarget
	c.Choice, c.ChoiceDone, c.ChoiceTarget = nil, false, 0
	if !answerDone && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChangeCombatants Optional$ True read as mandatory (no may-reselect ask)"})
	}
	g := h.Game()
	for idx, t := range Defined(h, c, sa) {
		if t.IsPlayer || t.Obj == 0 {
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield || !o.IsAttacking {
			// A non-attacker (or one that left the battlefield, or combat)
			// is a no-op — Forge's addToCombat requires isCreature() and
			// an active combat.
			continue
		}
		if o.Controller != g.Active {
			// CR 506.3b: a creature attacks only in its controller's own
			// combat; gorge combat is always the active player's.
			continue
		}
		if answerDone {
			if idx < answerIndex {
				// This attacker completed on the first pass before a later
				// attacker suspended the effect; re-running it could move a
				// second attack (or newly create a choice after its first
				// answer left). Skip it.
				continue
			}
			if idx == answerIndex {
				// Re-entry: emit the answered reselect exactly once, but only
				// when it actually MOVES the attack — Forge's addToCombat acts
				// only when the chosen defender does not already have this
				// attacker (!combat.getAttackersOf(defender).contains(c)), so
				// answering the current defender is a true no-op that preserves
				// BlockedBy (clearing it would silently unblock an attack whose
				// defender did not change). The answered player was one of the
				// offered candidates; a malformed non-player answer also keeps
				// the original defender.
				if len(answer) > 0 && answer[0].IsPlayer && answer[0].Player != o.Attacking {
					h.Emit(events.Event{Kind: events.CombatRetarget, Obj: o.ID, Player: answer[0].Player})
				}
				// Later attackers keep walking and pose their own asks.
				continue
			}
		}
		// Candidate defenders: every living seat except the attacker's
		// controller, in the deterministic AliveFrom seat walk — the same
		// enumeration askAttackers offers the KAttackers decision.
		var candidates []state.PlayerID
		for _, q := range g.AliveFrom(0) {
			if q != o.Controller {
				candidates = append(candidates, q)
			}
		}
		if len(candidates) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: o.Controller,
				Text: "no legal defender to reselect " + objName(g, o.ID) + " against"})
			continue
		}
		if len(candidates) == 1 && candidates[0] == o.Attacking {
			// The only legal answer keeps the attack where it is: no
			// decision anybody could answer differently is ever emitted.
			continue
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Source: c.Source, Min: 1, Max: 1,
			ResumeKind: "choice", ResumeSA: sa, ResumeTarget: idx,
			Prompt: "Choose which player " + objName(g, o.ID) + " attacks"}
		for _, q := range candidates {
			opt := decision.Option{Index: len(d.Options), Kind: "player", Player: q}
			if int(q) < len(g.Players) {
				opt.Label = g.Players[q].Name
			}
			d.Options = append(d.Options, opt)
		}
		switch Ask(h, d) {
		case AskAsked:
			// Suspended: the answer re-enters this SA through the ordinary
			// "choice" resume arm, lands in Ctx.Choice with Ctx.ChoiceTarget
			// carrying idx, and the re-entrant pass above emits the event.
			return
		case AskNoHost:
			// The R-9 stand-in: keep the original defender and record one
			// Note — never guess a defender.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: o.Controller,
				Text: "no engine host: " + objName(g, o.ID) + " keeps attacking its current defender"})
		}
		// AskEmpty is unreachable when the guards leave len(candidates) >= 2
		// (Min 1 has a non-empty option list); a SINGLETON candidate list
		// different from the current defender (the two guards only exclude
		// empty and current-defender singletons) still posts its one-option
		// ask. If a future caller ever reaches AskEmpty the loop simply keeps
		// the original defender — the same conservative read.
	}
}
