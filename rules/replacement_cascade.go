package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cascadeReplacementEvent builds the synthetic Cascade proposal
// ProposeCascadeReplacement holds out to the replacement matcher. It is never
// logged: the IDs carry the ordered exiled batch the body's ReplacedCards
// selector reads, and Obj is the cascade spell whose trigger is resolving.
func cascadeReplacementEvent(source state.ObjID, controller state.PlayerID, batch []state.ObjID) events.Event {
	return events.Event{Kind: events.Cascade, Obj: source, Player: controller,
		IDs: append([]state.ObjID(nil), batch...)}
}

// applyCascadeReplacements runs the matching Cascade replacement bodies against
// the held instruction proposal. There is no original event to emit (the
// cascade instruction's remainder is the caller's residue), so unlike
// applyReplacement this never folds or logs the proposal. The LAST body gets
// the residue chained after it (via runCascadeReplaceWith) so the bottoming
// and the free-cast election run once, after the body -- or after it resumes,
// when the body suspends at its hidden land pick.
func (e *Engine) applyCascadeReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	residue := e.cascadeResidue
	if residue == nil {
		return ev, false
	}
	var bodies []replMatch
	for _, m := range matches {
		if m.repl.With != nil {
			bodies = append(bodies, m)
		}
	}
	if len(bodies) == 0 {
		return ev, false
	}
	for i, m := range bodies {
		var tail *cards.SA
		if i == len(bodies)-1 {
			tail = residue
		}
		ctx := e.replCtx(m, ev)
		e.runCascadeReplaceWith(ctx, ev.Obj, m.repl.With, tail)
	}
	return ev, true
}

// runCascadeReplaceWith resolves one Cascade replacement body with an optional
// residue SA chained at the end of its SubAbility$ chain. The chain is
// DEEP-COPIED before the tail is attached: the body is shared, immutable
// corpus data, and mutating a shared SA's Sub would corrupt every later use of
// the same script.
func (e *Engine) runCascadeReplaceWith(ctx *effects.Ctx, replaced state.ObjID, with *cards.SA, residue *cards.SA) {
	body := with
	if residue != nil {
		body = cloneChainWithTail(with, residue)
	}
	e.runReplaceWith(ctx, replaced, body, nil)
}

// cloneChainWithTail returns a copy of the SubAbility$ chain rooted at sa with
// tail appended after its last link. Only the SA structs along Sub are copied;
// Params, Line and the compiled fields are shared (the chain is immutable).
func cloneChainWithTail(sa *cards.SA, tail *cards.SA) *cards.SA {
	cp := *sa
	if sa.Sub == nil {
		cp.Sub = tail
	} else {
		cp.Sub = cloneChainWithTail(sa.Sub, tail)
	}
	return &cp
}

// CascadeReplacement implements effects.Host. It holds one synthetic
// events.Cascade proposal out to the ordinary replacement collection and runs
// the matching bodies, returning whether anything matched. e.cascadeResidue
// carries the caller's residue through the synchronous dispatch (the same
// scoped-scratch pattern as e.scrySA/e.scryTarget in Engine.Scry).
func (e *Engine) CascadeReplacement(source state.ObjID, controller state.PlayerID, batch []state.ObjID, residue *cards.SA) bool {
	if e.applyingReplacement {
		return false
	}
	saved := e.cascadeResidue
	e.cascadeResidue = residue
	_, handled := e.applyReplacements(cascadeReplacementEvent(source, controller, batch))
	e.cascadeResidue = saved
	return handled
}
