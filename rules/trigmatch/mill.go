package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// milledMatches implements Mode$ Milled (trig:Milled) and Mode$ MilledAll
// (trig:MilledAll), the two "whenever ... mills ..." trigger modes (CR
// 701.17a). Both fire on the mill MoveZone event events.Mill emits -- a
// library->graveyard move carrying the mill action marker -- so the mill
// itself (effects' api:Mill, effMill) and the payoff agree on what a mill
// is, the same marker discipline IsDiscard/IsSacrifice use.
//
// The two modes differ only in cadence, which the dispatcher owns, not this
// matcher:
//
//   - Milled fires once PER CARD milled (a single milled card is one event),
//     so the ordinary per-event walk queues it once for each card.
//   - MilledAll fires once for the WHOLE mill action ("whenever one or more
//     cards are milled"). The batch latch lives in checkFaceTriggers
//     (rules/trigger_match.go), keyed on the trigger line inside effMill's
//     open mill batch; this matcher still has to return true for EVERY
//     matching card, because the latch accumulates the count from each
//     accepted event and closeMillBatch patches the total into the queued
//     trigger's TriggerAmount.
//
// Shared parameters, both matched here:
//
//   - ValidPlayer$ names whose mill counts. ev.Player is the player whose
//     library was milled (events.Mill's player), and "you" is the trigger
//     source's controller, so ValidPlayer$ Opponent (Infesting Radroach)
//     matches a mill of an opponent's library. An absent clause matches any
//     player.
//   - ValidCard$ is the milled card's filter (Card.nonLand on all five
//     corpus carriers). It is matched against ev.Obj, the milled card, with
//     the trigger source's controller as the filter perspective.
//
// Corpus population at the current pin, measured with GNU grep over
// .cards/cardsfolder: "Mode$ MilledAll" appears in 3 files (mirelurk_queen,
// the_wise_mothman, screeching_scorchbeast) and "Mode$ Milled " in 2
// (glowing_one, infesting_radroach). Infesting Radroach additionally carries
// TriggerZones$ Graveyard (its trigger lives in the graveyard) with a
// PresentZone$/IsPresent$ intervening-if gate, both handled by the shared
// zoneGate/triggerConditionHolds machinery before this matcher runs.
func milledMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if !events.IsMill(ev) {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return milledMatches(e, t, source, ev)
	}, "Milled", "MilledAll")
	effects.RegisterNonAPI("trig:Milled")
	effects.RegisterNonAPI("trig:MilledAll")
}
