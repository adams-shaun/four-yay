// unearth.go implements K:Unearth (CR 702.84a). The keyword's activation is
// an ordinary graveyard ability (cards/kw_unearth.go expands K:Unearth:<cost>
// into AB$ ChangeZone with Unearth$ True), so the offer, cost payment and
// graveyard->battlefield move ride the shared activated-ability machinery.
// This file owns the three riders the return implies:
//
//   - Haste: "It gains haste." A layer-6 grant scoped to the object itself,
//     UntilEOT, exactly the dash grant (altcast.go).
//   - The end-step exile: "Exile it at the beginning of the next end step."
//     A one-shot Mode$ Phase DelayedRegister at StepEnd resolving the
//     __kwUnearthExile builtin body, exactly the warp registration. The
//     registration is incarnation-tracked (its __kwUnearth prefix joins
//     events/apply.go's tracked set), so a permanent that left and returned
//     as a new incarnation is not exiled by a stale promise.
//   - The exile-instead replacement: "Exile it ... if it would leave the
//     battlefield." CR 702.84b makes this a replacement effect. It is
//     matched at the common move boundary (rules/replacement.go, the same
//     place the finality-counter rule lives): any MoveZone whose origin is
//     the battlefield and whose object is currently unearthed is redirected
//     to exile instead of its intended destination. "Currently unearthed"
//     is read from the live __kwUnearthExile registration for that exact
//     incarnation, so the redirect is a property of game state a log-only
//     replay rebuilds, never a mutable per-object marker.
//
// The entry provenance is the entered_unearthed Counter effects/zone.go
// stamps on the ChangeZone's MoveZone; altCostEnter reads it and calls
// unearthEnter. Nothing here writes a state.Game field directly: the haste
// is a ContinuousEffect and the exile promise is a logged DelayedRegister.
package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// unearthEnter applies CR 702.84a's two immediate riders to an object that
// just entered the battlefield through its unearth ability: the haste grant
// and the end-step exile promise. Called from altCostEnter for every
// battlefield entry whose event carries the entered_unearthed marker.
func (e *Engine) unearthEnter(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	// CR 702.84a: "It gains haste." A layer-6 grant scoped to the object
	// itself, UntilEOT -- the dash grant's exact shape (altcast.go). It
	// leaves with the permanent at the next end step anyway; the turn
	// boundary drops the grant for the pathological case that it stays.
	e.AddContinuous(state.ContinuousEffect{
		Source: id, Affects: "Card.Self", Controller: o.Controller,
		Layer: state.LAbilities, AddKeywords: []string{"Haste"}, UntilEOT: true,
	})
	// CR 702.84a: "Exile it at the beginning of the next end step." One
	// one-shot registration, fired by checkDelayedTriggers at the first end
	// step reached and removed by DelayedPush. __kwUnearthExile joins the
	// incarnation-tracked prefix set, so a re-entering card is not exiled by
	// this promise.
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id,
		Player: o.Controller, Step: state.StepEnd, Counter: "__kwUnearthExile"})
}

// unearthReplacementApplies reports whether id is a permanent currently under
// CR 702.84a's exile-if-it-would-leave replacement. It is derived entirely
// from game state -- the live __kwUnearthExile delayed registration whose
// source incarnation still matches the object's -- so a replay that folds the
// logged DelayedRegister rebuilds the same answer, and a leave after the
// promise already fired (or after the object changed incarnation) is not
// redirected.
func (e *Engine) unearthReplacementApplies(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		if dt.Source == id && dt.Execute == "__kwUnearthExile" &&
			dt.TrackSource && dt.SourceIncarnation == o.Incarnation {
			return true
		}
	}
	return false
}
