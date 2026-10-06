package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ActionReplaced runs an unlogged synthetic proposal before an action begins.
// The proposal's completed event, if any, is emitted later through the normal
// event path and is not itself a replacement window.
func (e *Engine) ActionReplaced(proposal events.Event) bool {
	if e.applyingReplacement {
		return false
	}
	if proposal.Kind == events.Connive {
		o := e.G.Obj(proposal.Obj)
		if o == nil {
			return false
		}
		proposal.Player = o.Controller
		proposal.Amount = -1
	}
	_, handled := e.applyReplacements(proposal)
	return handled
}

// continueActionReplacements dispatches a synthetic pre-action proposal
// (ActionReplaced) that met at least one replacement: Explore keeps its own
// continuation; a Connive proposal resolves the first match's body.
func continueActionReplacements(e interface {
	continueExploreReplacements(events.Event, []replMatch) (events.Event, bool)
	replCtx(replMatch, events.Event) *effects.Ctx
	runReplaceWith(*effects.Ctx, state.ObjID, *cards.SA, *events.Event)
}, ev events.Event, matches []replMatch) (events.Event, bool) {
	if ev.Kind == events.Explore {
		return e.continueExploreReplacements(ev, matches)
	}
	return continueConniveReplacements(e, ev, matches)
}

func continueConniveReplacements(e interface {
	replCtx(replMatch, events.Event) *effects.Ctx
	runReplaceWith(*effects.Ctx, state.ObjID, *cards.SA, *events.Event)
}, ev events.Event, matches []replMatch) (events.Event, bool) {
	if ev.Amount >= 0 || len(matches) == 0 {
		return ev, false
	}
	m := matches[0]
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	return ev, true
}

func replacementMatchesConnive(e interface {
	matchesSpecFrom(string, state.ObjID, state.PlayerID, state.ObjID) bool
	replacementConditionHolds(cards.Repl, state.ObjID, state.PlayerID) bool
}, r cards.Repl, source state.ObjID, ev events.Event, you state.PlayerID) bool {
	if ev.Kind != events.Connive || ev.Amount >= 0 {
		return false
	}
	if v, ok := r.Param(cards.PKValidCard); ok && !e.matchesSpecFrom(v, ev.Obj, you, source) {
		return false
	}
	return e.replacementConditionHolds(r, source, you)
}

var _ effects.HostReplacements = (*Engine)(nil)
