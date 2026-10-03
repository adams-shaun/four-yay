// Enlist (CR 702.160, task enlist1).
//
// `K:Enlist` is a static ability that functions while the creature is
// attacking: "As this creature attacks, you may tap a nonattacking creature
// you control without summoning sickness. When you do, add its power to this
// creature's power until end of turn." Cards/keywords.go deliberately does
// NOT expand it: the keyword's meaning is an election plus a state stamp the
// engine owns, not an ordinary effect body. rules reads `HasKeyword(id,
// "Enlist")` directly and this file owns the whole flow.
//
// The election is posed from handleAttackers (rules/combat.go) BEFORE the
// declaration's DeclareAttackers events are emitted, so an attack trigger
// whose intervening-if reads enlistedThisCombat (Aradesh, the Founder) sees
// the answered stamp when it is matched -- Forge's "as it attacks" action
// happens during the declaration, not after the attack triggers are put on
// the stack.
package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// enlistAsk is the declare-attackers enlist election's resumable state (the
// exertAsk precedent): the answered KAttackers declaration's option list, the
// attacking player, the deterministic offer list (attacking creatures with
// Enlist that have at least one eligible creature to tap, in declaration
// option order) and the cursor of the ask currently outstanding. Plain value,
// so Clone copies it.
type enlistAsk struct {
	chosen  []decision.Option
	player  state.PlayerID
	offers  []state.ObjID
	next    int
	started bool
}

