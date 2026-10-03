// miracle.go implements the Miracle keyword (CR 702.94), Task 18.
//
// Miracle is not a triggered ability in the T: sense -- it has no trigger
// line to fire -- but it shares the trigger machinery's queue: when a card
// with Miracle is the FIRST draw of its controller's turn, the draw is what
// offers it, and the controller chooses whether to cast it for the miracle
// cost. The draw still resolves as a normal draw (the card is already in
// hand); what Miracle adds is a yes/no offer layered on top.
//
// The offer is queued as a pendingTrigger with Miracle set and no Idx/SA (no
// T: line), so the drain (putTriggersOnStack, trigger_queue.go) treats it as
// an optional trigger whose decider is the owner: optionalDecider and
// triggerLabel special-case Miracle, askTriggerOptional asks the yes/no, and
// a yes routes pushTrigger into castMiracle below. A no drops the offer
// silently -- declining costs nothing and changes nothing but the offer.
package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// offerMiracle is called from checkTriggers (trigger_match.go) after a Draw
// event. It queues a miracle offer for the drawn card iff the card carries
// the Miracle keyword and this is the first draw of its controller's turn.
//
// "First draw of the turn" is derived from the log (drawsThisTurn, below) --
// never from a field that only lives in memory -- so a log-only replay
// reaches exactly the same conclusion. The card is owned by ev.Player (the
// draw moved it from that player's library to their hand, so the object's
// Controller at this instant is also that player; ev.Player is the source of
// truth the brief's interface names).
//
// The offer is queued as a trivial Miracle pendingTrigger. Idx/SA are zero/
// nil (there is no T: line); the drain and decider read the Miracle flag
// instead. The card at this point is guaranteed to be in the owner's hand
// (the draw just put it there); castMiracle re-verifies before it casts, in
// case the offer is still in the queue when something else moves the card.
func (e *Engine) offerMiracle(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil {
		return
	}
	if _, ok := o.Face().KeywordParam("Miracle"); !ok {
		return
	}
	if e.drawsThisTurn(ev.Player) != 1 {
		return // not the first draw of the turn: no miracle.
	}
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source:     ev.Obj,
		Controller: o.Controller,
		Miracle:    true,
		Ctx:        effects.NewCtx(ev.Obj, o.Controller, effects.CtxInit{}),
	})
}

// drawsThisTurn counts Draw events for player p since the last TurnChange in
// the log (or since the start of the log, on turn 1). It is the "first card
// drawn this turn" oracle for offerMiracle's ==1 gate, and mirrors
// spellsCastThisTurn (cast.go) exactly -- a log walk, so a replay derives
// the same number, and the opening-hand deal (which is logged before the
// first TurnChange) is naturally invisible to it.
func (e *Engine) drawsThisTurn(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// castMiracle is called from pushTrigger (trigger_queue.go) when the owner
// answered the miracle offer yes. It is the "place" step for a Miracle
// pendingTrigger, the way pushTrigger otherwise mints a T: line's stack
// object.
//
// The card must still be in the owner's hand -- the offer was queued at draw
// time, and something (another effect, a state-based action, the owner being
// eliminated) may have moved it since; if it is gone the offer is dropped
// silently. Otherwise it reveals the card (a Note carries the reveal so a
// replay observes it) and enters the ordinary cast flow with Mode "miracle",
// which makes cast.go charge the printed miracle cost (KeywordParam Miracle)
// and sets FlagMiracle on the spell.
//
// Because this runs inside the trigger drain, the same continuation Task 7
// introduced for a trigger asking its targets applies to whatever decision
// the cast flow pauses on: the drain stays interrupted while castMiracle's
// own X/target ask is outstanding, and the handler that answers it
// (handleChoose's cast case for X, handleTarget for a target) is what
// resumes the drain (resumeTriggerDrain), rather than a fresh priorityRound
// stealing a second attempt at state-based actions. drainAwaitsTarget is the
// shared flag: true exactly when the cast flow left a pending decision whose
// answer must resume the drain, false when the cast committed inline (then
// the handleTriggerOptional that called us picks the drain back up).
func (e *Engine) castMiracle(pt pendingTrigger) {
	o := e.G.Obj(pt.Source)
	if o == nil || o.Controller != pt.Controller || o.Zone != state.ZHand {
		return
	}
	name := "it"
	if f := o.Face(); f != nil && f.Name != "" {
		name = f.Name
	}
	e.emit(events.Event{Kind: events.Note, Player: pt.Controller, IDs: []state.ObjID{pt.Source},
		Text: "reveals " + name + " (miracle)"})
	e.drainAwaitsTarget = false
	e.beginCast(pt.Controller, decision.Option{Kind: "cast", Obj: pt.Source, Mode: "miracle"})
	e.drainAwaitsTarget = e.pending != nil
}

func init() {
	effects.RegisterNonAPI("kw:Miracle")
}
