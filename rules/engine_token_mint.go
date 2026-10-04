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
	// (EmitTokenCreate). It is a
	// stack discipline: a nested token creation saves and restores the outer
	// sink, so the outer effect's rider loop sees only its own mints. Nil on
	// every ordinary Emit, so no other emit pays for the collection.
	tokenMintSink *[]state.ObjID `clone:"reset"`
	// copyMintsPending are the CopyToken mints (a chosen-copy token plan's,
	// a DB$ CopyPermanent's) whose battlefield MoveZone has not completed
	// yet: the object exists in the library but has not entered. The emit
	// tail (publishTokenEntry) publishes such an id to tokenMintSink only
	// when its entry actually folds onto the battlefield -- directly, or on
	// the re-drive after a parked entry-counter order or as-enters election
	// is answered -- and drops it when the move lands anywhere else.
	copyMintsPending []state.ObjID `clone:"deep"`
	// answerInResolution is set for the synchronous extent of an answer to a
	// competition or CreateToken election that suspended a stack resolution
	// (handleReplacement for an inResolution competition, tokenReplAnswer for
	// an election carrying a parkedResume). resolvingObj is 0 there, yet an
	// entry the answer stages still belongs to that resolution: its order
	// competition must resume the suspended frame when answered, not drop it
	// as a cast-window pose's bookkeeping. Never set between calls.
	answerInResolution bool `clone:"reset"`
	// stackCopyMintSink, when non-nil, collects the object the StackCopy
	// event currently being emitted actually minted (EmitStackCopy). Same
	// stack discipline as tokenMintSink: a nested stack copy saves and
	// restores the outer sink. A StackCopy never spawns more than one object
	// (this engine has no CopySpell replacement), so the slice holds at most
	// one id. Nil on every ordinary Emit, so no other emit pays for it.
	stackCopyMintSink *[]state.ObjID `clone:"reset"`
}
