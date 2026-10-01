package searchprobe

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type epochKey struct {
	Player  state.PlayerID
	Ordinal int
}

type epochPosition struct {
	Index int
	Name  string
	Ref   uint32
	// Unseen marks a draw whose object was first seen by the observer in the
	// same burst as the draw itself: the sampled world must fill this
	// position with a card its own observer has not introduced yet.
	Unseen bool
}

type epochConstraints struct {
	Positions      []epochPosition
	Deadlines      []deadlineConstraint
	ArrangeWindows int
	Unguided       []string
}

type epochCursor struct {
	ordinal  int
	draws    int
	reliable bool
	order    []int // remaining library positions in the epoch's original shuffle
	arrange  *epochArrange
	// pendingPlacement records a LibraryOrder whose order may yet be erased
	// by an immediately following Shuffle of the same library. Such a
	// placement leaks nothing (CR 701.20 destroys the order) and must not
	// unguide the pre-shuffle epoch; the flag is cleared by the erasing
	// shuffle, or resolved conservatively by the next event that touches
	// that library first.
	pendingPlacement bool
}

type epochArrange struct {
	frame int
	order []int
}

func (c epochCursor) original(index int) int {
	if c.order == nil {
		return c.draws + index
	}
	if index >= len(c.order) {
		return -1
	}
	return c.order[index]
}

