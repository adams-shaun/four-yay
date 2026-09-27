package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Clash", effClash)
	// The trigger half of the mechanic, registered here beside the marker
	// emitting primitive (the effects/gift.go shape): effects.Supported()
	// reports trig:Clashed so the coverage census counts the four corpus
	// carriers as playable.
	RegisterNonAPI("trig:Clashed")
}

// effClash implements DB$/SP$/AB$ Clash (CR 701.31, Forge's ClashEffect; 29
// corpus SA lines across 29 files). The resolving ability's controller clashes
// with an opponent:
//
//  1. Each clashing player reveals the top card of their library (a public
//     ids-Note each, the same non-Secret reveal encoding the Dig window and
//     effReveal emit).
//  2. Each revealed card's owner chooses TOP or BOTTOM. The choice is an
//     owner-routed KChoose; the deterministic no-host fallback is BOTTOM.
//     A changed order is one Secret events.LibraryOrder for the whole library.
//  3. The player whose card had the strictly higher mana value wins; equal
//     mana values (including two empty libraries, both read as -1) leave NO
//     winner (CR 701.31, Forge's ClashEffect).
//  4. Forge's WinSubAbility$ runs when the resolving controller won,
//     OtherwiseSubAbility$ otherwise. Both names are resolved through the
//     source's SVar table and run as ordinary sub-abilities (the FlipCoin
//     WinSubAbility$/LoseSubAbility$ mechanism); the base SA's own
//     SubAbility$ chain is the enclosing Resolve loop's ordinary walk.
//  5. One events.Clash marker per clashing player records that player's
//     Won$ orientation (Amount 1 = won, 0 = lost or tied), which is what
//     trig:Clashed's ValidPlayer$/Won$ gate reads. Forge fires the Clashed
//     trigger once per player for exactly this reason.
//  6. RememberClasher$ True remembers the opponent(s) as player targets, so a
//     chained Defined$ Remembered names them (Captivating Glance's
//     OppControl).
//
// The clash opponent is, in order: the players a Defined$ names
// (Marvo's `Defined$ TriggeredDefendingPlayer`), else the player targets the
// ability chose (Pulling Teeth's `ValidTgts$ Player`), else the resolving
// controller's first living opponent -- Forge's own "choose an opponent"
// fallback, made deterministic rather than random. The resolving controller
// is always the first clashing player.
//
// Placement choices resume from the saved reveal and winner snapshot, so an
// answer never repeats a reveal, comparison, earlier placement, marker, or branch.
func effClash(h Host, c *Ctx, sa *cards.SA) {
	// Consume the answered snapshot before nested effects can run another Clash.
	continuation, top := c.ClashContinuation, c.ClashTop
	c.ClashContinuation, c.ClashTop = nil, false

	var players []state.PlayerID
	var revealed []state.ObjID
	winnerIdx, cursor := -1, 0
	if continuation != nil {
		r := continuation
		players = append([]state.PlayerID(nil), r.Players...)
		revealed = append([]state.ObjID(nil), r.Revealed...)
		winnerIdx, cursor = r.Winner, r.Cursor
		if cursor < len(players) && revealed[cursor] != 0 {
			if top {
				clashMoveToTop(h, players[cursor], revealed[cursor])
			} else {
				clashMoveToBottom(h, players[cursor], revealed[cursor])
			}
		}
		cursor++
	} else {
		players = clashParticipants(h, c, sa)
		if len(players) == 0 {
			return
		}
		revealed = make([]state.ObjID, len(players))
		cmc := make([]int, len(players))
		for i, p := range players {
			cmc[i] = -1
			lib := zoneOf(h.Game(), state.ZLibrary, p)
			if len(lib) == 0 {
				continue
			}
			revealed[i], cmc[i] = lib[0], manaValueOf(h.Game(), lib[0])
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: []state.ObjID{lib[0]}})
		}
		for i := range cmc {
			best := cmc[i] >= 0
			for j := range cmc {
				if j != i && cmc[j] >= cmc[i] {
					best = false
					break
				}
			}
			if best {
				winnerIdx = i
				break
			}
		}
	}
	c.ClashWinner, c.ClashWon = players[0], winnerIdx == 0
	if winnerIdx >= 0 {
		c.ClashWinner = players[winnerIdx]
	}
	for ; cursor < len(players); cursor++ {
		p, id := players[cursor], revealed[cursor]
		if id == 0 {
			continue
		}
		d := ClashPlacementDecision(p, c.Source, sa, players, revealed, winnerIdx, cursor, id)
		if Ask(h, d) == AskAsked {
			return
		}
		h.Emit(events.Event{Kind: events.Note, Player: p, Text: "Clash placement: no decision host; put revealed card on bottom"})
		clashMoveToBottom(h, p, id)
	}

	// The win/lose markers FIRST, so a WinSubAbility$/OtherwiseSubAbility$
	// that suspends on a mid-resolution ask cannot suppress them (the queued
	// triggers resolve after this ability either way).
	for i, p := range players {
		won := winnerIdx >= 0 && i == winnerIdx
		var amount int32
		if won {
			amount = 1
		}
		h.Emit(events.Event{Kind: events.Clash, Obj: c.Source, Player: p, Amount: amount})
	}

	// RememberClasher$ True: the opposing clashing players (every participant
	// but the resolving controller) join Remembered as player targets.
	if strings.EqualFold(strings.TrimSpace(sa.Params["RememberClasher"]), "True") {
		for _, p := range players {
			if p == c.Controller {
				continue
			}
			c.Remembered = append(c.Remembered, state.Target{Player: p, IsPlayer: true})
		}
	}

	// Forge's outcome branch, resolved by name like FlipCoin's.
	name := strings.TrimSpace(sa.Params["OtherwiseSubAbility"])
	if c.ClashWon {
		name = strings.TrimSpace(sa.Params["WinSubAbility"])
	}
	if name != "" && c.SVars != nil {
		Resolve(h, c, cards.ResolveSVar(c.SVars, name))
	}
}

