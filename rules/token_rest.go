// token_rest.go holds publishTokenEntry, the one place a minted token's id
// reaches the emitting effect's tokenMintSink.
package rules

import (
	"slices"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// publishTokenEntry is the ONE place a minted token's id reaches
// tokenMintSink -- and so the one place an effect's per-mint riders, markers
// and continuations (effects/token.go's TokenTapped$/RememberTokens$,
// Investigate's marker, Encore's haste and sacrifice group, Incubate's and
// Amass's counters, CopyPermanent's post-entry riders) are released. It runs
// in Engine.emit's tail for the FINAL event of every emit, after the fold:
//
//   - A TokenCreate/CardToken mints straight onto the battlefield, so its
//     entry completes in the same fold: want (the pre-fold NextID) is
//     published when the object exists.
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
