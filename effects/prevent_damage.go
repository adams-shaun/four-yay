package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("PreventDamage", effPreventDamage)
}

// shieldBody is the replacement body every shield this primitive registers
// carries: rules' damage-replacement dispatcher (rules/replacement.go's
// applyReplaceDamageBody) resolves it through the registered pool binding,
// which the same application path depletes after each partial prevention.
// Count$ChosenNumber is the head that reads ContinuousEffect.ChosenNumber —
// the pool rules decrement — so the match gate and the application always
// see the SAME remaining amount and a spent shield prevents nothing.
const shieldBody = "DB$ ReplaceDamage | Amount$ Count$ChosenNumber"

// effPreventDamage implements "SP$/AB$/DB$ PreventDamage" (Forge's
// PreventDamageEffect, CR 615): "prevent the next N damage that would be
// dealt to <target> this turn" — the one-shot rules-generated shield the
// printed prevention REPLACEMENT machinery (R:DamageDone Prevent$ True,
// DB$ ReplaceDamage bodies, stat:CantPreventDamage) cannot create, because a
// replacement line can only modify the damage event that is already
// happening. This primitive installs a real shield: one continuous
// DamageDone replacement per chosen recipient, scoped by its own captured
// recipient (rules' effect-created matcher, never the filter grammar — see
// the marker below), lasting until end of turn and depleting across events.
//
// The registered shape is exactly the one rules' dispatcher already resolves:
// ReplacementEvent "DamageDone" with a DB$ ReplaceDamage body whose Amount$
// reads Count$ChosenNumber, and ContinuousEffect.ChosenNumber carrying the
// per-recipient pool. The same head the SetChosenNumber$ binding machinery
// threads (rules' seedEffectReplCtx) therefore serves as the pool; rules'
// applyReplaceDamageBody decrements it after every application and drops the
// shield when it is spent, so "the next N" is a total across events, not a
// per-event allowance (CR 615's shield semantics; the Divine Deflection
// family's unworn DivideShield$ depletion is this primitive's own, not a
// retroactive change to that registration path).
//
// Scoping convention: every shield's ReplacementParams carry
// PreventionShield= "True" and rules' damageReplacementMatches scopes such a
// match by the registration's own Remembered (objects) /
// RememberedPlayers (players) lists — membership, not a ValidTarget$ filter
// spec. The filter grammar is deliberately not used: its IsRemembered
// predicate UNIONs the source's event-backed remembered list with the
// registration's capture, which would let unrelated remembered state widen
// the promise. The marker key is registration-internal (no corpus R: line
// carries it); a printed line is unaffected because the marker only reaches
// the matcher through the effect-created path's params copy.
//
// DividedAsYouChoose$ N divides the NAMED TOTAL among the chosen recipients —
// the same deterministic round-robin stand-in the DealDamage and PutCounter
// divisions ship (effects/damage.go, effects/counters.go): one point at a
// time in answer order, so earlier-chosen recipients take the extras and the
// total is exactly N. A real per-recipient allocation ask is the division
// ask's recorded stand-in everywhere else too.
//
// Unread, loud (each emits one Note naming the parameter and installs no
// shield): the random-recipient shape (whimsy's Random$ True |
// CardChoices$/PlayerChoices$ — this engine forbids ambient randomness), and
// a PreventionSubAbility$ whose ShieldEffectTarget$ is not the corpus's
// ParentTarget. A resolvable-but-zero Amount$ installs nothing (a shield
// that prevents nothing is no decision); an amount the numeric grammar
// cannot resolve is loud rather than silent.
func effPreventDamage(h Host, c *Ctx, sa *cards.SA) {
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRandom)), "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unimplemented PreventDamage random-recipient shape"})
		return
	}
	total := int32(0)
	if _, present := sa.Param(cards.PKAmount); present {
		n, ok := NumResolved(h, c, sa, "Amount", 0)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unresolvable PreventDamage Amount$ " + sa.ParamStr(cards.PKAmount)})
			return
		}
		total = n
	}
	if total <= 0 {
		return
	}
	// The rider binding (Acolyte's Reward, Vengeful Archon): the registered
	// shield deals what it prevents, to the PARENT SA's targets
	// (ShieldEffectTarget$ ParentTarget — the redirect target the enclosing
	// Pump chose), once per application, when the prevention happens.
	rider := ""
	if rn := strings.TrimSpace(sa.Params["PreventionSubAbility"]); rn != "" {
		if !strings.EqualFold(strings.TrimSpace(sa.Params["ShieldEffectTarget"]), "ParentTarget") {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unimplemented PreventDamage ShieldEffectTarget$ " + sa.Params["ShieldEffectTarget"]})
		} else {
			rider = rn
		}
	}
	var riderObjs []state.ObjID
	var riderPlayers []state.PlayerID
	if rider != "" {
		// c.Targets at this sub's dispatch are the PARENT SA's chosen targets
		// (resolveAbility binds them to the whole chain); the shield's own
		// recipients arrive through the generic pre-ask's PickedTargets — the
		// two lists are deliberately distinct.
		for _, t := range c.Targets {
			if t.IsPlayer {
				riderPlayers = append(riderPlayers, t.Player)
			} else {
				riderObjs = append(riderObjs, t.Obj)
			}
		}
	}
	// Eligible recipients, in Defined/answer order: a player still in the
	// game, or an object on the battlefield (the DealDamage discipline — a
	// vanished recipient takes no shield and its share is not redistributed).
	type recipient struct {
		player   state.PlayerID
		obj      state.ObjID
		isPlayer bool
		share    int32
	}
	var recs []recipient
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			p := t.Player
			if int(p) < 0 || int(p) >= len(h.Game().Players) || h.Game().Players[p].Lost {
				continue
			}
			recs = append(recs, recipient{player: p, isPlayer: true})
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		recs = append(recs, recipient{obj: t.Obj})
	}
	if len(recs) == 0 {
		return
	}
	// DividedAsYouChoose$ N: the total is divided round-robin over the
	// recipients in answer order (the DealDamage/PutCounter stand-in). The
	// undivided shape gives every recipient the full Amount$.
	if div, ok := dividedParam(sa); ok {
		divTotal := numText(h, c, div, 0)
		if divTotal < 0 {
			divTotal = 0
		}
		for i := int32(0); i < divTotal; i++ {
			recs[i%int32(len(recs))].share++
		}
	} else {
		for i := range recs {
			recs[i].share = total
		}
	}
	for _, rec := range recs {
		if rec.share <= 0 {
			continue
		}
		ce := state.ContinuousEffect{
			Source: c.Source, Controller: c.Controller,
			UntilEOT: true, Duration: "UntilEOT",
			ReplacementEvent: "DamageDone",
			ReplacementParams: map[string]string{
				"PreventionShield":     "True",
				"PreventionSubAbility": rider,
			},
			ReplacementBody: shieldBody,
			ChosenNumber:    rec.share,
			ShieldTargets:   riderObjs,
			// Engine-runtime slices must never alias a Ctx's backing array
			// (clone.go deep-copies the params map for the same reason).
			ShieldTargetPlayers: append([]state.PlayerID(nil), riderPlayers...),
		}
		if rec.isPlayer {
			ce.RememberedPlayers = []state.PlayerID{rec.player}
		} else {
			ce.Remembered = []state.ObjID{rec.obj}
		}
		h.AddContinuous(ce)
	}
}
