package effects

// ask_tape_zone.go holds the zone movers' share of the resolution kernel's
// converted asks (W3 step 2, lasagna spec §7): the answer readers and the
// re-entry echoes. A converted site continues locally with the answer in
// hand, but the legacy path it must stay byte-identical with re-enters the
// primitive from its first line, re-emitting whatever that entry emits (an
// unread-parameter Note, an unresolvable selector's Note, a limited search's
// look) before it reaches the answered walk. Each echo replays exactly that
// entry, through the same helpers the entry itself calls, so the two paths
// cannot drift.

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tapeAnswerObjs is a served answer's object ids in answer order: the list
// every Obj-carrying resume arm ("search", "hand_move", "hidden_pick",
// "sacrifice", "imprint") binds.
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

// changeZoneReentryEcho is a legacy re-entry of effChangeZone up to its
// dispatch on Origin$ (changeZonePrelude); the dispatch itself emits nothing
// on the way to an answered walker.
func changeZoneReentryEcho(h Host, c *Ctx, cz *ChangeZoneParams) {
	changeZonePrelude(h, c, cz)
}

// searchReentryEcho is a legacy re-entry of effChangeZone into
// effSearchLibrary's answered owner: the prelude, the ChooseFromDefined$
// Note, and -- for an answered pick or may-shuffle, not for a confirmation,
// which is asked before the look -- this owner's limited-search look.
func searchReentryEcho(h Host, c *Ctx, cz *ChangeZoneParams, owner state.PlayerID, zones []state.Zone, look bool) {
	changeZoneReentryEcho(h, c, cz)
	searchChooseFromDefined(h, c, cz)
	if look {
		searchLook(h, c, cz, owner, zones)
	}
}

// definedLibraryReentryEcho is a legacy re-entry of effChangeZone into
// moveDefinedLibraryObjects' answered Optional$ election.
func definedLibraryReentryEcho(h Host, c *Ctx, cz *ChangeZoneParams) {
	changeZoneReentryEcho(h, c, cz)
}

// searchChooseFromDefined resolves a library search's ChooseFromDefined$
// pool (effSearchLibrary's walk and its re-entry echo): an unresolvable
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
// (effSearchLibrary's walk and its re-entry echo): a window shorter than the
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
			emitLook(h, []state.PlayerID{searchChooser(h, c, cz)}, state.ZLibrary, lookWindow,
				"looks at the top of the library")
		}
	}
	return lib, lookWindow
}

// libraryOnlyZones is the origin set a library search's may-shuffle tail
// re-enters with as far as its look is concerned: the tail is reached only
// when the search's origin includes the library (read-only).
var libraryOnlyZones = []state.Zone{state.ZLibrary}

// objectPathReentryEcho is a legacy re-entry of effChangeZone's object path
// up to its answered may-shuffle tail: the prelude, then the Hand-origin
// DefinedPlayer$-unread Note the Origin$ dispatch emits on the way through.
func objectPathReentryEcho(h Host, c *Ctx, cz *ChangeZoneParams) {
	changeZoneReentryEcho(h, c, cz)
	if cz.OriginPresent {
		changeZoneDefinedPlayerNote(h, c, cz, cz.Origin, cz.OriginAll)
	}
}

// objectPathMoveEcho is a legacy re-entry of effChangeZone's object path up
// to its answered AlternativeDecider$ or Imprint$ choice: objectPathReentryEcho,
// then the WithCountersAmount$ read the path makes before resolving its
// objects (loud when malformed). The rest of the way -- the objects'
// resolution and the forget-other clears the first pass already applied --
// emits nothing on a re-entry.
func objectPathMoveEcho(h Host, c *Ctx, cz *ChangeZoneParams, to state.Zone) {
	objectPathReentryEcho(h, c, cz)
	if cz.WithCountersType != "" && counterDestination(to) {
		withCounterAmount(h, c, cz)
	}
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

// hiddenPickReentryEcho is a legacy re-entry of effChangeZone into
// effHiddenPick's answered fetch player: the prelude, then the pick's own
// entry reads in their order -- the unmodelled-Origin$ Note, the
// WithCountersAmount$ read, the ChooseFromDefined$ Note.
func hiddenPickReentryEcho(h Host, c *Ctx, cz *ChangeZoneParams, to state.Zone, originValid bool, from string) {
	changeZoneReentryEcho(h, c, cz)
	hiddenPickOriginNote(h, c, originValid, from)
	if cz.WithCountersType != "" && counterDestination(to) {
		withCounterAmount(h, c, cz)
	}
	if raw := cz.ChooseFromDefined; raw != "" {
		if _, ok := chooseFromDefinedPool(h, c, raw); !ok {
			hiddenPickChooseFromDefinedNote(h, c, cz)
		}
	}
}

// handMoveReentryEcho is a legacy re-entry of effChangeZone into
// handMoveOwnersWalk's answered owner: the prelude, then the walk's
// WithCountersAmount$ read (loud when malformed). The hand dispatchers emit
// only on shapes that never reach an ask.
func handMoveReentryEcho(h Host, c *Ctx, cz *ChangeZoneParams, to state.Zone) {
	changeZoneReentryEcho(h, c, cz)
	if cz.WithCountersType != "" && counterDestination(to) {
		withCounterAmount(h, c, cz)
	}
}

// tapeAnswerTargets is a served target answer as the "tgts" and "choice"
// arms bind it: a player option is a player target, any other option with
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
