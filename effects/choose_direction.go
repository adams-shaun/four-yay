package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("ChooseDirection", effChooseDirection)
}

// directionLeft / directionRight are the two answers ChooseDirection offers.
// They are the wire labels a client sees and the strings the chosen direction
// is carried as; "left" is the next seat in turn (APNAP) order and "right"
// the previous one, the two directions around the seating ring.
const (
	directionLeft  = "left"
	directionRight = "right"
)

// effChooseDirection implements Forge's ChooseDirection: a mid-resolution
// "choose left or right" ask (Aminatou, the Fateshifter's [-6]; Order of
// Succession; Mystic Barrier / Pramikon / Teyo's attack-direction statics).
// The answer is the ability's own left/right pick, not an object or player,
// so it has no event-backed home: it rides Ctx.ChosenDirection, the same
// resolution-scratch transport NameChoice/ChosenColor use, and the answer is
// re-derived by replay from the recorded intent through the "choosedirection"
// resume arm. The value must survive to the SubAbility$ that consumes it
// (Aminatou's DBControl, Order's DBGainControl), so it is left set on the
// shared Ctx for the rest of the walk instead of being cleared.
//
// A no-host run takes the deterministic left fallback, the R-9 stand-in for
// an ask the engine cannot pose.
func effChooseDirection(h Host, c *Ctx, sa *cards.SA) {
	if dir := strings.ToLower(strings.TrimSpace(c.ChosenDirection)); dir == directionLeft || dir == directionRight {
		// The answered re-entry (the "choosedirection" resume arm set the
		// field). Nothing further to emit: the direction is resolution
		// context its SubAbility reads, not a state mutation.
		return
	}
	chooser := c.Controller
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "choosedirection", ResumeSA: sa,
		Prompt: "Choose left or right"}
	d.Options = append(d.Options,
		decision.Option{Index: 0, Kind: "direction", Label: directionLeft},
		decision.Option{Index: 1, Kind: "direction", Label: directionRight})
	if Ask(h, d) == AskAsked {
		return
	}
	c.ChosenDirection = directionLeft
}

// gainControlNeighbor returns the nearest living seat from p in direction dir
// ("left" = next in APNAP order, "right" = previous). The walk is around the
// full seating ring, skipping eliminated seats, so the neighbour of a seat is
// always another LIVING seat -- the checked turn/seat order a bare index
// arithmetic would miss. ok is false when no other living seat exists (a
// one-seat ring has no neighbour).
func gainControlNeighbor(g *state.Game, p state.PlayerID, dir string) (state.PlayerID, bool) {
	n := len(g.Players)
	if n == 0 {
		return 0, false
	}
	step := 1
	if dir == directionRight {
		step = n - 1
	}
	for k := 1; k <= n; k++ {
		q := state.PlayerID((int(p) + step*k) % n)
		if !g.Players[q].Lost {
			if q == p {
				return 0, false
			}
			return q, true
		}
	}
	return 0, false
}

// gainControlDirectionRing lists every living seat once, starting at start and
// proceeding in direction dir -- Order of Succession's "starting with you and
// proceeding in the chosen direction" visit order. It falls back to APNAP
// order from start when start itself has left the game.
func gainControlDirectionRing(g *state.Game, start state.PlayerID, dir string) []state.PlayerID {
	alive := g.AliveFrom(0)
	if len(alive) == 0 {
		return nil
	}
	ok := false
	for _, p := range alive {
		if p == start {
			ok = true
			break
		}
	}
	if !ok {
		return g.AliveFrom(start)
	}
	out := make([]state.PlayerID, 0, len(alive))
	cur := start
	for len(out) < len(alive) {
		out = append(out, cur)
		next, nextOK := gainControlNeighbor(g, cur, dir)
		if !nextOK {
			break
		}
		cur = next
	}
	return out
}

// gainControlVariantDirection reads the direction ChooseDirection chose for
// the resolving chain and reports whether it is a modelled one. A value the
// engine cannot read is a loud Note and no transfer -- the fail-closed
// direction, never a silent default.
func gainControlVariantDirection(h Host, c *Ctx, sa *cards.SA) (string, bool) {
	dir := strings.ToLower(strings.TrimSpace(c.ChosenDirection))
	if dir == directionLeft || dir == directionRight {
		return dir, true
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "GainControlVariant " + sa.Params["ChangeController"] + " has no chosen direction"})
	return "", false
}
