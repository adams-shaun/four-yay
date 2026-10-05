package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Discover (CR 701.57) exiles cards from the top of the controller's library
// until a nonland card with mana value at most Num is found.  The found card
// is offered as a free Play; every other card exiled by the scan is put on the
// bottom first.  The engine's deterministic existing-order convention is used
// for the CR's random bottom order, as it is for Cascade.
func init() {
	Register("Discover", effDiscover)
	Register("DiscoverBottom", effDiscoverBottom)
}

// discoverPlayer resolves WHO the discover acts for (CR 701.57: "that player
// discovers X").  Forge's DiscoverEffect defaults to Defined$ You (the
// resolving controller), so an absent Defined$ keeps c.Controller exactly as
// before this read.  A PRESENT Defined$ (Zoyowa's Justice's
// `Defined$ TargetedOwner`, the only corpus carrier) resolves through the ONE
// shared defined-player machinery (context.go's definedPlayers), so every
// spelling the corpus writes binds the way the rest of the engine binds it.
// The second result is false when the selector named nobody (no targets, a
// departed object): the caller fails closed and discovers for nobody rather
// than falling back to the resolving controller, the same fail-closed
// direction definedSpecTargetedOwner documents.
func discoverPlayer(h Host, c *Ctx, sa *cards.SA) (state.PlayerID, bool) {
	r := DefinedRefOf(sa)
	if !r.Set() {
		return c.Controller, true
	}
	ps := definedPlayers(h, c, sa)
	if len(ps) == 0 {
		return 0, false
	}
	return ps[0], true
}

func discoverPlaySA(value int32, remember bool, defined string) *cards.SA {
	// A PRESENT Defined$ names the discovering player; the Play's Controller$
	// resolves the same spelling to the same seat (play.go routes the ask and
	// the cast to it), so the found card is played by the player who
	// discovered it.  An empty spelling is ignored by both readers, so the
	// Play keeps its c.Controller default for the no-Defined carriers.
	play := &cards.SA{Kind: "DB", API: "Play", Params: map[string]string{
		"Defined": "Remembered", "WithoutManaCost": "True", "Optional": "True",
		"Controller": defined, "TriggerDescription": "Discover",
	}}
	// The tail carries the same Defined$ spelling so its own Discover marker
	// names the discovering seat rather than the resolving controller.
	tail := map[string]string{"Amount": strconv.FormatInt(int64(value), 10), "Defined": defined}
	if remember {
		tail["RememberDiscovered"] = "True"
	}
	play.Sub = &cards.SA{Kind: "DB", API: "DiscoverBottom", Params: tail}
	return play
}

func emitDiscover(h Host, c *Ctx, player state.PlayerID, value int32) {
	h.Emit(events.Event{Kind: events.Discover, Player: player, Obj: c.Source, Amount: value})
}

func effDiscover(h Host, c *Ctx, sa *cards.SA) {
	value := Num(h, c, sa, "Num", 0)
	if value < 0 {
		value = 0
	}
	g := h.Game()
	p, ok := discoverPlayer(h, c, sa)
	if !ok {
		// The Defined$ selector named nobody: no library is scanned, so no
		// card is exiled and no Discover marker is emitted (the fail-closed
		// direction, never the resolving controller's library).
		return
	}
	if int(p) >= len(g.Players) || g.Players[p].Lost {
		return
	}
	defined := DefinedRefOf(sa).Text
	lib := append([]state.ObjID(nil), g.Zone(state.ZLibrary, p)...)
	var exiled []state.ObjID
	var found state.ObjID
	for _, id := range lib {
		o := g.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary,
			To: state.ZExile, Text: "discovered"})
		exiled = append(exiled, id)
		if !o.Face().IsLand() && int(o.Face().Cmc()) <= int(value) {
			found = id
			break
		}
	}
	if len(exiled) > 0 {
		h.Emit(events.Event{Kind: events.Note, Player: p, IDs: exiled,
			Text: "discovers, exiling cards from the top of the library"})
	}
	// The found card is left in exile for the Play election.  All preceding
	// cards are already known not to be the discover card and can be bottomed
	// before the election without changing anything observable.
	rest := exiled
	if found != 0 {
		rest = exiled[:len(exiled)-1]
	}
	for _, id := range rest {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile,
			To: state.ZLibrary, Text: "undiscovered card put on the bottom of the library"})
	}
	if found == 0 {
		emitDiscover(h, c, p, value)
		return
	}
	c.Remembered = []state.Target{{Obj: found}}
	play := discoverPlaySA(value, strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberDiscovered)), "True"), defined)
	effPlay(h, c, play)
	if !h.Suspended() {
		// R-9's no-host fallback declines the optional Play.
		effDiscoverBottom(h, c, play.Sub)
	}
}

// effDiscoverBottom is the chained tail of the optional cast.  On a decline
// the found card moves to its controller's hand; after a free cast it has
// left exile and is therefore skipped. The preceding cards were already
// bottomed by effDiscover.
// The marker is emitted here because a found card's optional cast is part of
// completing the discover action.
func effDiscoverBottom(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	remembered := c.Remembered
	if !strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberDiscovered)), "True") {
		c.Remembered = nil
	}
	for _, t := range remembered {
		if t.IsPlayer || t.Obj == 0 {
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZExile {
			continue
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: t.Obj, From: state.ZExile,
			To: state.ZHand, Text: "the undiscovered card is put into its owner's hand"})
	}
	p, ok := discoverPlayer(h, c, sa)
	if !ok {
		return
	}
	emitDiscover(h, c, p, Num(h, c, sa, "Amount", 0))
}
