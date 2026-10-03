package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("EndTurn", effEndTurn) }

// effEndTurn implements DB$/SP$/AB$ EndTurn (Forge's EndTurnEffect; 9 corpus
// carriers: Time Stop, Discontinuity, Glorious End, Sundial of the Infinite,
// Day's Undoing, Ultima, Hurkyl's Final Meditation, Obeka Brute Chronologist
// and The Drum, Mining Facility). CR 723.1's "end the turn" is a
// resolution-time turn-structure change, so like DB$ AddPhase it goes through
// events (AGENTS.md's single-mutation-point rule) rather than writing state
// here: one events.EndTurn whose IDs is a snapshot of the whole stack taken
// BEFORE the emit. events.Apply's EndTurn case is the fold -- it exiles every
// named stack object (the resolving spell included, so Time Stop is exiled
// rather than put in the graveyard) and removes every creature/planeswalker
// from combat (CR 723.1a/c). The rules engine observes the EndTurn event,
// clears the triggers still waiting to be placed and jumps the turn straight
// to the cleanup step (CR 723.1d/e -- see Engine.finishEndTurn).
//
// The stack is snapshotted in the effect, not read from the fold, because by
// the time events.Apply runs the resolving object is still on the stack and
// the fold must move exactly the objects the turn's end is replacing:
// resolving after a MoveZone (or after an intervening replacement) would exile
// a different set.
//
// Obeka's Defined$ ActivePlayer | Optional$ True lets the player whose turn
// it is decide, even when another player controls Obeka. A declined election
// leaves the resolving ability to finish normally; an accepted one exiles it.
// ConditionPlayerTurn$ is handled by the shared condition gate; Sundial's
// PlayerTurn$ True is an activation restriction outside this primitive.
func effEndTurn(h Host, c *Ctx, sa *cards.SA) {
	answer := c.EndTurnOpt
	c.EndTurnOpt = "" // a chained EndTurn must pose its own election
	g := h.Game()
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		if answer == "" {
			chooser := c.Controller
			if strings.TrimSpace(sa.ParamStr(cards.PKDefined)) == "ActivePlayer" {
				chooser = g.Active
			}
			d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "endturn_optional", ResumeSA: sa,
				ResumeRemembered: append([]state.Target(nil), c.Remembered...),
				Prompt:           "End the turn?", Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes", Player: chooser},
					{Index: 1, Kind: "no", Label: "No", Player: chooser},
				}}
			ans, ok := AskTape(h, d)
			if !ok {
				// Without an askable host, take the conservative R-9 decline.
				if Ask(h, d) == AskNoHost {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "EndTurn Optional$ declined (no engine host to ask)"})
				}
				return
			}
			// The resolution kernel's answer in hand: the
			// "endturn_optional" arm's yes/no, consumed below as the
			// re-entry consumes Ctx.EndTurnOpt.
			answer = "no"
			if len(ans) > 0 && ans[0].Kind == "yes" {
				answer = "yes"
			}
		}
		if answer != "yes" {
			return
		}
	}
	ids := append([]state.ObjID(nil), g.Stack...)
	h.Emit(events.Event{Kind: events.EndTurn, IDs: ids})
}
