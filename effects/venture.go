package effects

import (
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Venture", effVenture) }

// effVenture implements DB$/SP$/AB$ Venture (CR 701.49, "Venture into the
// Dungeon"; 37 corpus carriers, every one a plain `DB$ Venture`, 12 of them
// with `Defined$ You`). Forge represents each dungeon as a token script
// (`.cards/tokenscripts/*.txt`: `Types:Dungeon`, one `K:Dungeon:<room>,...`
// keyword naming the room order, and one SVar per room carrying that room's
// `RoomName$` and `NextRoom$ A,B` arrows), stored in Game.Tokens by file
// stem like every token script.
//
// CR 701.49a: venturing with no dungeon owned puts a dungeon card into the
// command zone and the marker on its topmost room. The player CHOOSES the
// dungeon (KChoose, options Kind "dungeon", one per dungeon token script
// with the card's printed name as Label and the token-script key as the
// server-side Option.Key the engine reads back). Plain venture (no
// Dungeon$ parameter) never offers Undercity: the Undercity dungeon card's
// own printed rule is "You can't enter this dungeon unless you 'venture
// into Undercity.'" (CR 701.49d's variant form). Dungeon$ <quality> is the
// explicit "venture into [quality]" form (701.49d) the initiative (CR 726.2)
// uses: candidates are narrowed to dungeon token scripts whose type line
// carries the named subtype -- `Dungeon$ Undercity` picks the Undercity
// without asking (it is the only dungeon of that quality), and the same
// parameter also accepts an exact token-script key or dungeon name.
//
// CR 701.49b: venturing while the marker is on any non-bottommost room moves
// it to one of the current room's NextRoom$ rooms. One option per arrow in
// the script's printed order (KChoose, options Kind "room", Label the room's
// RoomName$); a two-arrow room asks the venturing seat which to follow, a
// one-arrow room moves without asking. The marker move is one DungeonRoom
// event, so replay is byte-identical and the per-seat view (which projects
// the dungeon and marker, slice 1) updates publicly.
//
// CR 701.49c (venturing FROM the bottommost room completes the dungeon and
// begins a new one) is the dungeon chain's slice 3, together with the room
// abilities that trigger on entering a room; until it lands this primitive
// says so with one loud Note and does nothing, rather than silently moving
// nothing (the same loud-degradation contract AddPhase's unresolvable phase
// values take).
//
// Players: actingPlayers -- the shared "Defined$ else ValidTgts$-targeted
// else the resolving controller" selector. All 37 carriers are single-player
// (`Defined$ You` or absent), and the walk is cursor-based (Ctx.VentureIdx)
// so a future multi-Defined carrier asks each player in order across the
// suspensions.
//
// A host that cannot ask (R-9) takes the deterministic stand-in instead of
// wedging: the FIRST printed candidate (dungeons in token-key sort order,
// rooms in NextRoom$ printed order), with one loud Note naming the
// degradation.
func effVenture(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	players := actingPlayers(h, c, sa)
	idx := 0
	if c.VentureEnter != "" || c.VentureRoom != "" {
		// An answered ask rides back with the cursor it was posed for
		// (fx42 scoping): apply it, clear the markers, and resume the walk
		// after that player.
		idx = int(c.VentureIdx)
		key, room := c.VentureEnter, c.VentureRoom
		c.VentureEnter, c.VentureRoom, c.VentureIdx = "", "", 0
		if idx < 0 || idx >= len(players) {
			return
		}
		p := players[idx]
		if key != "" {
			ventureEnter(h, g, p, key)
		} else if room != "" {
			h.Emit(events.Event{Kind: events.DungeonRoom, Player: p, Obj: g.Players[p].DungeonObj, Text: room})
		}
		idx++
	}
	quality := strings.TrimSpace(sa.Params["Dungeon"])
	for i := idx; i < len(players); i++ {
		p := players[i]
		if p < 0 || int(p) >= len(g.Players) {
			continue
		}
		if id := g.Players[p].DungeonObj; id != 0 {
			ventureAdvance(h, g, c, sa, p, i, id)
		} else {
			if ventureChoose(h, g, c, sa, p, i, quality) {
				return
			}
		}
	}
}

// ventureAdvance moves p's marker one room (CR 701.49b). It reports whether
// the walk suspended (an ask was posted) so the caller can return.
func ventureAdvance(h Host, g *state.Game, c *Ctx, sa *cards.SA, p state.PlayerID, i int, id state.ObjID) bool {
	dungeon := g.Obj(id)
	if dungeon == nil || dungeon.Face() == nil {
		return false
	}
	room := g.Players[p].DungeonRoom
	if room == "" {
		// A dungeon with no marker room yet: CR 309.4a places the marker on
		// the topmost room as the dungeon enters, which ventureEnter already
		// did. An externally created dungeon (hand-built events) with no
		// marker resumes from the printed first room.
		rooms := dungeonRooms(dungeon.Face())
		if len(rooms) == 0 {
			return false
		}
		h.Emit(events.Event{Kind: events.DungeonRoom, Player: p, Obj: id, Text: rooms[0]})
		return false
	}
	nexts := ventureNextRooms(dungeon.Face(), room)
	switch {
	case len(nexts) == 0:
		// CR 701.49c: the marker is on the bottommost room; venturing from it
		// completes the dungeon and begins a new one. Dungeon completion is
		// the dungeon chain's slice 3 (with the room abilities that trigger
		// on entering a room); say so loudly and move nothing.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Venture: " + dungeon.Face().Name + "'s marker is on its bottommost room (" + room + "); dungeon completion is not yet implemented"})
		return false
	case len(nexts) == 1:
		h.Emit(events.Event{Kind: events.DungeonRoom, Player: p, Obj: id, Text: nexts[0]})
		return false
	}
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "venture_room", ResumeSA: sa, ResumeTarget: i,
		Prompt: "Choose the next room"}
	for j, nk := range nexts {
		d.Options = append(d.Options, decision.Option{Index: j, Kind: "room",
			Label: ventureRoomLabel(dungeon.Face(), nk), Key: nk, Player: p})
	}
	switch Ask(h, d) {
	case AskAsked:
		c.VentureIdx = int32(i)
		return true
	default:
		// R-9 (AskNoHost; AskEmpty cannot happen over non-empty options):
		// follow the first printed arrow, loudly.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Venture: room choice degraded to " + ventureRoomLabel(dungeon.Face(), nexts[0]) + " (no engine host to ask)"})
		h.Emit(events.Event{Kind: events.DungeonRoom, Player: p, Obj: id, Text: nexts[0]})
		return false
	}
}

