package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// engineTokenMint groups the Engine's parked-mint and token-emission
// collectors. It is embedded by value in Engine (rules/engine_struct.go),
// so every field keeps its documented contract comment and every existing
// e.<field> access keeps compiling unchanged through Go's field promotion.
// Clone's per-field copy classes (rules/clone.go) are unchanged by the
// move.
type engineTokenMint struct {
	// tokenMintSink, when non-nil, collects every object the TokenCreate or
	// CardToken event currently being emitted actually created
	// (EmitTokenCreate, and a parked mint's answer through withMintSink). It is a
	// stack discipline: a nested token creation saves and restores the outer
	// sink, so the outer effect's rider loop sees only its own mints. Nil on
	// every ordinary Emit, so no other emit pays for the collection.
	tokenMintSink *[]state.ObjID
	// mintParkFrom is EmitTokenCreate's report to SuspendTokenRest (rules/
	// token_rest.go): 1 + the replacement-choice queue length before an emit
	// that parked the resolution behind a replacement-order ask, else 0.
	// mintSinks are the collectors those parked mints' answers mint into,
	// keyed by an id from mintSinkSeq and consumed by the "token_rest" frame.
	// mintParkElection is the same report for the OTHER park: an as-enters
	// election (etbMove/riotMove/unleashMove/siegeMove) posed from inside the
	// emit parked the mint's own entry, so the election -- not a queued
	// competition -- is the continuation the collector rides (SuspendTokenRest
	// tags it through pendingMintSink).
	mintParkFrom     int
	mintParkElection bool
	mintSinks        []mintSink
	mintSinkSeq      uint64
	// tokenMintSinkID is the named collector (a mintSinks key) the current
	// tokenMintSink scope belongs to: withMintSink sets it to its collector's
	// id for the scope's duration and EmitTokenCreate resets it to 0 (its own
	// sink is a local buffer, never a named collector), both restored after.
	// An ask posed inside the scope records it as pendingMintSink, so an
	// as-enters election the scope's entry parked on carries the collector to
	// its answer. Engine scratch, never logged; Clone-copied (clone.go).
	tokenMintSinkID uint64
	// pendingMintSink is the collector id the PENDING mid-resolution ask's
	// answer must run under when that ask is an as-enters election that
	// parked a resolving DB$ Token's mint entry: Engine.ask records it from
	// tokenMintSinkID at pose time, and the election answer arms (rules/
	// turn.go's chooseETBEntry, chooseRiot, chooseUnleash, chooseSiege)
	// re-emit the parked entry under withMintSink with it, so publishTokenEntry
	// lands the minted id in the waiting collector. 0 = no collector. It is
	// overwritten at every pose, never cleared: a stale id names a collector
	// takeMintSink has already consumed, and withMintSink(0-or-spent) runs
	// unchanged. Engine scratch, never logged; Clone-copied (clone.go).
	pendingMintSink uint64
	// copyMintsPending are the CopyToken mints (a chosen-copy token plan's,
	// a DB$ CopyPermanent's) whose battlefield MoveZone has not completed
	// yet: the object exists in the library but has not entered. The emit
	// tail (publishTokenEntry) publishes such an id to tokenMintSink only
	// when its entry actually folds onto the battlefield -- directly, or on
	// the re-drive after a parked entry-counter order or as-enters election
	// is answered -- and drops it when the move lands anywhere else.
	copyMintsPending []state.ObjID
	// answerInResolution is set for the synchronous extent of an answer to a
	// competition or CreateToken election that suspended a stack resolution
	// (handleReplacement for an inResolution competition, tokenReplAnswer for
	// an election carrying a parkedResume). resolvingObj is 0 there, yet an
	// entry the answer stages still belongs to that resolution: its order
	// competition must resume the suspended frame when answered, not drop it
	// as a cast-window pose's bookkeeping. Never set between calls.
	answerInResolution bool
	// stackCopyMintSink, when non-nil, collects the object the StackCopy
	// event currently being emitted actually minted (EmitStackCopy). Same
	// stack discipline as tokenMintSink: a nested stack copy saves and
	// restores the outer sink. A StackCopy never spawns more than one object
	// (this engine has no CopySpell replacement), so the slice holds at most
	// one id. Nil on every ordinary Emit, so no other emit pays for it.
	stackCopyMintSink *[]state.ObjID
}
