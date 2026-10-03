package rules

import (
	"github.com/adams-shaun/gorge/decision"
)

// actingView is the decision and intent Submit's engine-side validators and
// handlers see. A decision a CR 722 redirect moved to a controller
// (controlPlayerRedirect, searchControlRedirect) is ANSWERED by that
// controller -- Decision.Validate and the log keep the answering seat -- but
// it was built for, and acts for, the seat it was asked OF (Decision.Acting):
// the controlled player's priority options are that player's lands, mana
// abilities and hand (askPriority's legalActions(p)), its attackers are that
// player's creatures, its search is that player's library. Every handler
// reads d.Player or in.Player as the player taking the action, so the view
// re-seats both on the acting seat; an unredirected decision is returned
// as is, at no cost.
//
// The one action that stays the answering seat's is a concession: CR 722.5
// says a player who controls another can't make that player concede, while
// any player may concede at any time. A redirected priority's concede option
// therefore concedes the controller who chose it, exactly as it did before
// the acting view existed.
func actingView(d *decision.Decision, in decision.Intent) (*decision.Decision, decision.Intent) {
	if !d.Redirected || d.Actor == d.Player {
		return d, in
	}
	if d.Kind == decision.KPriority && len(in.Choices) > 0 {
		if c := in.Choices[0]; c >= 0 && c < len(d.Options) && d.Options[c].Kind == "concede" {
			return d, in
		}
	}
	ad := *d
	ad.Player = d.Actor
	in.Player = d.Actor
	return &ad, in
}
