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
	// start is the object stint (objectStintStart) the count belongs to: a
	// later zone change makes a new object (CR 400.7) whose count starts
	// over from its own stint.
	start int
	pos   int
	n     int
}

// objectStint is objectStintStart's fold of id's zone over the log before
// pos: zone is the folded zone (known reports whether any zone-moving event
// has named id yet) and start is the index just past the latest event that
// actually moved id to a different zone.
type objectStint struct {
	id    state.ObjID
	pos   int
	zone  state.Zone
	known bool
	start int
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
	stints  []objectStint
}

// copyActivationIndex copies x's folds into dst's emptied storage, owned by
// c. Every entry is a value (svar is an immutable string; loyaltyPrefix's
// card points into the compiled corpus), so a by-value copy is exact.
func copyActivationIndex(dst activationIndex, x *activationIndex, c *Engine) activationIndex {
	return activationIndex{
		owner:   c,
		game:    append(dst.game[:0], x.game...),
		loyalty: append(dst.loyalty[:0], x.loyalty...),
		stints:  append(dst.stints[:0], x.stints...),
	}
}

func (e *Engine) activationIdx() *activationIndex {
	x := &e.actIndex
	if x.owner != e {
		*x = activationIndex{owner: e}
	}
	return x
}

// gameActivationsUsed is activationUsedCount(id, ability, svar, false): the
// count over the object's current stint (objectStintStart), never across a
// zone change.
func (e *Engine) gameActivationsUsed(id state.ObjID, ability int, svar string) int {
	x := e.activationIdx()
	n := len(e.L.Events)
	start := e.objectStintStart(id)
	var c *gameActivationCount
	for i := range x.game {
		g := &x.game[i]
		if g.id == id && g.ability == ability && g.svar == svar {
			c = g
			break
		}
	}
	if c == nil {
		x.game = append(x.game, gameActivationCount{id: id, ability: ability, svar: svar, start: start, pos: start})
		c = &x.game[len(x.game)-1]
	}
	if c.pos > n || c.start != start {
		c.start, c.pos, c.n = start, start, 0
	}
	c.n += activationUsedIn(e.L.Events[c.pos:n], id, ability, svar)
	c.pos = n
	if walkCacheVerify {
		if want := activationUsedIn(e.L.Events[objectStintStartIn(e.L.Events, id):], id, ability, svar); want != c.n {
			panic(fmt.Sprintf("rules: incremental game activation count for obj %d ability %d %q is %d, the scan %d", id, ability, svar, c.n, want))
		}
	}
	return c.n
}

// objectStintStart returns the log index just past the latest event that
// moved id into a DIFFERENT zone (0 when none has). ObjID is stable for the
// match, but CR 400.7 makes an object that changes zones a new object with
// no memory of its previous existence, so every per-object activation count
// ("Activate only once", "only once each turn", Exhaust, Power-up, Boast's
// once-each-turn) counts only events at or after this index. It is a pure
// fold of the log -- MoveZone, Draw and PutOnStack are the only kinds whose
// fold relocates an object (events.Apply's foldMoveZone) -- kept
// incrementally like the other folds in this file. A same-zone re-append
// (a library reorder) is not a new object.
func (e *Engine) objectStintStart(id state.ObjID) int {
	x := e.activationIdx()
	n := len(e.L.Events)
	var c *objectStint
	for i := range x.stints {
		if x.stints[i].id == id {
			c = &x.stints[i]
			break
		}
	}
	if c == nil {
		x.stints = append(x.stints, objectStint{id: id})
		c = &x.stints[len(x.stints)-1]
	}
	if c.pos > n {
		*c = objectStint{id: id}
	}
	evs := e.L.Events
	for i := c.pos; i < n; i++ {
		ev := &evs[i]
		if ev.Obj != id || !zoneMovingKind(ev.Kind) || !ev.To.Valid() {
			continue
		}
		if !c.known || c.zone != ev.To {
			c.start = i + 1
		}
		c.zone, c.known = ev.To, true
	}
	c.pos = n
	if walkCacheVerify {
		if want := objectStintStartIn(evs[:n], id); want != c.start {
			panic(fmt.Sprintf("rules: incremental stint start for obj %d is %d, the scan %d", id, c.start, want))
		}
	}
	return c.start
}

// objectStintStartIn is objectStintStart's whole-log scan.
func objectStintStartIn(evs []events.Event, id state.ObjID) int {
	start, known, zone := 0, false, state.Zone(0)
	for i := range evs {
		ev := &evs[i]
		if ev.Obj != id || !zoneMovingKind(ev.Kind) || !ev.To.Valid() {
			continue
		}
		if !known || zone != ev.To {
			start = i + 1
		}
		zone, known = ev.To, true
	}
	return start
}

// zoneMovingKind reports the event kinds whose fold relocates ev.Obj.
func zoneMovingKind(k events.Kind) bool {
	return k == events.MoveZone || k == events.Draw || k == events.PutOnStack
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
