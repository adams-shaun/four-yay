package effects

// ask_tape_zone.go holds the zone movers' AskTape answer readers and the
// entry helpers they share. An answered site continues locally with the
// answer in hand; the primitive's entry events (an unread-parameter Note, an
// unresolvable selector's Note, a limited search's look) were emitted once on
// the way in and are never re-emitted after the answer.

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tapeAnswerObjs is a served answer's object ids in answer order (the
// "search", "hand_move", "hidden_pick", "sacrifice" and "imprint" asks).
func tapeAnswerObjs(ans []decision.Option) []state.ObjID {
	out := make([]state.ObjID, 0, len(ans))
	for _, o := range ans {
		if o.Obj != 0 {
			out = append(out, o.Obj)
		}
	}
	return out
}

// tapeAnswerYes reads a served yes/no answer the way every confirmation arm
// does: option Kind "yes" accepts, anything else (including an empty answer)
// declines.
func tapeAnswerYes(ans []decision.Option) bool {
	return len(ans) > 0 && ans[0].Kind == "yes"
}

// searchChooseFromDefined resolves a library search's ChooseFromDefined$
// pool (effSearchLibrary's walk): an unresolvable
// selector fails CLOSED with one Note -- an empty pool, never a
// whole-library search.
func searchChooseFromDefined(h Host, c *Ctx, cz *ChangeZoneParams) (pool map[state.ObjID]bool, active, resolved bool) {
	resolved = true
	if raw := cz.ChooseFromDefined; raw != "" {
		active = true
		var ok bool
		if pool, ok = chooseFromDefinedPool(h, c, raw); !ok {
			resolved = false
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "ChangeZone ChooseFromDefined$ " + raw + " is not resolvable; nothing is offered"})
		}
	}
	return pool, active, resolved
}

// searchLook is one search owner's library and MaxRevealed$ look window
// (effSearchLibrary's walk): a window shorter than the
// library is looked at (or, with Reveal$, revealed) before anything is
// chosen from it.
func searchLook(h Host, c *Ctx, cz *ChangeZoneParams, owner state.PlayerID, zones []state.Zone) (lib, lookWindow []state.ObjID) {
	lib = zoneOf(h.Game(), state.ZLibrary, owner)
	if !zoneIn(zones, state.ZLibrary) {
		lib = nil
	}
	lookWindow = searchLibraryWindow(h, c, cz, lib)
	if len(lookWindow) < len(lib) && !cz.NoLooking {
		if cz.Reveal {
			h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: append([]state.ObjID(nil), lookWindow...)})
		} else {
			emitLook(h, []state.PlayerID{searchChooser(h, c, cz, owner)}, state.ZLibrary, lookWindow,
				"looks at the top of the library")
		}
	}
	return lib, lookWindow
}

// hiddenPickOriginNote is effHiddenPick's loud read of an Origin$ naming a
// zone this engine does not model.
func hiddenPickOriginNote(h Host, c *Ctx, originValid bool, from string) {
	if !originValid {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZone Origin$ " + from + " includes a zone this engine does not model (no outside-the-game cards exist); nothing is offered from it"})
	}
}

// hiddenPickChooseFromDefinedNote is effHiddenPick's fail-closed Note for an
// unresolvable ChooseFromDefined$.
func hiddenPickChooseFromDefinedNote(h Host, c *Ctx, cz *ChangeZoneParams) {
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "ChangeZone ChooseFromDefined$ " + cz.ChooseFromDefined + " is not resolvable; nothing is offered"})
}

// tapeAnswerTargets is a served target answer as a target set: a player
// option is a player target, any other option with
// an object an object target. Never nil, so an empty answer stays an answer.
func tapeAnswerTargets(ans []decision.Option) []state.Target {
	out := make([]state.Target, 0, len(ans))
	for _, o := range ans {
		if o.Kind == "player" {
			out = append(out, state.Target{Player: o.Player, IsPlayer: true})
		} else if o.Obj != 0 {
			out = append(out, state.Target{Obj: o.Obj})
		}
	}
	return out
}
