package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Incremental forms of two whole-log activation scans the offer walk runs per
// ability per walk.
//
// Both are pure folds of the append-only event log (nothing they fold reads
// the current state, except where noted), so a fold kept at log position pos
// is exactly the fold of the first pos events for as long as the log still
// holds them: an entry whose pos is past the log's length (a shorter log) is
// dropped, and the rest of the log is folded on from pos. The entries live
// in a per-engine table owned like the walk's object classes: a by-value
// Engine copy (entryPreview's speculative engine, whose log forks the live
// prefix) starts its own table instead of writing the original's. Verify
// mode (walkCacheVerify) recomputes every answer with the full scan.

// gameActivationCount is one activationUsedCount(id, ability, svar, false)
// fold: the whole game's count at log position pos.
type gameActivationCount struct {
	id      state.ObjID
	ability int
	svar    string
	pos     int
	n       int
}

// loyaltyPrefix is loyaltyActivationsThisTurn's fold of id's battlefield
// stint and face over the log before pos (the events before the current
// turn), as of card: FlipFace's bound reads the object's face count.
type loyaltyPrefix struct {
	id   state.ObjID
	card *cards.Card
	pos  int
	onBF bool
	face int
}

// activationIndex is the engine's table of both folds.
type activationIndex struct {
	owner   *Engine
	game    []gameActivationCount
	loyalty []loyaltyPrefix
}

func (e *Engine) activationIdx() *activationIndex {
	x := &e.actIndex
	if x.owner != e {
		*x = activationIndex{owner: e}
	}
	return x
}

// gameActivationsUsed is activationUsedCount(id, ability, svar, false).
func (e *Engine) gameActivationsUsed(id state.ObjID, ability int, svar string) int {
	x := e.activationIdx()
	n := len(e.L.Events)
	var c *gameActivationCount
	for i := range x.game {
		g := &x.game[i]
		if g.id == id && g.ability == ability && g.svar == svar {
			c = g
			break
		}
	}
	if c == nil {
		x.game = append(x.game, gameActivationCount{id: id, ability: ability, svar: svar})
		c = &x.game[len(x.game)-1]
	}
	if c.pos > n {
		c.pos, c.n = 0, 0
	}
	c.n += activationUsedIn(e.L.Events[c.pos:n], id, ability, svar)
	c.pos = n
	if walkCacheVerify {
		if want := activationUsedIn(e.L.Events, id, ability, svar); want != c.n {
			panic(fmt.Sprintf("rules: incremental game activation count for obj %d ability %d %q is %d, the scan %d", id, ability, svar, c.n, want))
		}
	}
	return c.n
}

// activationUsedIn is activationUsedCount's per-event test over evs.
func activationUsedIn(evs []events.Event, id state.ObjID, ability int, svar string) int {
	used := 0
	for i := range evs {
		ev := &evs[i]
		if ev.Obj != id {
			continue
		}
		switch ev.Kind {
		case events.AbilityPush, events.ManaActivate:
			if ev.Kind == events.ManaActivate && len(ev.IDs) > 0 {
				continue
			}
			if ability >= 0 && ev.Amount == int32(ability) {
				used++
			}
		case events.DelayedPush, events.GrantAbilityPush:
			if svar != "" && ev.Counter == svar {
				used++
			}
		}
	}
	return used
}

// turnStart returns the index just past the log's last TurnChange that
// names a valid seat (0 when there is none): the point loyaltyActivations
// ThisTurn's count last reset at a turn boundary.
func (e *Engine) turnStart() int {
	evs := e.L.Events
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == events.TurnChange && int(evs[i].Player) < len(e.G.Players) {
			return i + 1
		}
	}
	return 0
}

// loyaltyStintAt returns id's folded battlefield stint and face at the
// current turn's start (turnStart), from the index's kept prefix.
func (e *Engine) loyaltyStintAt(o *state.Object, start int) (onBF bool, face int) {
	x := e.activationIdx()
	var c *loyaltyPrefix
	for i := range x.loyalty {
		if x.loyalty[i].id == o.ID {
			c = &x.loyalty[i]
			break
		}
	}
	if c == nil {
		x.loyalty = append(x.loyalty, loyaltyPrefix{id: o.ID})
		c = &x.loyalty[len(x.loyalty)-1]
		c.card = o.Card
	}
	if c.pos > start || c.card != o.Card {
		*c = loyaltyPrefix{id: o.ID, card: o.Card}
	}
	for _, ev := range e.L.Events[c.pos:start] {
		switch ev.Kind {
		case events.MoveZone:
			if ev.Obj == o.ID && ev.To.Valid() {
				c.onBF = ev.To == state.ZBattlefield
			}
		case events.FlipFace:
			if ev.Obj == o.ID && ev.Amount >= 0 && int(ev.Amount) < len(o.Card.Faces) {
				c.face = int(ev.Amount)
			}
		}
	}
	c.pos = start
	return c.onBF, c.face
}