// enlistCandidates returns the creatures an attacker carrying CR 702.160a's
// Enlist may tap, in deterministic battlefield zone order: a creature the
// attacker's controller controls, on the battlefield, untapped, not
// attacking, without summoning sickness, and not the attacker itself. The
// filter goes through the ordinary spec grammar, so a non-creature permanent
// never qualifies and a creature whose type was granted in a layer still
// does.
func (e *Engine) enlistCandidates(attacker state.ObjID) []state.ObjID {
	o := e.G.Obj(attacker)
	if o == nil || o.Face() == nil {
		return nil
	}
	ctrl := o.Controller
	sc := e.specCtx(attacker, ctrl)
	// "nonattacking" during the election window: the DeclareAttackers events
	// -- whose fold sets IsAttacking -- have not run yet (they follow every
	// election), so the declared set is the recorded declaration itself. Two
	// Enlist attackers in one declaration must never enlist each other.
	declared := map[state.ObjID]bool{}
	for _, opt := range e.enlistAskState.chosen {
		declared[opt.Obj] = true
	}
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, ctrl) {
		if id == attacker || declared[id] {
			continue
		}
		c := e.G.Obj(id)
		if c == nil || c.Tapped || c.IsAttacking || c.SummonSick {
			continue
		}
		if !e.matchesSpec("Creature", id, sc) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// enlistOfferList returns the declared attackers (in chosen-option order,
// deduped) that carry Enlist and have at least one eligible creature to tap.
// An attacker with no eligible creature poses no election: there is no
// decision a player could answer differently.
func (e *Engine) enlistOfferList(chosen []decision.Option) []state.ObjID {
	var out []state.ObjID
	seen := make(map[state.ObjID]bool, len(chosen))
	for _, opt := range chosen {
		id := opt.Obj
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if e.hasKeywordH(id, kwhEnlist) && len(e.enlistCandidates(id)) > 0 {
			out = append(out, id)
		}
	}
	return out
}

// startEnlistAsks seeds the enlist election from the answered declaration.
// Returns true when it posed an ask (the caller must then return and let the
// answer resume the declaration); false when no attacker enlists a creature,
// so the caller finishes the declaration inline.
func (e *Engine) startEnlistAsks(chosen []decision.Option, player state.PlayerID) bool {
	// Seed the state BEFORE deriving the offers, so every candidate walk --
	// including the offer gate's own -- already excludes the whole declared
	// set (two Enlist attackers must never enlist each other).
	e.enlistAskState = enlistAsk{chosen: chosen, player: player, started: true}
	offers := e.enlistOfferList(chosen)
	if len(offers) == 0 {
		e.enlistAskState = enlistAsk{}
		return false
	}
	e.enlistAskState.offers = offers
	e.askNextEnlist()
	return true
}

// askNextEnlist poses the enlist election for the next offerable attacker, or
// finishes the declaration once the list is exhausted. The offer gate is
// re-evaluated per ask: earlier answers tap creatures, which can empty a
// later attacker's candidate pool, and the cursor must stay honest against
// that.
func (e *Engine) askNextEnlist() {
	st := e.enlistAskState
	for st.next < len(st.offers) {
		id := st.offers[st.next]
		o := e.G.Obj(id)
		if o == nil {
			st.next++
			continue
		}
		cands := e.enlistCandidates(id)
		if len(cands) == 0 {
			st.next++
			continue
		}
		// CR 702.160a's election is a may: option 0 always declines, so a
		// single candidate is still a real decision (unlike the mandatory
		// batch asks, whose strict-supersets rule suppresses a forced pick).
		d := &decision.Decision{Player: st.player, Kind: decision.KChoose, Min: 0, Max: 1,
			Prompt: fmt.Sprintf("Enlist %s? (Tap a nonattacking creature you control without summoning sickness; %s gets its power until end of turn.)", o.Face().Name, o.Face().Name),
			Source: id}
		d.Options = append(d.Options,
			decision.Option{Index: 0, Kind: "enlist", Label: "Don't enlist for " + o.Face().Name})
		for _, cid := range cands {
			c := e.G.Obj(cid)
			label := "a creature"
			if c != nil && c.Face() != nil {
				label = c.Face().Name
			}
			// Obj is the creature this option would enlist; the attacker is
			// the ask's Source (the offer cursor). The decline option (index 0)
			// carries no Obj, so an answered option with Obj != 0 IS the
			// enlisted creature.
			d.Options = append(d.Options,
				decision.Option{Index: len(d.Options), Kind: "enlist", Label: "Enlist " + label,
					Obj: cid})
		}
		e.enlistAskState = st
		e.choosing = chooseEnlist
		e.ask(d)
		return
	}
	e.enlistAskState = enlistAsk{}
	e.finishAttackers(st.chosen, st.player)
}

// enlistAnswer applies one answered enlist election: the decline (an option
// with no enlisted creature) emits nothing, a yes taps the chosen creature,
// emits the canonical Enlist event (whose fold stamps the per-combat marker
// the enlistedThisCombat predicate and the Mode$ Enlisted trigger read) and
// registers the +power/+0 pump on the attacker. The cursor then advances to
// the next offerable attacker, or the declaration is finished.
func (e *Engine) enlistAnswer(d *decision.Decision, in decision.Intent) {
	st := e.enlistAskState
	e.choosing = chooseNone
	if len(st.offers) == 0 || st.next >= len(st.offers) {
		e.enlistAskState = enlistAsk{}
		return
	}
	attacker := st.offers[st.next]
	chosen := d.Chosen(in)
	var enlisted state.ObjID
	if len(chosen) == 1 {
		enlisted = chosen[0].Obj
	}
	if enlisted != 0 {
		e.applyEnlist(attacker, enlisted)
	}
	st.next++
	e.enlistAskState = st
	e.askNextEnlist()
}

// applyEnlist performs one CR 702.160a enlist action: tap the enlisted
// creature, record the action (which also stamps the attacker's per-combat
// marker and fires Mode$ Enlisted), then register the attacker's +X/+0 pump
// with X the enlisted creature's power AT ENLIST TIME (CR 702.160a binds the
// value as the action happens, so a later change to the enlisted creature's
// power does not move the bonus).
func (e *Engine) applyEnlist(attacker, enlisted state.ObjID) {
	o := e.G.Obj(attacker)
	if o == nil {
		return
	}
	e.emitTap(enlisted, o.Controller, false)
	e.emit(events.Event{Kind: events.Enlist, Obj: attacker,
		IDs: []state.ObjID{enlisted}, Player: o.Controller})
	power := e.Derived(enlisted).Power
	if power == 0 {
		return
	}
	e.AddContinuous(state.ContinuousEffect{
		Source: attacker, Affects: "Card.Self", Controller: o.Controller,
		Layer: state.LPT, Sub: state.SubModify, AddPower: power, UntilEOT: true,
	})
}

// enlistedMatches implements Mode$ Enlisted ("Whenever CARDNAME enlists a
// creature"): the Enlist event's Obj is the ATTACKING creature that enlisted
// (ValidCard$ Card.Self names the trigger's own source), and its IDs[0] the
// creature it tapped, which ValidEnlisted$ filters (Goblin Morale Sergeant's
// Creature.!token).
func (e *Engine) enlistedMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Enlist || len(ev.IDs) == 0 {
		return false
	}
	ctrl := e.controllerOf(source)
	sc := e.specCtx(source, ctrl)
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.matchesSpec(v, ev.Obj, sc) {
		return false
	}
	if v := t.Params["ValidEnlisted"]; v != "" && !e.matchesSpec(v, ev.IDs[0], sc) {
		return false
	}
	return true
}

func init() {
	// enlist1: the `K:Enlist` keyword (CR 702.160) is engine-owned -- the
	// election, the Enlist event and the +X/+0 pump live in this file, and
	// `T:Mode$ Enlisted` is the listener trigger enlistedMatches serves.
	// Registered as non-API primitives so the coverage census reads the
	// carriers as playable.
	effects.RegisterNonAPI("kw:Enlist", "trig:Enlisted")
	registerTrigMatcher((*Engine).enlistedMatches, "Enlisted")
}
