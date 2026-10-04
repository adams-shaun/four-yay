package pay

import (
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// PayMillCost settles every Mill<N> cost component: the payer mills the SUM
// of the parts' requirements from the top of their own library, one real
// MoveZone event per card in deterministic top-first order. The moves share a
// mill batch so MilledAll triggers once for this cost payment. No choice is
// involved, so nothing is asked. A mill instruction moves all remaining cards
// when its count exceeds the library size, so the snapshot clamps to the
// available prefix.
func PayMillCost(e Engine, p state.PlayerID, parts []costvocab.CostPart) {
	total, ok := costvocab.MillCostTotal(parts)
	if !ok || total <= 0 {
		return
	}
	lib := e.Game().Zone(state.ZLibrary, p)
	if int64(len(lib)) < total {
		total = int64(len(lib))
	}
	// Snapshot the ids before emitting: each MoveZone mutates the library
	// the slice was read from.
	ids := append([]state.ObjID(nil), lib[:total]...)
	e.Batch(BatchMill, true)
	for _, id := range ids {
		e.Emit(events.Mill(id, p))
	}
	e.Batch(BatchMill, false)
}

// PayDiscardCost settles every hand->graveyard discard component of a cost
// payment as ONE discard action: the payer discards the settled cards
// together, so Mode$ DiscardedAll fires once for the payment with its
// TriggerCount$Amount (and Remembered/Captured set) equal to the number of
// matching cards (CR 701.8), exactly as PayMillCost batches a multi-card Mill
// cost. Every cost discard goes through this helper -- a cast, an activated
// ability, a mana-ability activation and a triggered mandatory cost all reach
// it -- so the four sites cannot diverge and a new one cannot forget the
// bracket. The bracket is opened and closed entirely inside this call and the
// emission loop cannot suspend, so successive cost actions never coalesce and
// an enclosing api:Discard batch (effects/cardflow.go's effDiscard) simply
// nests by depth. cycling names the cycling ability when the discard is paid
// for one (CR 702.29), tagging each card's event for Mode$ Cycled; an empty
// keyword emits the plain cost form. An absent object is skipped, exactly as
// the triggered-cost site's own guard did.
func PayDiscardCost(e Engine, ids []state.ObjID, cycling string) {
	if len(ids) == 0 {
		return
	}
	e.Batch(BatchDiscard, true)
	for _, id := range ids {
		if e.Game().Obj(id) == nil {
			continue
		}
		if cycling != "" {
			e.Emit(events.DiscardCostCycling(id, cycling))
		} else {
			e.Emit(events.DiscardCost(id))
		}
	}
	e.Batch(BatchDiscard, false)
}
