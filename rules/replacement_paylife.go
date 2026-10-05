package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func init() { effects.RegisterNonAPI("repl:PayLife") }

type payLifeProposalDispatcher interface {
	applyReplacementsDispatch(events.Event) (events.Event, bool)
}

// interceptPayLifeProposal turns a marked cost settlement into an unlogged,
// positive-amount proposal before the negative LifeChange reaches the fold.
func prepareEmitReplacement(dispatch payLifeProposalDispatcher, ev *events.Event, applying bool,
	action string, replaced state.ObjID) bool {
	if interceptPayLifeProposal(dispatch, ev) {
		return true
	}
	if applying {
		*ev = events.CarryAction(action, replaced, *ev)
	}
	return false
}

func interceptPayLifeProposal(dispatch payLifeProposalDispatcher, ev *events.Event) bool {
	if ev.Kind != events.LifeChange || ev.Text != pay.PayLifeProposalText || ev.Amount >= 0 {
		return false
	}
	proposal := *ev
	proposal.Amount = -proposal.Amount
	if _, handled := dispatch.applyReplacementsDispatch(proposal); handled {
		return true
	}
	ev.Text = ""
	return false
}

type lifeReplacementMatcher interface {
	Game() *state.Game
	replCtx(replMatch, events.Event) *effects.Ctx
	replacementAmountMatches(string, int32, *effects.Ctx) bool
	replacementConditionHolds(cards.Repl, state.ObjID, state.PlayerID) bool
}

func gainLifeReplacementMatches(e lifeReplacementMatcher, r cards.Repl, source state.ObjID, ev events.Event,
	rememberedPlayers []state.PlayerID, you state.PlayerID) bool {
	if ev.Kind != events.LifeChange || ev.Amount <= 0 {
		return false
	}
	if vp := strings.TrimSpace(r.ParamStr(cards.PKValidPlayer)); vp != "" {
		if vp == "Player.IsRemembered" {
			found := false
			for _, p := range rememberedPlayers {
				if p == ev.Player {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		} else if !effects.MatchesPlayerSpec(e.Game(), vp, ev.Player, you) {
			return false
		}
	}
	return e.replacementConditionHolds(r, source, you)
}

func payLifeReplacementMatches(e lifeReplacementMatcher, r cards.Repl, source state.ObjID, ev events.Event, you state.PlayerID) bool {
	if ev.Kind != events.LifeChange || ev.Text != pay.PayLifeProposalText || ev.Amount <= 0 {
		return false
	}
	if vp, ok := r.Param(cards.PKValidPlayer); ok &&
		!effects.MatchesPlayerSpec(e.Game(), vp, ev.Player, you) {
		return false
	}
	ctx := e.replCtx(replMatch{id: source, repl: &r}, ev)
	return e.replacementAmountMatches(r.ParamStr(cards.PKAmount), ev.Amount, ctx) &&
		e.replacementConditionHolds(r, source, you)
}