// ventureChoose runs CR 701.49a's dungeon choice for p and reports whether
// the walk suspended.
func ventureChoose(h Host, g *state.Game, c *Ctx, sa *cards.SA, p state.PlayerID, i int, quality string) bool {
	candidates := dungeonCandidates(g, quality)
	if len(candidates) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Venture: no dungeon card" + ventureQualityText(quality) + " is available"})
		return false
	}
	if len(candidates) == 1 {
		// CR 701.49d: `venture into Undercity` -- the only dungeon of the
		// named quality -- is entered without asking.
		ventureEnter(h, g, p, candidates[0])
		return false
	}
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "venture_dungeon", ResumeSA: sa, ResumeTarget: i,
		Prompt: "Choose a dungeon"}
	for j, key := range candidates {
		name := key
		if def := g.Tokens[key]; def != nil && len(def.Faces) > 0 && def.Faces[0].Name != "" {
			name = def.Faces[0].Name
		}
		d.Options = append(d.Options, decision.Option{Index: j, Kind: "dungeon", Label: name, Key: key, Player: p})
	}
	switch Ask(h, d) {
	case AskAsked:
		c.VentureIdx = int32(i)
		return true
	default:
		// R-9 (AskNoHost): enter the first sorted candidate, loudly.
		name := candidates[0]
		if def := g.Tokens[name]; def != nil && len(def.Faces) > 0 && def.Faces[0].Name != "" {
			name = def.Faces[0].Name
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Venture: dungeon choice degraded to " + name + " (no engine host to ask)"})
		ventureEnter(h, g, p, candidates[0])
		return false
	}
}

