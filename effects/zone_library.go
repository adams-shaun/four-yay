package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// shuffleLibrary applies the default hidden-library shuffle used by searches
// and direct Defined$ fetches: the CR 701.23d shuffle, unless the SA opts out
// (NoShuffle$ True / Shuffle$ False). A hand put-back is different: it
// shuffles only when its own SA explicitly says Shuffle$ True, and uses
// shuffleLibraryExplicit below. ShuffleNonMandatory$ True -- Forge's "Do you
// want to shuffle the library?" confirm, an information-mercy so a player may
// keep the library order a search just taught them -- is NOT read here: this
// helper is the mandatory path, and the confirm belongs to the search's own
// tail, searchShuffleTail below.
func shuffleLibrary(h Host, cz *ChangeZoneParams, owner state.PlayerID) {
	if cz.NoShuffle || cz.ShuffleFalse {
		return
	}
	shuffleLibraryOrder(h, owner)
}

// objectPathShuffleOwed reports whether an object-target ChangeZone that
// moved objects into a library states the explicit Shuffle$ True (the
// graveyard/battlefield/exile "shuffle it into their library" family). The
// object path reads only the explicit flag: the 66 corpus lines that move a
// card into a library with no Shuffle$ parameter are LibraryPosition$ "put
// it on top of your library" movers, which must not shuffle. NoShuffle$
// True is honoured exactly as shuffleLibrary reads it.
func objectPathShuffleOwed(cz *ChangeZoneParams) bool {
	return cz.ShuffleTrue && !cz.NoShuffle
}

// objectPathShuffleTail finishes an object-target ChangeZone that moved
// objects into a library, after the moves landed in effChangeZone. It
// shuffles each distinct card owner's library once, and when the SA also sets
// ShuffleNonMandatory$ it poses Forge's may-shuffle confirm first. Today's
// flag-bearing corpus lines target their controller's own graveyard, so the
// controller is the owner; this is not a per-owner election for future
// multi-owner movers. SP-parented DB carriers reach this tail since task
// spcz1: their own targeting is offered by changeZoneChosenTargets's ask
// (rules/ pins the live path on Put Away and Cathartic Parting).
// The confirm is answered in place via AskTape and the tail completes here;
// an answered confirm returns true, which ends effChangeZone after the tail
// (the AlternativeDecider$ placement does not run). A host that cannot ask
// takes the deterministic decline (R-9), the same stand-in every other
// may-shuffle confirm uses, and returns false.
func objectPathShuffleTail(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, moved []state.ObjID) bool {
	if !cz.ShuffleNonMandatory {
		objectPathShuffleOwners(h, moved)
		return false
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "search_mayshuffle", ResumeSA: sa,
		Prompt: "Shuffle your library?",
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — shuffle", Player: c.Controller},
			{Index: 1, Kind: "no", Label: "No — keep the order", Player: c.Controller},
		}}
	if ans, ok := AskTape(h, d); ok {
		// Answered in place: the answered tail; the caller stops after it.
		if tapeAnswerYes(ans) {
			objectPathShuffleOwners(h, moved)
		}
		return true
	}
	// No-host stand-in (R-9): decline the shuffle, keep the order.
	return false
}

// objectPathShuffleOwners shuffles the library of every distinct owner among
// the moved objects, each once, in first-move order (deterministic; never a
// map range). An object that has already left the game is skipped.
func objectPathShuffleOwners(h Host, moved []state.ObjID) {
	g := h.Game()
	seen := make(map[state.PlayerID]bool, len(moved))
	for _, id := range moved {
		o := g.Obj(id)
		if o == nil || seen[o.Owner] {
			continue
		}
		seen[o.Owner] = true
		shuffleLibraryOrder(h, o.Owner)
	}
}

