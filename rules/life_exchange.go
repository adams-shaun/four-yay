package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// life_exchange.go is the life-exchange transaction (CR 701.12, api:ExchangeLife
// and its power/toughness variants): staging both sides, consuming them as
// their LifeChange events apply, and settling a suspended exchange.

// EmitLifeChange reports whether the exact proposed life change was applied.
// Kept for non-exchange callers; ExchangeLifeVariant uses the transactional
// method below so transformed and parked events can complete coherently.
func (e *Engine) EmitLifeChange(ev events.Event) bool {
	queued := len(e.replChoices)
	stored := e.emit(ev)
	return e.pending == nil && len(e.replChoices) == queued && stored.Kind == events.LifeChange && stored.Player == ev.Player && stored.Amount == ev.Amount
}

// ExchangeLifeVariant carries the exchange across a CR 616 choice. The life
// change is resolved first; the source's selected characteristic is set to
// its former total only if the player's life actually changed.
func (e *Engine) ExchangeLifeVariant(ev events.Event, source state.ObjID, controller state.PlayerID, oldLife int32, setPower, setToughness bool) {
	tx := &lifeExchangeTransaction{source: source, controller: controller, oldLife: oldLife,
		player: ev.Player, lifeBefore: e.G.Players[ev.Player].Life, setPower: setPower, setToughness: setToughness}
	e.lifeExchange = tx
	e.emit(ev)
	e.lifeExchange = nil
	if e.pending == nil && len(e.replChoices) == 0 {
		e.finishLifeExchange(tx)
	} else {
		// The life change suspended (a consumed GainLife→Draw body that itself
		// parked a Dredge ask): park the transaction so a later drain can
		// finish it. Nothing else references tx once this call returns.
		e.pendingLifeExchange = tx
	}
}

// settlePendingLifeExchange re-drives a parked exchange transaction once the
// engine is idle again. It is the ONE home every post-suspension drain calls
// (askNextReplacementChoice, and through it Submit's tail): a suspension that
// is not a replacement-order ask leaves the transaction referenced by nothing
// else, so without this the exchange silently half-applies. A second
// suspension inside finishLifeExchange re-parks it.
func (e *Engine) settlePendingLifeExchange() {
	tx := e.pendingLifeExchange
	if tx == nil || e.pending != nil || len(e.replChoices) > 0 {
		return
	}
	e.pendingLifeExchange = nil
	e.finishLifeExchange(tx)
}

// finishLifeExchange completes an exchange transaction. It resumes from the
// stage the transaction stopped at, so a re-drive after a suspension (the
// dredge arm, Submit's tail) settles exactly the sides not yet applied. It
// re-parks the transaction on the engine whenever a replacement path
// suspends again, so the transaction is never orphaned mid-flight.
func (e *Engine) finishLifeExchange(tx *lifeExchangeTransaction) {
	if tx == nil {
		return
	}
	if e.pendingLifeExchange == tx {
		e.pendingLifeExchange = nil
	}
	if e.pending != nil || len(e.replChoices) > 0 {
		e.pendingLifeExchange = tx
		return
	}
	if tx.second.Kind != 0 && tx.stage == 0 {
		tx.stage = 1
		prior := e.lifeExchange
		e.lifeExchange = tx
		e.emit(tx.second)
		e.lifeExchange = prior
		if e.pending != nil || len(e.replChoices) > 0 {
			e.pendingLifeExchange = tx
			return
		}
		e.finishLifeExchange(tx)
		return
	}
	if tx.second.Kind != 0 {
		if len(tx.staged) != 2 {
			e.emit(events.Event{Kind: events.Note, Obj: tx.source, Player: tx.controller,
				Text: "ExchangeLife settled with a replaced life-change side"})
		}
		prior, applying := e.lifeExchange, e.applyingReplacement
		e.lifeExchange, e.applyingReplacement = nil, true
		for _, ev := range tx.staged {
			e.emit(ev)
		}
		e.lifeExchange, e.applyingReplacement = prior, applying
		if tx.rememberLoss && tx.rememberMemory != nil &&
			int(tx.controller) < len(e.G.Players) {
			if loss := tx.controllerLife - e.G.Players[tx.controller].Life; loss > 0 {
				// The memory is the chain's SHARED pointer (Ctx.ExchangeMemory),
				// so this write is visible to every Ctx a suspension rebuilt:
				// the SubAbility$ continuation that reads Count$RememberedNumber
				// holds the same memory no matter which frame it resumed from.
				tx.rememberMemory.Number = loss
			}
		}
		return
	}
	if int(tx.player) >= len(e.G.Players) || e.G.Players[tx.player].Life == tx.lifeBefore {
		e.emit(events.Event{Kind: events.Note, Obj: tx.source, Player: tx.player, Text: "ExchangeLifeVariant abandoned: life did not change"})
		return
	}
	ce := state.ContinuousEffect{Source: tx.source, Controller: tx.controller, Affects: "Card.Self",
		Layer: state.LPT, Sub: state.SubSet, HasSet: true, SetPower: tx.oldLife, SetToughness: tx.oldLife,
		SetPowerPresent: tx.setPower, SetToughnessPresent: tx.setToughness, StaticSet: true}
	e.AddContinuous(ce)
}

func (e *Engine) stageExchangeLife(ev events.Event) events.Event {
	tx := e.lifeExchange
	if tx != nil {
		tx.staged = append(tx.staged, ev)
	}
	return ev
}

func (e *Engine) consumeExchangeLifeSide(ev events.Event) {
	if e.lifeExchange == nil || e.lifeExchange.second.Kind == 0 {
		return
	}
	e.emit(events.Event{Kind: events.Note, Obj: e.lifeExchange.source, Player: ev.Player,
		Text: "ExchangeLife settled with a replaced life-change side"})
	e.lifeExchange.staged = append(e.lifeExchange.staged, events.Event{Kind: events.LifeChange, Player: ev.Player})
}

// ExchangeLife carries both life changes through the replacement machinery as
// one continuation. Neither event is applied until both replacement paths,
// including any suspended CR 616 choices, have settled.
func (e *Engine) ExchangeLife(first, second events.Event, controller state.PlayerID, oldControllerLife int32, ctx *effects.Ctx, rememberLoss bool) {
	tx := &lifeExchangeTransaction{source: first.Obj, controller: controller, player: first.Player,
		lifeBefore: e.G.Players[first.Player].Life, second: second, rememberLoss: rememberLoss,
		controllerLife: oldControllerLife}
	if ctx != nil {
		tx.rememberMemory = ctx.ExchangeMemory
	}
	e.lifeExchange = tx
	e.emit(first)
	e.lifeExchange = nil
	if e.pending == nil && len(e.replChoices) == 0 {
		e.finishLifeExchange(tx)
	} else {
		// The FIRST side suspended (a replacement body that parked a decision):
		// park the transaction so a later drain emits the second side and
		// settles both. Without this, neither side is referenced again.
		e.pendingLifeExchange = tx
	}
}
