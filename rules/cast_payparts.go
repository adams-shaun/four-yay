package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// drawCostCard emits the ordinary Draw event one card of a cost payment
// draws: the library's top card moves to the payer's hand, and a draw from
// an empty library is the loss the SBA checks (the same shape DrawFor's
// no-replacement draw and resumeOrdinaryDraw emit).
func (e *Engine) drawCostCard(p state.PlayerID) {
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		e.playerLoses(p, loseReasonMilled, "drew from an empty library")
		return
	}
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// payMillCost settles every Mill<N> cost component: the payer mills the SUM
// of the parts' requirements from the top of their own library, one real
// MoveZone event per card in deterministic top-first order. The moves share a
// mill batch so MilledAll triggers once for this cost payment. No choice is
// involved, so nothing is asked. A mill instruction moves all remaining cards
// when its count exceeds the library size, so the snapshot clamps to the
// available prefix.
func (e *Engine) payMillCost(p state.PlayerID, parts []CostPart) {
	total, ok := millCostTotal(parts)
	if !ok || total <= 0 {
		return
	}
	lib := e.G.Zone(state.ZLibrary, p)
	if int64(len(lib)) < total {
		total = int64(len(lib))
	}
	// Snapshot the ids before emitting: each MoveZone mutates the library
	// the slice was read from.
	ids := append([]state.ObjID(nil), lib[:total]...)
	e.BeginMillBatch()
	for _, id := range ids {
		e.emit(events.Mill(id, p))
	}
	e.EndMillBatch()
}

func (e *Engine) payMillCostParts(pc *pendingCast) {
	e.payMillCost(pc.player, pc.cost.Mill)
}

// payDiscardCost settles every hand->graveyard discard component of a cost
// payment as ONE discard action: the payer discards the settled cards
// together, so Mode$ DiscardedAll fires once for the payment with its
// TriggerCount$Amount (and Remembered/Captured set) equal to the number of
// matching cards (CR 701.8), exactly as payMillCost batches a multi-card Mill
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
func (e *Engine) payDiscardCost(ids []state.ObjID, cycling string) {
	if len(ids) == 0 {
		return
	}
	e.BeginDiscardBatch()
	for _, id := range ids {
		if e.G.Obj(id) == nil {
			continue
		}
		if cycling != "" {
			e.emit(events.DiscardCostCycling(id, cycling))
		} else {
			e.emit(events.DiscardCost(id))
		}
	}
	e.EndDiscardBatch()
}

// payDrawCostParts settles every Draw cost component of a cast or activation
// payment: one ordinary draw per card of the part's count, for the drawer the
// part's spec names. The count is the literal N, or -- for the dynamic
// Draw<X/Spec> form -- the source's SVar bound by part.Dyn, resolved here at
// payment time (Champion of Wits' "draw cards equal to its power"). The
// offer gate (nonManaCastable) already proved each part's drawer and dynamic
// count resolvable, so a part that is somehow unresolvable at payment -- a
// stale stored cost -- pays nothing rather than guessing a count; the whole
// cost is never offered, so this is a belt-and-braces no-op, not a live path.
func (e *Engine) payDrawCostParts(pc *pendingCast) {
	for _, part := range pc.cost.Draw {
		drawer, ok := pay.CastFlowDrawPlayer(part.Spec, pc.player)
		if !ok {
			continue
		}
		n, ok := pay.DrawCostCount(asPayer(e), pc.card, pc.player, part)
		if !ok {
			continue
		}
		for k := int32(0); k < n; k++ {
			e.drawCostCard(drawer)
		}
	}
}

// settlePutToLibCost settles every PutToLib cost component of a cast or
// activation payment (PutCardToLibFrom<Zone><N/Pos/Spec>): the chosen cards
// move to their OWNER's library. MoveZone appends to the destination zone, so
// a plain move lands at the bottom (Forge's Pos -1); a top placement (Pos 0)
// follows the move with one LibraryOrder per owner putting the moved cards
// back on top in the order they were chosen -- exactly the shape effects'
// libraryOrderPlacement emits (the same private flag), re-derived here rather
// than imported because effects must never be reached for a cost settle.
func (e *Engine) settlePutToLibCost(pc *pendingCast) {
	idx := 0
	for _, part := range pc.cost.PutToLib {
		n := int(part.N)
		end := idx + n
		if end > len(pc.putToLibs) {
			end = len(pc.putToLibs)
		}
		picks := pc.putToLibs[idx:end]
		idx = end
		for _, id := range picks {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZLibrary,
					Text: "put on the library as a cost"})
			}
		}
		if part.LibraryPos == 0 && len(picks) > 0 {
			pay.PutLibPicksOnTop(asPayer(e), picks)
		}
	}
}

// payDamageCost makes the payer take n damage from the source -- the
// DamageYou<N> cost payment (Forge CostDamage). The event shape is the one
// payUnlessDamageCost emits: the Damage event names the payer, the engine's
// damage-source context names the source, and the same-source lifelink
// gains the controller the damage (CR 702.16d).
func (e *Engine) payDamageCost(payer state.PlayerID, n int32, source state.ObjID, sourceLKI damageKeywordLKI, sourceControllerLKI state.PlayerID) {
	if n <= 0 {
		return
	}
	liveSource := e.G.Obj(source)
	keywords := e.damageKeywordsOf(source)
	controller := payer
	if liveSource != nil && liveSource.Zone == state.ZBattlefield {
		controller = liveSource.Controller
	} else {
		keywords = sourceLKI
		controller = sourceControllerLKI
	}
	prev := e.SetDamageSource(source)
	dam := events.Event{Kind: events.Damage, Player: payer, Amount: n}
	if keywords.infect {
		// CR 702.90b: even a cost payment is damage dealt by its source, so
		// an infect source's DamageYou cost pays in counter/poison form.
		dam.Counter = "infect"
	}
	ev := e.emit(dam)
	e.SetDamageSource(prev)
	if ev.Kind != events.Damage || !keywords.lifelink {
		return
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: controller, Amount: n})
}
