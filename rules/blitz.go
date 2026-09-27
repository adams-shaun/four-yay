package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// The coverage census: kw:Blitz is implemented as a casting option plus an
	// entry hook, read directly off the K: line and the pay-time CastInfo flag
	// (the Dash/MayFlashSac family, rules/altcast.go's doc), so it registers
	// here in its own file exactly as bestow.go and mutate.go register theirs.
	// Proof: the real-corpus tests in rules/blitz_test.go.
	effects.RegisterNonAPI("kw:Blitz")
}

// Blitz (CR 702.152) is the alternative-cost keyword:
//
//	K:Blitz:{2}{R}{R}, Discard a card
//	  "If you cast this spell for its blitz cost, it gains haste and 'When
//	   this creature dies, draw a card.' Sacrifice it at the beginning of the
//	   next end step."
//
// The alternative cost itself rides the ordinary alternative-cost family:
// legal.go's hand walk offers the "blitzed" mode (and the may-play walk offers
// it from a graveyard a MayPlay$ Spell.Blitz static grants), beginCast's
// "blitzed" case charges ParseCost of the written K: parameter in place of the
// mana cost -- including the trailing Discard<1/Card> part, which ParseCost
// already models -- and modeFlags records state.FlagBlitzed. The riders are
// what this file owns, registered by altCostEnter (the same battlefield-entry
// hook dash and MayFlashSac use) off that flag:
//
//   - haste while it entered this turn (a layer-6 UntilEOT grant, the Dash
//     shape);
//   - "When this creature dies, draw a card": a runtime-granted trigger (a
//     ContinuousEffect carrying an AddTrigger$, the static-grant route
//     layers.go uses) whose Execute$ resolves the builtin __kwBlitzDraw body.
//     The effect's Affects$ is Card.Self and its Source is the permanent
//     itself, so only that permanent gains the trigger, and the source-leaves
//     rule drops it when the permanent leaves -- while the leaves-the-
//     battlefield look-back walk reads the PRE-departure continuous list
//     (checkTriggers' triggerBefore snapshot), so the trigger is still live
//     for the very death it observes;
//   - the next-end-step sacrifice: a DelayedRegister with the builtin
//     __kwBlitzSacrifice body, the Dash/MayFlashSac machinery.
//
// Every registration is a real event (DelayedRegister) or an AddContinuous
// the replay re-executes, so a replayed game re-derives the identical board.

// blitzFace reports whether f prints K:Blitz. A nil face, or one whose
// keyword is absent, is false.
func blitzFace(f *cards.Face) bool {
	return f != nil && f.HasKeyword("Blitz")
}

// blitzEnter registers the CR 702.152 riders for a permanent a blitz-cost
// spell became. It is called from altCostEnter (a battlefield MoveZone), so
// the registrations' Source is the entering permanent -- the "creature" the
// oracle text names.
func (e *Engine) blitzEnter(id state.ObjID, controller state.PlayerID) {
	// Haste (CR 702.152c): a layer-6 grant scoped to the object itself, the
	// Dash shape. It is UntilEOT because it already resolved as a one-shot;
	// the permanent is sacrificed at the next end step anyway.
	e.AddContinuous(state.ContinuousEffect{
		Source: id, Affects: "Card.Self", Controller: controller,
		Layer: state.LAbilities, AddKeywords: []string{"Haste"}, UntilEOT: true,
	})
	// "When this creature dies, draw a card" (CR 702.152c): a runtime-granted
	// trigger. The static-grant walk resolves Execute$ against the grantor's
	// own face SVar table (falling back to cards' builtinSVars), so the body
	// is the builtin __kwBlitzDraw. Affects$ Card.Self pins the grant to this
	// permanent; the source-leaves rule drops it when the permanent leaves,
	// and the death look-back sees the pre-departure list.
	if t, ok := cards.ParseTriggerLine(
		"Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | Execute$ __kwBlitzDraw | TriggerDescription$ When this creature dies, draw a card."); ok {
		e.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: controller,
			AddTrigger: &t,
			// The grant carries the controller's SVar table implicitly through
			// the source face; TriggerGrantor stays 0 (the statics route, where
			// Source itself carries the body).
		})
	}
	// Sacrifice at the beginning of the next end step (CR 702.152d). One
	// DelayedRegister event, so replay rebuilds the registration identically.
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id,
		Player: controller, Step: state.StepEnd, Counter: "__kwBlitzSacrifice"})
}
