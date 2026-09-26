// token_rest.go carries a resolving DB$ Token across a replacement-order ask
// posed from INSIDE one of its mints.
//
// A TokenCreate can park behind a CR 616.1 order choice -- the token's
// entry-counter grant under non-commuting AddCounter replacements
// (rules/entry_counters.go's stage), or competing CreateToken replacements
// (replChoiceToken). The ask suspends the resolution before the object
// exists, so EmitTokenCreate returns without the minted id and the effect's
// per-mint riders (TokenTapped$, RememberTokens$, PumpKeywords$, AttachedTo$,
// ...) have nothing to land on; worse, the effect's next mint would be
// emitted into the outstanding ask and read as its re-drive. effToken
// therefore stops at the park and reports its continuation here: a
// "token_rest" frame that re-enters the Token SA once the answer has minted,
// with the frozen per-resolution job, the cursor of the parked mint, and the
// objects the answer actually created.
//
// Those objects are collected by id-keyed value data, never a shared
// pointer, so Engine.Clone copies them like the rest of the resume state:
// each competition the park posed (and every re-pose or later plan mint its
// answer poses) names the collector through replChoice.mintSink, and
// withMintSink points the answer's TokenCreate sink at it.
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// mintSink is one parked mint's collector: the objects the answers to its
// competitions minted, in mint order.
type mintSink struct {
	id  uint64
	ids []state.ObjID
}

// SuspendTokenRest implements effects' optional tokenRestHost: the Token SA's
// last EmitTokenCreate parked this resolution (mintParkFrom), so record the
// frame that re-enters sa with rest once the answer has minted. Every
// competition the park appended is tagged with a fresh collector. Setting
// repeatReported suppresses the enclosing Resolve loop's own
// SuspendContinuation report of sa (the SuspendFlipRest convention: the frame
// re-enters the primitive itself, and its walk continues at sa.Sub).
func (e *Engine) SuspendTokenRest(sa *cards.SA, rest effects.TokenRest) bool {
	from := e.mintParkFrom - 1
	e.mintParkFrom = 0
	if from < 0 || e.resume == nil || from > len(e.replChoices) {
		return false
	}
	e.mintSinkSeq++
	id := e.mintSinkSeq
	tagged := false
	for i := from; i < len(e.replChoices); i++ {
		if mintContinuation(e.replChoices[i].kind) && e.replChoices[i].mintSink == 0 {
			e.replChoices[i].mintSink = id
			tagged = true
		}
	}
	if !tagged {
		return false
	}
	e.mintSinks = append(e.mintSinks, mintSink{id: id})
	rest.SinkID = id
	rest = rest.Clone()
	e.contChain = append(e.contChain, contFrame{sa: sa, tokenRest: &rest})
	e.repeatReported = sa
	return true
}

// mintContinuation reports whether a competition kind is one whose answer
// completes a parked mint: the entry-counter order of the minted token, or
// the CreateToken replacement order that rewrites the mint.
func mintContinuation(k replChoiceKind) bool {
	return k == replChoiceEntryOrder || k == replChoiceToken
}

// withMintSink runs an answered competition's work with the TokenCreate sink
// pointed at the competition's parked-mint collector, so every object the
// answer mints (the staged token, the rest of its plan) is recorded for the
// waiting "token_rest" frame. A competition the answer re-poses or newly
// parks for the same mint inherits the collector. rc without a collector runs
// f unchanged.
func (e *Engine) withMintSink(rc replChoice, f func()) {
	if rc.mintSink == 0 {
		f()
		return
	}
	i := e.mintSinkIndex(rc.mintSink)
	if i < 0 {
		f()
		return
	}
	ids := append([]state.ObjID(nil), e.mintSinks[i].ids...)
	saved := e.tokenMintSink
	e.tokenMintSink = &ids
	queued := len(e.replChoices)
	f()
	e.tokenMintSink = saved
	if i = e.mintSinkIndex(rc.mintSink); i >= 0 {
		e.mintSinks[i].ids = ids
	}
	for j := queued; j < len(e.replChoices); j++ {
		if mintContinuation(e.replChoices[j].kind) && e.replChoices[j].mintSink == 0 {
			e.replChoices[j].mintSink = rc.mintSink
		}
	}
}

func (e *Engine) mintSinkIndex(id uint64) int {
	for i := range e.mintSinks {
		if e.mintSinks[i].id == id {
			return i
		}
	}
	return -1
}

// takeMintSink removes and returns collector id's objects (the "token_rest"
// frame's re-entry consumes it exactly once).
func (e *Engine) takeMintSink(id uint64) []state.ObjID {
	i := e.mintSinkIndex(id)
	if i < 0 {
		return nil
	}
	ids := e.mintSinks[i].ids
	e.mintSinks = append(e.mintSinks[:i:i], e.mintSinks[i+1:]...)
	return ids
}