// ventureEnter puts the named dungeon into p's command zone and the marker
// on its topmost room (CR 309.4a, CR 701.49a): one DungeonCreate then one
// DungeonRoom event, so the room ability slice can trigger off the marker's
// entry into the room exactly as it triggers off every later advance.
func ventureEnter(h Host, g *state.Game, p state.PlayerID, key string) {
	h.Emit(events.Event{Kind: events.DungeonCreate, Player: p, Text: key})
	id := g.Players[p].DungeonObj
	if id == 0 {
		return
	}
	if def := g.Tokens[key]; def != nil && len(def.Faces) > 0 {
		if rooms := dungeonRooms(def.Faces[0]); len(rooms) > 0 {
			h.Emit(events.Event{Kind: events.DungeonRoom, Player: p, Obj: id, Text: rooms[0]})
		}
	}
}

// dungeonCandidates lists the dungeon token scripts the game can offer a
// venturing player, in token-key sort order (the deterministic order the
// choice and its R-9 stand-in both read). quality "" is plain venture
// (CR 701.49a): dungeons of the Undercity quality are excluded, per the
// Undercity card's own printed "You can't enter this dungeon unless you
// 'venture into Undercity.'" rule. A named quality (CR 701.49d) narrows the
// list to dungeon scripts whose type line carries the named subtype; the
// exact token-script key or dungeon name is also accepted, so an explicit
// caller can name the card it wants.
func dungeonCandidates(g *state.Game, quality string) []string {
	var keys []string
	for key, def := range g.Tokens {
		if def == nil || len(def.Faces) == 0 {
			continue
		}
		f := def.Faces[0]
		if !slices.Contains(f.Types, "Dungeon") {
			continue
		}
		if quality == "" {
			if slices.Contains(f.Types, "Undercity") {
				continue
			}
		} else if !ventureQualityMatches(f, key, quality) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ventureQualityMatches reports whether a dungeon face carries the named
// venture-into quality: a printed subtype ("Undercity"), the token-script
// key, or the dungeon's printed name.
func ventureQualityMatches(f *cards.Face, key, quality string) bool {
	if slices.Contains(f.Types, quality) {
		return true
	}
	return strings.EqualFold(key, quality) || strings.EqualFold(f.Name, quality)
}

func ventureQualityText(quality string) string {
	if quality == "" {
		return ""
	}
	return " of the " + quality + " quality"
}

// dungeonRooms reads a dungeon face's printed room order from its
// K:Dungeon:<room>,... keyword. A dungeon without the keyword has no rooms
// to enter.
func dungeonRooms(f *cards.Face) []string {
	list, ok := f.KeywordParam("Dungeon")
	if !ok {
		return nil
	}
	var out []string
	for _, room := range strings.Split(list, ",") {
		if room = strings.TrimSpace(room); room != "" {
			out = append(out, room)
		}
	}
	return out
}

// ventureNextRooms reads the room's printed NextRoom$ arrows, in script
// order. Every non-bottommost room of every dungeon script carries at least
// one arrow, so an empty answer means the marker has nowhere to go -- the
// bottommost room (whose venture is CR 701.49c, slice 3), or a room key the
// script does not resolve. Both fail closed to the same loud no-move Note.
func ventureNextRooms(f *cards.Face, room string) []string {
	sa := cards.ResolveSVar(f.SVars, room)
	if sa == nil {
		return nil
	}
	var out []string
	for _, next := range strings.Split(sa.Params["NextRoom"], ",") {
		if next = strings.TrimSpace(next); next != "" {
			out = append(out, next)
		}
	}
	return out
}

// ventureRoomLabel resolves a room key to its printed RoomName$, the same
// fallback view's dungeon projection takes: a missing or malformed
// RoomName$ falls back to the key rather than hiding the marker.
func ventureRoomLabel(f *cards.Face, room string) string {
	sa := cards.ResolveSVar(f.SVars, room)
	if sa == nil {
		return room
	}
	if name := strings.TrimSpace(sa.Params["RoomName"]); name != "" {
		return name
	}
	return room
}