// searchShuffleTail is a hidden-library search's shuffle-and-place tail, with
// the ShuffleNonMandatory$ read (Path to Exile, Stoneforge Mystic, Squadron
// Hawk, Boggart Harbinger -- 209 exact-Origin$ Library corpus lines carry the
// flag). When the flag is set, even if the search moved no cards, the
// searcher is offered Forge's may-shuffle confirm -- "Shuffle your
// library?" -- instead of the unconditional shuffle: declining keeps the
// library order the search's option list (offered in library order) just
// taught them. The confirm is offered whether or not the search moved a
// card (searchmay1): the fail-to-find shape asks too, because the search's
// mandatory shuffle is exactly what the confirm may spare, and a player who
// failed to find has just as much reason to keep the order they know. This
// is also the tail an object-target ChangeZone into a library calls
// (searchmay1), so a graveyard shuffle-in poses the same confirm.
//
// The confirm is answered in place via AskTape after the moves: "yes" emits
// the same Secret events.Shuffle every library shuffle emits, "no" keeps the
// order, and either way placeLibraryObjects runs after the shuffle point
// exactly as the unconditional path ordered it. A host that cannot ask takes
// the deterministic no-host stand-in (R-9): decline, keep the order -- the
// same stand-in the arrange_mayshuffle confirm falls back to.
func searchShuffleTail(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, owner state.PlayerID, moved []state.ObjID, to state.Zone) {
	if !cz.ShuffleNonMandatory {
		shuffleLibrary(h, cz, owner)
		placeLibraryObjects(h, c, cz, owner, moved, to)
		return
	}
	d := &decision.Decision{Player: owner, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "search_mayshuffle", ResumeSA: sa,
		ResumeTarget: c.Search.Target,
		Prompt:       "Shuffle your library?",
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — shuffle", Player: owner},
			{Index: 1, Kind: "no", Label: "No — keep the order", Player: owner},
		}}
	if ans, ok := AskTape(h, d); ok {
		// Answered in place: the answered tail.
		if tapeAnswerYes(ans) {
			shuffleLibraryOrder(h, owner)
		}
		placeLibraryObjects(h, c, cz, owner, moved, to)
		return
	}
	// No-host stand-in (R-9): decline the shuffle, keep the order.
	placeLibraryObjects(h, c, cz, owner, moved, to)
}

func shuffleLibraryExplicit(h Host, cz *ChangeZoneParams, owner state.PlayerID) {
	if cz.ShuffleTrue && !cz.NoShuffle {
		shuffleLibraryOrder(h, owner)
	}
}

func shuffleLibraryOrder(h Host, owner state.PlayerID) {
	order := h.ShuffleLibrary(owner, h.Game().Zone(state.ZLibrary, owner))
	h.Emit(events.Event{Kind: events.Shuffle, Player: owner, IDs: order, Secret: true})
}

// placeTargetedLibraryObjects implements LibraryPosition$ for the
// object-target path of effChangeZone (golgari_thug1): the one placement the
// targeted movers never reached. Each moved card is placed in ITS OWNER's
// library -- a battlefield creature controlled by another player still
// returns to its owner's library, because the MoveZone keeps its owner (the
// same rule effChangeZoneAll applies) -- and the targets of one owner are
// placed as a block, in target/move order, at the exact position: "0" = top,
// "1" = second from top, "N" = beneath the top N cards, negative = from the
// bottom ("-1" is the bottom). An SVar-resolved value (Quarry Colossus'
// LibraryPosition$ X, read through the ordinary Num grammar) resolves at
// resolution time; a value Num cannot resolve is LOUD -- one Note and the
// MoveZone bottom append stands -- never a guessed placement. An ABSENT
// LibraryPosition$ is Forge's TOP default (golgari_thug2):
// ChangeZoneEffect.changeKnownOriginResolve computes libPos = 0 when the
// parameter is absent, the same default the hand path (handLibraryTail) and
// the ChangeZoneAll path apply, so the absent spelling places at position 0.
func placeTargetedLibraryObjects(h Host, c *Ctx, cz *ChangeZoneParams, moved []state.ObjID) {
	position := int32(0) // Forge's absent-LibraryPosition$ default is TOP
	if raw := cz.LibraryPositionText; raw != "" {
		p, ok := numResolvedText(h, c, cz.LibraryPosition, 0)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "LibraryPosition$ " + raw + " is not implemented; the cards sit at the BOTTOM of their owners' libraries (the MoveZone append)"})
			return
		}
		position = p
	}
	type ownerMoved struct {
		owner state.PlayerID
		ids   []state.ObjID
	}
	var groups []ownerMoved
	for _, id := range moved {
		o := h.Game().Obj(id)
		if o == nil { // a token ceased to exist on leaving the battlefield
			continue
		}
		idx := -1
		for i := range groups {
			if groups[i].owner == o.Owner {
				idx = i
				break
			}
		}
		if idx < 0 {
			groups = append(groups, ownerMoved{owner: o.Owner})
			idx = len(groups) - 1
		}
		groups[idx].ids = append(groups[idx].ids, id)
	}
	for _, grp := range groups {
		if position < 0 && cz.RandomOrder && cz.NoShuffle {
			randomizeLibraryPile(h, grp.ids)
		}
		libraryOrderPlacementAt(h, grp.owner, grp.ids, position)
	}
}

