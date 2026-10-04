package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// AtEOT$ is Forge's end-of-turn rider: "at the beginning of the next end
// step" the affected object leaves the battlefield (exiled, sacrificed,
// returned to hand, destroyed). It rides the same delayed-registration
// machinery dash/warp/encore and CopyPermanent already use -- one
// events.DelayedRegister per affected object, fired one-shot by
// rules.checkDelayedTriggers at the first end step reached after resolution,
// resolved through the builtin SVar bodies in cards/builtinSVars.
//
// The affected set is the primitive's own: Animate/Pump act on their
// per-object targets, Token on each mint, ChangeZone/ChangeZoneAll on the
// objects the move actually moved (some carriers RememberChanged$ and some
// do not, so the moved set is passed in rather than read back out of
// Remembered).
//
// TIMING READING (deferred nuance, stated here once): Forge distinguishes
// "at the beginning of YOUR next end step" (YourExile/YourSacrifice/
// YourExileUpkeep...) from the bare "the next end step". This build treats
// the Your-prefixed values as their plain equivalents -- the first end step
// reached after resolution -- because the corpus's carriers overwhelmingly
// resolve on their controller's own turn, where the two coincide. The
// divergence (a rider resolving on an opponent's turn would fire at that
// opponent's end step, a turn early) is recorded in the report.
//
// Values outside the implemented set stay LOUD: one Note per call naming the
// value, with the body still applied (the CopyPermanent convention). A value
// is never silently dropped.
func scheduleAtEOT(h Host, c *Ctx, sa *cards.SA, affected []state.ObjID) {
	value := strings.TrimSpace(sa.ParamStr(cards.PKAtEOT))
	if value == "" {
		return
	}
	body := atEOTBody(value)
	if body == "" {
		// Out of scope (Library shuffle-into, the end-of-combat and
		// next-upkeep families, an unrecognised spelling): one loud Note
		// per call -- emitted even when the affected set is EMPTY, so a body
		// carrying an unimplemented value is never dropped silently (the
		// affected set is empty exactly when there is nothing to schedule,
		// which is when a silent drop would be easiest to miss) -- and the
		// body still stands.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "AtEOT$ " + value + " is not implemented; the affected object stays on the battlefield (one Note per call)"})
		return
	}
	// RememberChanged$'s Ledger of remembered ids is not consulted: the
	// caller owns the moved set.
	for _, id := range affected {
		if h.Game().Obj(id) == nil {
			continue // a caller may hand over an id that never resolved
		}
		// The registration's Source IS the affected object, so the builtin
		// body's Defined$ Self resolves to it -- exactly the dash/warp/
		// encore shape. StepEnd is the first end step reached after
		// resolution (one-shot, removed by DelayedPush).
		h.Emit(events.Event{Kind: events.DelayedRegister, Obj: id,
			Player: c.Controller, Step: state.StepEnd, Counter: body})
	}
}

// atEOTBody maps an AtEOT$ value to the builtin SVar body its registration
// fires. Reusing the existing dash/warp/encore bodies gives each family its
// established semantics for free: __kwWarpExile is incarnation-tracked (a
// card that left the battlefield and returned as a new incarnation is not
// exiled by a stale promise; events/apply.go tracks the __kwWarp and
// __kwAtEOT prefixes), __kwDashReturn and __kwAtEOTDestroy likewise.
// __kwEncoreSacrifice is not tracked, the CopyPermanent convention. Every
// off-battlefield stale promise is a no-op at fire time: warp/dash carry an
// Origin$ Battlefield guard, and effSacrifice's object path skips an object
// that is not on the battlefield.
func atEOTBody(value string) string {
	if v, ok := atEOTBodyTab1.Get(value); ok {
		return v
	}
	return ""
}

// atEOTInclude protects the source from an AtEOT$ Remembered rider unless the
// source card's persistent remembered list actually contains it. The shared
// Defined$ resolver removes a trigger referent that Forge's card list does not
// remember; retain this guard as a defensive check for delayed riders.
func atEOTInclude(h Host, c *Ctx, sa *cards.SA, id state.ObjID) bool {
	if !DefinedRefOf(sa).Is(RefRemembered) || id != c.Source {
		return true
	}
	if o := h.Game().Obj(c.Source); o != nil {
		for _, t := range o.Remembered {
			if !t.IsPlayer && t.Obj == id {
				return true
			}
		}
	}
	return false
}

var atEOTBodyTab1 = state.NewStrTable[string](
	state.StrEntry[string]{Key: "Exile", Val: "__kwWarpExile"},
	state.StrEntry[string]{Key: "YourExile", Val: "__kwWarpExile"},
	state.StrEntry[string]{Key: "Sacrifice", Val: "__kwEncoreSacrifice"},
	state.StrEntry[string]{Key: "YourSacrifice", Val: "__kwEncoreSacrifice"},
	state.StrEntry[string]{Key: "SacrificeCtrl", Val: "__kwEncoreSacrifice"},
	state.StrEntry[string]{Key: "Hand", Val: "__kwDashReturn"},
	state.StrEntry[string]{Key: "Destroy", Val: "__kwAtEOTDestroy"},
)
