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
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mintSink is one parked mint's collector: the objects the answers to its
// competitions minted, in mint order.
type mintSink struct {
	id  uint64
	ids []state.ObjID
}

// SuspendTokenRest implements effects' optional tokenRestHost: a mint never
// parks on the kernel (its elections and orders are answered in place), so
// there is no continuation to record.
func (e *Engine) SuspendTokenRest(sa *cards.SA, rest effects.TokenRest) bool { return false }

// tagMintContinuations names collector id on every mint continuation that
// does not have one yet: the queued competitions from index from on whose
// answer completes a mint (mintContinuation), and choice, a parked
// CreateToken election (nil for none). Reports whether anything was tagged.
func (e *Engine) tagMintContinuations(id uint64, from int, choice *tokenChoiceState) bool {
	tagged := false
	for i := from; i < len(e.replChoices); i++ {
		if mintContinuation(e.replChoices[i].kind) && e.replChoices[i].mintSink == 0 {
			e.replChoices[i].mintSink = id
			tagged = true
		}
	}
	if choice != nil && choice.mintSink == 0 {
		choice.mintSink = id
		tagged = true
	}
	return tagged
}

// mintContinuation reports whether a competition kind is one whose answer
// completes a parked mint: the entry-counter order of the minted token, or
// the CreateToken replacement order that rewrites the mint.
func mintContinuation(k replChoiceKind) bool {
	return k == replChoiceEntryOrder || k == replChoiceToken
}

// withMintSink runs the answer to one of a parked mint's continuations (an
// order competition, a CreateToken election, or the as-enters election the
// mint's own entry parked on) with the TokenCreate sink
// pointed at collector id, so every object the answer mints (the staged
// token, a chosen copy, the rest of its plan) is recorded for the waiting
// "token_rest" frame. Whatever the answer poses next for the same mint -- a
// re-posed order, a later plan mint's stage, a chosen-copy election --
// inherits the collector, so it follows the mint through every nested ask.
// id 0 (no DB$ Token waiting) runs f unchanged.
func (e *Engine) withMintSink(id uint64, f func()) {
	i := -1
	if id != 0 {
		i = e.mintSinkIndex(id)
	}
	if i < 0 {
		f()
		return
	}
	ids := append([]state.ObjID(nil), e.mintSinks[i].ids...)
	saved := e.tokenMintSink
	savedID := e.tokenMintSinkID
	e.tokenMintSink = &ids
	e.tokenMintSinkID = id // names this collector to every ask the answer poses
	queued, choiceBefore := len(e.replChoices), e.tokenChoice
	f()
	e.tokenMintSink, e.tokenMintSinkID = saved, savedID
	if i = e.mintSinkIndex(id); i >= 0 {
		e.mintSinks[i].ids = ids
	}
	var choice *tokenChoiceState
	if e.tokenChoice != choiceBefore {
		choice = e.tokenChoice
	}
	if queued > len(e.replChoices) {
		queued = len(e.replChoices)
	}
	e.tagMintContinuations(id, queued, choice)
}

func (e *Engine) mintSinkIndex(id uint64) int {
	for i := range e.mintSinks {
		if e.mintSinks[i].id == id {
			return i
		}
	}
	return -1
}

// publishTokenEntry is the ONE place a minted token's id reaches
// tokenMintSink -- and so the one place an effect's per-mint riders, markers
// and continuations (effects/token.go's TokenTapped$/RememberTokens$,
// Investigate's marker, Encore's haste and sacrifice group, Incubate's and
// Amass's counters, CopyPermanent's post-entry riders) are released. It runs
// in Engine.emit's tail for the FINAL event of every emit, after the fold:
//
//   - A TokenCreate/CardToken mints straight onto the battlefield, so its
//     entry completes in the same fold: want (the pre-fold NextID) is
//     published when the object exists. A mint that parked behind an
//     entry-counter order never reaches the fold; its answer's re-drive does,
//     and publishes then (into the collector withMintSink points the sink at).
//   - A CopyToken only creates the copy in the library; its entry is the
//     separate MoveZone that follows. The id is recorded as pending and
//     published only when a MoveZone of it folds onto the battlefield --
//     directly, or on the re-drive after a parked order or as-enters election
//     -- and dropped (never published) when the move lands anywhere else.
//
// Nothing else may append to tokenMintSink.
func (e *Engine) publishTokenEntry(stored events.Event, want state.ObjID) {
	switch stored.Kind {
	case events.TokenCreate, events.CardToken:
		if want != 0 && e.G.Obj(want) != nil && e.tokenMintSink != nil {
			*e.tokenMintSink = append(*e.tokenMintSink, want)
		}
	case events.CopyToken:
		if want != 0 && e.G.Obj(want) != nil {
			e.copyMintsPending = append(e.copyMintsPending, want)
		}
	case events.MoveZone:
		i := slices.Index(e.copyMintsPending, stored.Obj)
		if i < 0 {
			return
		}
		e.copyMintsPending = slices.Delete(e.copyMintsPending, i, i+1)
		o := e.G.Obj(stored.Obj)
		if stored.To == state.ZBattlefield && o != nil && o.Zone == state.ZBattlefield && e.tokenMintSink != nil {
			*e.tokenMintSink = append(*e.tokenMintSink, stored.Obj)
		}
	}
}