func compileEpochs(h History) (map[epochKey]epochConstraints, error) {
	epochs := make(map[epochKey]epochConstraints)
	cursors := make(map[state.PlayerID]epochCursor)
	deadlineCounts := make(map[epochKey]map[string]int)
	names := make(map[uint32]string)
	owners := make(map[uint32]state.PlayerID)
	for frameIndex, frame := range h.Frames {
		introduced := make(map[uint32]Identity, len(frame.Identities))
		for _, identity := range frame.Identities {
			// Stack abilities have observer identities but no underlying Card.
			// A name is required only when an identity is used as a card fact.
			if identity.ID == 0 {
				return nil, fail("contradictory", "observed identity has zero reference")
			}
			if old := names[identity.ID]; old != "" && old != identity.Name {
				return nil, fail("contradictory", "observed identity %d changed from %s to %s", identity.ID, old, identity.Name)
			}
			names[identity.ID] = identity.Name
			owners[identity.ID] = identity.Owner
			introduced[identity.ID] = identity
		}
		for _, ev := range frame.Events {
			cursor := cursors[ev.Player]
			switch ev.Kind {
			case events.Shuffle:
				// This is the eraser for a placement this player's library
				// just received: the order is destroyed without ever being
				// observed, so the pre-shuffle epoch stays guided.
				cursor.pendingPlacement = false
				cursor.ordinal++
				cursor.draws = 0
				cursor.reliable = true
				cursor.order, cursor.arrange = nil, nil
				cursors[ev.Player] = cursor
				ensureEpoch(epochs, epochKey{Player: ev.Player, Ordinal: cursor.ordinal - 1})
				continue
			case events.Draw:
				// A draw observes the library before any shuffle erases a
				// pending placement: break the pairing and fall back to
				// today's conservative result.
				if cursor.pendingPlacement {
					addLibraryOrderUnguided(epochs, cursor, ev.Player)
					cursor.reliable = false
					cursor.pendingPlacement = false
				}
				if ev.Player == h.Actor && ev.Obj != 0 && names[ev.Obj] == "" {
					return nil, fail("contradictory", "draw identity %d has no card name", ev.Obj)
				}
				if cursor.ordinal > 0 && cursor.reliable && ev.Player == h.Actor && ev.Obj != 0 {
					name := names[ev.Obj]
					key := epochKey{Player: ev.Player, Ordinal: cursor.ordinal - 1}
					ep := epochs[key]
					position := epochPosition{Index: cursor.original(0), Name: name}
					if position.Index < 0 {
						return nil, fail("contradictory", "draw follows an empty observed library")
					}
					if _, firstSeen := introduced[ev.Obj]; !firstSeen {
						position.Ref = ev.Obj
					}
					// A drawn object that the observer had not introduced before
					// this burst must stay a first introduction in the sampled
					// world too: such positions require a physical card its own
					// observer has not yet introduced.
					position.Unseen = position.Ref == 0
					if err := addEpochPosition(&ep, position); err != nil {
						return nil, err
					}
					epochs[key] = ep
				}
				cursor.draws++
				cursor.arrange = nil
				if len(cursor.order) > 0 {
					cursor.order = cursor.order[1:]
				}
				cursors[ev.Player] = cursor
			}

			if identity, ok := introduced[ev.Obj]; ok && identity.Owner != h.Actor && ev.From == state.ZHand && !ev.To.Hidden() {
				if identity.Name == "" {
					return nil, fail("contradictory", "public hand-exit identity %d has no card name", ev.Obj)
				}
				delete(introduced, ev.Obj)
				ownerCursor := cursors[identity.Owner]
				if ownerCursor.ordinal > 0 && ownerCursor.reliable {
					key := epochKey{Player: identity.Owner, Ordinal: cursors[identity.Owner].ordinal - 1}
					if deadlineCounts[key] == nil {
						deadlineCounts[key] = make(map[string]int)
					}
					deadlineCounts[key][identity.Name]++
					ep := epochs[key]
					ep.Deadlines = append(ep.Deadlines, deadlineConstraint{Through: cursors[identity.Owner].draws, Name: identity.Name, Count: deadlineCounts[key][identity.Name]})
					epochs[key] = ep
				} else if ownerCursor.ordinal > 0 {
					key := epochKey{Player: identity.Owner, Ordinal: cursors[identity.Owner].ordinal - 1}
					ep := epochs[key]
					addUnguided(&ep, "opponent_hand_exit")
					epochs[key] = ep
				}
			}

			if ev.Kind == events.LibraryOrder {
				// A second placement before the first is shuffled away means
				// the earlier order was overwritten, not erased: break the
				// pairing conservatively.
				if cursor.pendingPlacement {
					addLibraryOrderUnguided(epochs, cursor, ev.Player)
					cursor.reliable = false
					cursor.pendingPlacement = false
				}
				if cursor.reliable && ev.Player == h.Actor && cursor.arrange != nil && cursor.arrange.frame == frameIndex {
					cursor.order = cursor.arrange.order
					cursor.arrange = nil
					cursors[ev.Player] = cursor
					continue
				}
				// Defer: a Shuffle of this same library immediately after
				// erases the order and leaves the epoch guided. An unpaired
				// placement is resolved at the end of the history.
				cursor.arrange = nil
				cursor.pendingPlacement = true
				cursors[ev.Player] = cursor
			}
			if ev.Kind == events.MoveZone && (ev.From == state.ZLibrary || ev.To == state.ZLibrary) {
				owner, known := owners[ev.Obj]
				// Player is the effect controller at some MoveZone sites. Without
				// an observed owner, invalidate every active cursor, not a guess.
				for player, affected := range cursors {
					if known && player != owner {
						continue
					}
					if affected.pendingPlacement {
						addLibraryOrderUnguided(epochs, affected, player)
						affected.reliable = false
						affected.pendingPlacement = false
					}
					if affected.ordinal > 0 {
						key := epochKey{Player: player, Ordinal: affected.ordinal - 1}
						ep := epochs[key]
						addUnguided(&ep, "library_mutation")
						epochs[key] = ep
					}
					affected.reliable = false
					affected.arrange = nil
					cursors[player] = affected
				}
			}
		}
		if d := frame.Decision; d != nil && d.Player == h.Actor && d.Kind == decision.KArrange {
			cursor := cursors[d.Player]
			if cursor.ordinal <= 0 {
				return nil, fail("contradictory", "arrange decision precedes library shuffle")
			}
			key := epochKey{Player: d.Player, Ordinal: cursor.ordinal - 1}
			ep := epochs[key]
			for i, option := range d.Options {
				ref := option.Action.Obj
				name := names[ref]
				if ref == 0 || name == "" {
					return nil, fail("contradictory", "arrange option %d has no observed identity", i)
				}
				if cursor.reliable {
					if err := addEpochPosition(&ep, epochPosition{Index: cursor.original(i), Name: name, Ref: ref}); err != nil {
						return nil, err
					}
				}
			}
			if cursor.reliable {
				ep.ArrangeWindows++
				if answer, answered := h.Answers[frameIndex]; answered {
					if order, ok := arrangedEpochPositions(frame, cursor, answer); ok {
						cursor.arrange = &epochArrange{frame: frameIndex + 1, order: order}
						cursors[d.Player] = cursor
					}
				}
			} else {
				addUnguided(&ep, "arrange_window")
			}
			epochs[key] = ep
		}
	}
	// An unresolved placement -- a LibraryOrder with no following Shuffle of
	// the same library anywhere in the history -- keeps the old conservative
	// result: the order stayed observable, so the pre-shuffle epoch is
	// unguided. A pairing that crossed a frame boundary was already consumed
	// by the Shuffle event, which clears the flag.
	for player, cursor := range cursors {
		if cursor.pendingPlacement {
			addLibraryOrderUnguided(epochs, cursor, player)
		}
	}
	return epochs, nil
}

