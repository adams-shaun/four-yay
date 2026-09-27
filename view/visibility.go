package view

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Visibility is how much of the hidden information a projection reveals.
// It is a property of the viewer's relationship to the table, not of the
// game: the same state.Game projects three different Views.
type Visibility uint8

const (
	// Seat is a player's own view: their hand, a decision asked of them, and
	// every other seat's hidden zones as counts. A mana pool is public (CR
	// 106.4a/106.4b), so every seat's pool is present here too -- only the
	// hand is gated on "is this the viewer's own seat" (CR 400.2 names hand
	// as a hidden zone). This is what Project has always produced, minus the
	// now-retired pool redaction.
	Seat Visibility = iota
	// Public is a spectator with no seat: every hidden zone (the hand) is a
	// count, no decision is attached, and every seat's mana pool is present
	// (public under CR 106.4a/106.4b). Today's spectator redaction under a
	// name.
	Public
	// Omniscient is a spectator who sees every hand, and every mana pool too
	// (though the pool no longer needs this arm -- project() now fills the
	// pool for every seat, so this visibility only adds the hands) — the
	// bot-table default — but never library order: it spoils draws and
	// teaches nothing (spec D12).
	Omniscient
)

var visibilityNames = [...]string{"seat", "public", "omniscient"}

// String is the wire name; an out-of-range value prints "unknown", the same
// total shape as state.Step.String.
func (v Visibility) String() string {
	if int(v) < len(visibilityNames) {
		return visibilityNames[v]
	}
	return "unknown"
}

// ParseVisibility is String's inverse for flags and table configs.
func ParseVisibility(s string) (Visibility, error) {
	for i, n := range visibilityNames {
		if n == s {
			return Visibility(i), nil
		}
	}
	return 0, fmt.Errorf("view: unknown visibility %q (want seat, public or omniscient)", s)
}

// MarshalText/UnmarshalText make a Visibility its name in JSON and flags,
// so a table configuration on disk reads "omniscient", not 2.
func (v Visibility) MarshalText() ([]byte, error) { return []byte(v.String()), nil }

func (v *Visibility) UnmarshalText(b []byte) error {
	p, err := ParseVisibility(string(b))
	if err != nil {
		return err
	}
	*v = p
	return nil
}

// NoSeat is the viewer id of a spectator. state.PlayerID is a uint8 and no
// table has 255 seats, so it can never collide with a real seat; Project's
// own "out-of-range viewer is a spectator" rule does the rest.
const NoSeat state.PlayerID = 255

// ProjectFor is Project with an explicit visibility. Seat is exactly
// Project. Public forces the spectator path regardless of viewer. Omniscient
// projects every seat's hand (the pool is public under CR 106.4a/106.4b and
// project() already fills it for every seat). It also carries a copy of the
// pending decision for read-only replay/spectate, including private search
// options; only Public remains decision-free.
func ProjectFor(g *state.Game, ch Chars, viewer state.PlayerID, vis Visibility, d *decision.Decision) View {
	switch vis {
	case Public:
		v := project(g, ch, NoSeat, nil, false)
		v.Viewer = viewer
		v.Visibility = vis.String()
		return v
	case Omniscient:
		v := project(g, ch, viewer, nil, true)
		if g != nil {
			for i := range v.Players {
				p := &g.Players[i]
				// An omniscient spectator can read this hand, but cannot act from it:
				// ability costs are only needed on a viewer's own hand.
				v.Players[i].Hand = cardViews(g, ch, g.Zone(state.ZHand, p.ID), false, p.ID, viewer, true)
			}
		}
		v.Decision = copyDecision(d)
		v.Visibility = vis.String()
		return v
	default:
		v := project(g, ch, viewer, d, false)
		v.Visibility = Seat.String()
		return v
	}
}

// RedactEventsFor is RedactEvents with an explicit visibility. Seat and
// Public are RedactEvents (a Public viewer is NoSeat, so every owner-only
// branch stays closed). Omniscient passes every event through unredacted
// except a Secret event whose payload is or reveals library order —
// Shuffle (genesis order), LibraryOrder (a chosen new order on the top of
// the library), a Secret Note that is NOT a hand look (a private look at a
// library's top; a hand look rides From == ZHand and passes, because an
// omniscient spectator's visibility already includes every hand — the
// strip list is about library ORDER, per Ruling FL-9), or any Secret move
// landing back IN a library (a Dig/rearrange that returns a card to a
// hidden position reveals where in the order it went, per Ruling FL-9) —
// which keep only their shape. A Secret Draw or
// MoveZone OUT of the library still passes: the card is now in a hand the
// omniscient viewer sees. A non-Secret move into a library (e.g. from a
// public zone) is not this kind of reveal and stays public.
func RedactEventsFor(g *state.Game, evs []events.Event, viewer state.PlayerID, vis Visibility) []events.Event {
	out := make([]events.Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, RedactEventFor(g, e, viewer, vis))
	}
	return out
}

// RedactEventFor is RedactEventsFor's single-event half: redacts one event
// for one (viewer, visibility) pair. Split out so a hot caller (host's
// eventBodiesFor) that reads through every event can redact each into its
// own output without first building a len(evs) intermediate []events.Event
// (the whole-slice wrapper still allocates one for callers that want it).
// Visibility is a property of the viewer's relationship to a table, so the
// (viewer, vis) pair, taken together, is exactly the key that determines
// what a projection may reveal -- two callers may share a result only when
// their pair is identical.
func RedactEventFor(g *state.Game, e events.Event, viewer state.PlayerID, vis Visibility) events.Event {
	switch vis {
	case Public:
		// Public forces the NoSeat path regardless of who is asking -- a
		// real seat passed in with Public must not see its own hand.
		return RedactEvent(g, e, NoSeat)
	case Omniscient:
		e.IDs = append([]state.ObjID(nil), e.IDs...)
		e.Pairs = append([][2]state.ObjID(nil), e.Pairs...)
		if e.Secret && (e.Kind == events.Shuffle || e.Kind == events.PlanarDeckShuffle || (e.Kind == events.Note && e.From != state.ZHand) || e.Kind == events.LibraryOrder ||
			((e.Kind == events.MoveZone || e.Kind == events.Draw || e.Kind == events.PutOnStack) && e.To == state.ZLibrary)) {
			return events.Event{
				Seq: e.Seq, Kind: e.Kind, Player: e.Player,
				From: e.From, To: e.To, Step: e.Step, Secret: e.Secret,
			}
		}
		return e
	default:
		return RedactEvent(g, e, viewer)
	}
}

// poolView is the viewer-facing mana pool: only the symbols with mana in
// them, always non-nil so an empty pool marshals "{}" rather than null.
func poolView(m state.Mana) map[string]int32 {
	pool := map[string]int32{}
	for idx, sym := range [...]string{"W", "U", "B", "R", "G", "C"} {
		if n := m[idx]; n > 0 {
			pool[sym] = n
		}
	}
	return pool
}