// clashParticipants is the deterministic clashing-player list: the resolving
// controller first, then (in order, deduped, living only) the Defined$-named
// players, the ability's chosen player targets, or the controller's first
// living opponent. The controller is always included, matching Forge's
// ClashEffect -- the controller is one of the two clashing players and is the
// one the WinSubAbility$ branch is keyed on.
func clashParticipants(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	g := h.Game()
	out := []state.PlayerID{c.Controller}
	seen := map[state.PlayerID]bool{c.Controller: true}
	add := func(p state.PlayerID) {
		if int(p) >= len(g.Players) || seen[p] || g.Players[p].Lost {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	if strings.TrimSpace(sa.Params["Defined"]) != "" {
		for _, p := range definedPlayers(h, c, sa) {
			add(p)
		}
		if len(out) > 1 {
			return out
		}
		// A present-but-unresolvable Defined$ (an attack trigger whose
		// TriggeredDefendingPlayer role the engine could not capture) must
		// still clash with an opponent: fall through to Forge's own "choose
		// an opponent" fallback rather than clashing with nobody.
	}
	if _, targeted := sa.Params["ValidTgts"]; targeted {
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				add(t.Player)
			}
		}
		if len(out) > 1 {
			return out
		}
	}
	for _, p := range g.AliveFrom(0) {
		if p != c.Controller {
			add(p)
			break
		}
	}
	return out
}

// ClashPlacementDecision builds the owner-routed, always-legal two-way choice
// used by api:Clash. Keeping construction here lets botpolicy validate the
// exact option shape rather than maintaining a synthetic parallel decision.
func ClashPlacementDecision(p state.PlayerID, source state.ObjID, sa *cards.SA, players []state.PlayerID, revealed []state.ObjID, winner, cursor int, id state.ObjID) *decision.Decision {
	return &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1, Source: source,
		ResumeKind: "clash_placement", ResumeSA: sa,
		ResumeClash: &decision.ClashResume{Players: append([]state.PlayerID(nil), players...), Revealed: append([]state.ObjID(nil), revealed...), Winner: winner, Cursor: cursor},
		Prompt:      "Put the revealed card on top or bottom of your library",
		Options:     []decision.Option{{Index: 0, Kind: "bottom", Label: "Put it on the bottom", Obj: id, Player: p}, {Index: 1, Kind: "top", Label: "Keep it on top", Obj: id, Player: p}}}
}

// clashMoveToBottom puts id on the BOTTOM of player p's library as one Secret
// events.LibraryOrder carrying the whole reordered library -- the same
// single-event record Dig's moveRestToBottom emits, so a replay re-derives
// the same order. The emit is skipped when the card already sits on the
// bottom (a one-card library), which leaves the log byte-identical to the
// no-op it is.
func clashMoveToTop(h Host, p state.PlayerID, id state.ObjID) {
	lib := zoneOf(h.Game(), state.ZLibrary, p)
	if len(lib) == 0 || lib[0] == id {
		return
	}
	newLib := append([]state.ObjID(nil), lib...)
	idx := -1
	for i, card := range newLib {
		if card == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	copy(newLib[1:idx+1], newLib[:idx])
	newLib[0] = id
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: newLib, Secret: true})
}

func clashMoveToBottom(h Host, p state.PlayerID, id state.ObjID) {
	g := h.Game()
	lib := zoneOf(g, state.ZLibrary, p)
	idx := -1
	for i, card := range lib {
		if card == id {
			idx = i
			break
		}
	}
	if idx < 0 || idx == len(lib)-1 {
		return
	}
	newLib := make([]state.ObjID, 0, len(lib))
	newLib = append(newLib, lib[:idx]...)
	newLib = append(newLib, lib[idx+1:]...)
	newLib = append(newLib, id)
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: newLib, Secret: true})
}