// addLibraryOrderUnguided records the pre-shuffle epoch's positional loss when
// a LibraryOrder's order was never erased by a shuffle of the same library.
func addLibraryOrderUnguided(epochs map[epochKey]epochConstraints, cursor epochCursor, player state.PlayerID) {
	if cursor.ordinal <= 0 {
		return
	}
	key := epochKey{Player: player, Ordinal: cursor.ordinal - 1}
	ep := epochs[key]
	addUnguided(&ep, "library_order")
	epochs[key] = ep
}

// The owned board supplies only the public library size. Semantic actor
// answers identify option references; private/raw option indices never enter.
func arrangedEpochPositions(frame Frame, cursor epochCursor, answer []Action) ([]int, bool) {
	d := frame.Decision
	if len(answer) < d.Min || len(answer) > d.Max || len(d.Options) == 0 {
		return nil, false
	}
	size := -1
	for _, player := range frame.Board.Players {
		if player.Seat == d.Player {
			size = player.LibrarySize
		}
	}
	if size < len(d.Options) || cursor.order != nil && len(cursor.order) != size {
		return nil, false
	}
	kind := d.Options[0].Action.Kind
	if kind != "" && kind != "top" && kind != "bottom" {
		return nil, false
	}
	for _, option := range d.Options {
		if option.Action.Kind != kind {
			return nil, false
		}
	}
	chosen := make([]bool, len(d.Options))
	order := make([]int, 0, size)
	for _, action := range answer {
		found := -1
		for i, option := range d.Options {
			if action == option.Action {
				if found >= 0 {
					return nil, false
				}
				found = i
			}
		}
		if found < 0 || chosen[found] {
			return nil, false
		}
		chosen[found] = true
		order = append(order, cursor.original(found))
	}
	var unchosen []int
	for i := range d.Options {
		if !chosen[i] {
			unchosen = append(unchosen, cursor.original(i))
		}
	}
	if kind != "bottom" {
		order = append(order, unchosen...)
	}
	for i := len(d.Options); i < size; i++ {
		order = append(order, cursor.original(i))
	}
	if kind == "bottom" {
		order = append(order, unchosen...)
	}
	return order, true
}

func ensureEpoch(epochs map[epochKey]epochConstraints, key epochKey) {
	if _, ok := epochs[key]; !ok {
		epochs[key] = epochConstraints{}
	}
}

func addEpochPosition(epoch *epochConstraints, position epochPosition) error {
	for _, existing := range epoch.Positions {
		if position.Ref != 0 && existing.Ref == position.Ref && existing.Index != position.Index {
			return fail("contradictory", "observed object %d occupies shuffle positions %d and %d", position.Ref, existing.Index, position.Index)
		}
		if existing.Index != position.Index {
			continue
		}
		if existing.Name != position.Name || existing.Ref != position.Ref {
			return fail("contradictory", "position %d requires both %s and %s", position.Index, existing.Name, position.Name)
		}
		return nil
	}
	epoch.Positions = append(epoch.Positions, position)
	return nil
}

func addUnguided(epoch *epochConstraints, reason string) {
	for _, existing := range epoch.Unguided {
		if existing == reason {
			return
		}
	}
	epoch.Unguided = append(epoch.Unguided, reason)
}

func (k epochKey) String() string {
	return fmt.Sprintf("player %d shuffle %d", k.Player, k.Ordinal)
}