// placeLibraryObjects implements LibraryPosition$ after its source library
// was shuffled. It is shared by a searched subset and a Defined$ fetch list.
// Reorder$ True (Goblin Recruiter's "put those cards on top in any order",
// Brainstorm's put-back) is the marker that the ANSWER order is the
// placement order: the branch below pins the chosen cards on top in exactly
// the order the player's answer carried them (libraryOrderPlacement), never
// a re-sorted one.
func placeLibraryObjects(h Host, c *Ctx, cz *ChangeZoneParams, owner state.PlayerID, moved []state.ObjID, to state.Zone) {
	// An ABSENT LibraryPosition$ is Forge's TOP default on the searched-library
	// path too (agent-20260928T191540Z): Forge computes libPos = 0 when the
	// parameter is absent in BOTH resolvers -- changeKnownOriginResolve
	// (ChangeZoneEffect.java:484) and changeHiddenOriginResolve (:994) -- the
	// same default the object-target and ChangeZoneAll paths apply
	// (golgari_thug2), so this helper treats "" exactly like the explicit "0"
	// in both of its branches. Without it a searched card put back into its
	// library (Knowledge Exploitation, the Kodama's Reach/Cultivate family)
	// stayed at the MoveZone bottom append.
	position := cz.LibraryPositionText
	if position == "" {
		position = "0"
	}
	if cz.Reorder && to == state.ZLibrary {
		if len(moved) > 0 && (position == "0" || position == "-1") {
			libraryOrderPlacement(h, owner, moved, position == "-1")
		}
		return
	}
	if to != state.ZLibrary || len(moved) == 0 {
		return
	}
	if position == "0" || position == "-1" {
		if position[0] == '-' && cz.RandomOrder && cz.NoShuffle {
			randomizeLibraryPile(h, moved)
		}
		libraryOrderPlacement(h, owner, moved, position == "-1")
		return
	}
	// A non-{0,-1} position (Long-Term Plans' "put that card third from the
	// top", LibraryPosition$ 2) resolves through the same NumResolved grammar
	// placeTargetedLibraryObjects applies and places via
	// libraryOrderPlacementAt (positive = zero-based from the top, negative =
	// from the bottom). An unresolvable value is LOUD -- one Note, the
	// MoveZone bottom append stands -- never a guessed placement.
	p, ok := numResolvedText(h, c, cz.LibraryPosition, 0)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "LibraryPosition$ " + position + " is not implemented; the cards sit at the BOTTOM of the library (the MoveZone append)"})
		return
	}
	libraryOrderPlacementAt(h, owner, moved, p)
}

func randomizeLibraryPile(h Host, ids []state.ObjID) {
	for i := len(ids) - 1; i > 0; i-- {
		j := h.Rand(i + 1)
		ids[i], ids[j] = ids[j], ids[i]
	}
}

// libraryOrderPlacement is the one LibraryPosition$ placement both
// hidden-origin movers (the library search's tutor-back and the hand
// put-back) share. The moved cards are already in the library (Move appended
// them at the bottom, in settle order); this one Secret LibraryOrder makes
// position exact: bottom=false puts the chosen cards on TOP in chosen order,
// bottom=true leaves them at the bottom in that same order, with the rest of
// the library beneath/above them respectively. Secret so the full order is
// visible only to the library's owner (redaction rule (1)).
func libraryOrderPlacement(h Host, owner state.PlayerID, moved []state.ObjID, bottom bool) {
	selected := make(map[state.ObjID]bool, len(moved))
	for _, id := range moved {
		selected[id] = true
	}
	lib := h.Game().Zone(state.ZLibrary, owner)
	rest := make([]state.ObjID, 0, len(lib)-len(moved))
	placed := make([]state.ObjID, 0, len(moved))
	for _, id := range moved {
		if containsID(lib, id) {
			placed = append(placed, id)
		}
	}
	for _, id := range lib {
		if !selected[id] {
			rest = append(rest, id)
		}
	}
	order := make([]state.ObjID, 0, len(lib))
	if !bottom {
		order = append(order, placed...)
		order = append(order, rest...)
	} else {
		order = append(order, rest...)
		order = append(order, placed...)
	}
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: owner, IDs: order, Secret: true})
}

// libraryOrderPlacementAt places an already-moved subset at an exact library
// position. Negative positions count from the bottom (-1 is the bottom); a
// positive position is a zero-based offset from the top. AlternativeDecider
// uses position 1 for the corpus's second-from-top vs bottom choices.
func libraryOrderPlacementAt(h Host, owner state.PlayerID, moved []state.ObjID, position int32) {
	selected := make(map[state.ObjID]bool, len(moved))
	for _, id := range moved {
		selected[id] = true
	}
	lib := h.Game().Zone(state.ZLibrary, owner)
	rest := make([]state.ObjID, 0, len(lib)-len(moved))
	placed := make([]state.ObjID, 0, len(moved))
	for _, id := range moved {
		if containsID(lib, id) {
			placed = append(placed, id)
		}
	}
	for _, id := range lib {
		if !selected[id] {
			rest = append(rest, id)
		}
	}
	order := make([]state.ObjID, 0, len(lib))
	if position < 0 {
		order = append(order, rest...)
		order = append(order, placed...)
	} else {
		offset := int(position)
		if offset > len(rest) {
			offset = len(rest)
		}
		order = append(order, rest[:offset]...)
		order = append(order, placed...)
		order = append(order, rest[offset:]...)
	}
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: owner, IDs: order, Secret: true})
}
