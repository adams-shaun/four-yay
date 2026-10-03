package rules

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A token created onto the battlefield ENTERS the battlefield (CR 111.1,
// 603.6a), so the "enters" replacements of OTHER permanents apply to it
// exactly as to a card that moves there: Authority of the Consuls'
// "Creatures your opponents control enter tapped", Kismet, Thalia, Heretic
// Cathar, Grumgully's "enters with an additional +1/+1 counter". Those are
// R:Event$ Moved | Destination$ Battlefield lines (and the
// K:ETBReplacement:Other:<body>:...:<ValidCard> keyword that expands to
// one), matched only against a MoveZone. A scripted mint (TokenCreate) and
// Encore's copy (CardToken) are created straight onto the battlefield by
// their own Apply fold with no MoveZone, so the replacement never saw them:
// the opponent's Goblin tokens entered untapped under Authority.
//
// applyTokenEntryUpdates runs, for one just-minted token, the Updated ("the
// event still happens, augmented") Moved-to-battlefield replacements of
// other sources -- the composeUpdatedReplacements shape a card's entry takes:
// the entry has folded, each body resolves in deterministic scan order, and
// the entry's own triggers are matched after (Engine.emit checks them after
// this hook), so an ETB trigger sees the token tapped or countered. The
// match reads a synthetic, never-logged MoveZone whose origin is ZCeased --
// the token comes from outside the game, so no Origin$-scoped line matches
// it.
//
// Only the entry-state bodies -- Tap, Untap and PutCounter, the whole
// corpus population of non-self Updated entry bodies except two -- are run;
// a Clone body (Essence of the Wild's EssenceClone, Displaced Dinosaurs) and
// an Animate body need the pre-entry as-enters machinery a folded token no
// longer has, and are left unapplied. The token's own Moved lines are not
// read here: token scripts carry none, and its own entry counters ride the
// entry-counter pass.
func (e *Engine) applyTokenEntryUpdates(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	ev := events.Event{Kind: events.MoveZone, Obj: id, Player: o.Controller,
		From: state.ZCeased, To: state.ZBattlefield}
	var matches []replMatch
	if e.effectReplacementsPossible() {
		ceL := e.active()
		for i := range ceL {
			ce := &ceL[i]
			if ce.ReplacementEvent != "Moved" || ce.Source == id {
				continue
			}
			with := replacementBodySA(ce.ReplacementBody)
			if with == nil || !tokenEntryBody(ce.ReplacementParams, with) {
				continue
			}
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams, With: with}
			if e.replacementMatchesEffectCreatedBy(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers, ce.Controller) {
				matches = append(matches, replMatch{id: ce.Source, repl: r, remembered: ce.Remembered,
					rememberedPlayers: ce.RememberedPlayers,
					chosen:            ce.ChosenNumber, controller: ce.Controller, frozenController: true,
					key: "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))})
			}
		}
	}
	e.forEachReplacementSourceFor(replEventBit("Moved"), func(src state.ObjID) {
		if src == id {
			return
		}
		f := e.replacementFace(src, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event != "Moved" || r.With == nil || !tokenEntryBody(r.Params, r.With) {
				continue
			}
			if e.replacementMatches(*r, src, ev) {
				matches = append(matches, replMatch{id: src, face: f, repl: r})
			}
		}
	})
	matches = e.dropAppliedReplacements(matches)
	for _, m := range matches {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			return
		}
		e.runReplaceWith(e.replCtx(m, ev), id, m.repl.With, nil)
	}
}

// tokenEntryBody reports whether a Moved line is an Updated battlefield-entry
// replacement whose body applyTokenEntryUpdates runs on a folded token.
func tokenEntryBody(params map[string]string, with *cards.SA) bool {
	if params["ReplacementResult"] != "Updated" || params["Destination"] != "Battlefield" {
		return false
	}
	switch with.API {
	case "Tap", "Untap", "PutCounter":
		return true
	}
	return false
}
