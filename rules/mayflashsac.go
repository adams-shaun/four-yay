package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// The coverage census: kw:MayFlashSac is implemented as a casting
	// permission plus an ETB cleanup hook, both read directly off the K: line
	// (the Flash/Bestow/Mutate/MayFlashCost family -- see rules/mayflash.go's
	// doc), so it is registered here in its own file exactly as bestow.go and
	// mutate.go register theirs. Proof: the sorcery-speed and instant-window
	// casts of Necromancy in rules/mayflashsac_test.go.
	effects.RegisterNonAPI("kw:MayFlashSac")
}

// MayFlashSac (Forge's K:MayFlashSac) is the whole CR 702.8 "you may cast
// this as though it had flash" permission together with the keyword's own
// self-sacrifice rider:
//
//	K:MayFlashSac
//	  "You may cast Necromancy as though it had flash. If you cast it any time
//	   a sorcery couldn't have been cast, the controller of the permanent it
//	   becomes sacrifices it at the beginning of the next cleanup step."
//
// The keyword takes NO colon parameter -- unlike MayFlashCost (rules/mayflash.go),
// which charges an additional cost for the permission, this one is free and
// bears the consequence instead. Both halves are therefore read directly by
// rules rather than expanded onto the face (the Flash/Flashback/Bestow/Mutate
// family cards/keywords.go's expandKeywords doc names): there is no face
// ability to add. The pieces:
//
//   - the permission itself is read inline from spellTimingOK
//     (rules/statics.go), which accepts the cast at instant timing exactly
//     like the printed Flash keyword. No extra cost is folded into the offer.
//   - offSorceryAtCast captures whether a just-announced cast is the
//     off-sorcery shape the rider keys on: the cast was not made at a time a
//     sorcery could have been cast. It is evaluated at beginCast (before CR
//     601.2a puts the spell on the stack, so the stack-emptiness half of
//     sorcerySpeed is the pre-cast board state, not this spell's own push)
//     and, gated on the face actually carrying the keyword, stamped onto the
//     pay-time CastInfo as state.FlagMayFlashSac -- the replayable provenance
//     the ETB hook reads.
//   - mayFlashSacEnter (called from altCostEnter for every battlefield entry)
//     reads that flag and registers the delayed sacrifice at the next cleanup
//     step, through the ordinary DelayedRegister/DelayedPush machinery with
//     the builtin __kwMayFlashSacrifice SVar. A sorcery-timed cast carries no
//     flag, so it registers nothing and the permanent stays. The promise is
//     INCARNATION-TRACKED (events.Apply's DelayedRegister track set): a
//     MayFlashSac permanent that leaves the battlefield and returns before
//     that cleanup expires the promise without acting on the returned
//     incarnation, the same CR 400.7 discipline dash/warp/AtEOT already
//     follow. The registration is one-shot and resolves inside the cleanup
//     step itself through the CR 514.3 tail (rules/turn.go's
//     finishCleanupStep) -- not at the next turn's upkeep.

// mayFlashSacFace reports whether f prints K:MayFlashSac. A nil face, or one
// whose keyword is absent, is false -- the permission and the rider both key
// off the same read, so they cannot disagree about which cards carry it.
//
// The read is the face's interned keyword-head bitset (one bit test on a bound
// face), not HasKeyword's string path: spellTimingOK asks it for every
// non-instant hand card on every priority walk. MayFlashSac has no compiled
// keyword-mask bit, so HasKeyword answered from this same line scan anyway.
func mayFlashSacFace(f *cards.Face) bool {
	return f != nil && f.KeywordLinesHaveHead(kwhMayFlashSac.S, kwhMayFlashSac.ID)
}

// kwhMayFlashSac is the compiled K:MayFlashSac head mayFlashSacFace reads.
var kwhMayFlashSac = newKWHead("MayFlashSac")

// offSorceryAtCast captures the CR 702.8 rider's condition at announcement
// time: true when the cast was made at a time a sorcery could NOT have been
// cast. It is evaluated in beginCast, before the spell is pushed (CR 601.2a),
// so sorcerySpeed's empty-stack half reflects the board the caster announced
// into rather than the spell being announced. payCast's flag arm gates on
// !pc.isAbility(), so an ability proposal never stamps the flag.
func (e *Engine) offSorceryAtCast(p state.PlayerID) bool {
	return !e.sorcerySpeed(p)
}

// mayFlashSacEnter registers the cleanup-step delayed sacrifice for a
// permanent the spell became, when that spell was cast off-sorcery through
// the keyword. It is called from altCostEnter (a battlefield MoveZone), so
// the registration's Source is the entering permanent -- the "controller of
// the permanent it becomes" the oracle text names. One DelayedRegister event
// is emitted, so replay rebuilds the registration identically.
func (e *Engine) mayFlashSacEnter(id state.ObjID, controller state.PlayerID) {
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id,
		Player: controller, Step: state.StepCleanup, Counter: "__kwMayFlashSacrifice"})
}
