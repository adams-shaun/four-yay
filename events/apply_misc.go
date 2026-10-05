// Small event folds that do not warrant a file of their own: markers, mana pool, ring/monarch, planar and dungeon walks, player notes.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// foldManaAdd folds Kind ManaAdd into state.
func foldManaAdd(g *state.Game, e *Event) {
	applyManaAdd(g, e)
}

// foldManaClear folds Kind ManaClear into state.
func foldManaClear(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		// PersistentMana$ True units survive the boundary (CR 500.4 with the
		// producing card's exception) — only the slot's ordinary share
		// empties, and the tag tallies are drained alongside it so they
		// never exceed the shrunken pool. Persistent RESTRICTION batches
		// survive too; the ordinary ones empty with the pool.
		//
		// The Text keep letters (stat:UnspentMana, rules/turn.go's
		// unspentManaKeep) protect a slot whole: the static's "don't lose
		// unspent mana as steps and phases end" keeps the slot's ordinary
		// share AND its restriction batches of that colour (the restriction
		// provenance is not time-bounded; only the emptying is). "" keeps
		// nothing — the historical shape every game without a live carrier
		// emits — so old logs replay byte-identically.
		keep := manaClearKeepSlots(e.Text)
		player := &g.Players[e.Player]
		for i := range player.Pool {
			if keep[i] {
				continue
			}
			if clear := player.Pool[i] - player.PersistentMana[i]; clear > 0 {
				clearNonPersistent(player, i, clear)
			}
		}
		kept := player.RestrictedMana[:0]
		for _, r := range player.RestrictedMana {
			// state.ManaSlot is the ONE full-counter decoder (the payment
			// paths in rules/stack.go use it): a restricted batch stores its
			// producing ManaAdd.Counter verbatim, so a tagged red batch
			// ("SR" snow red, "TreasureR") read through ManaIndex(c[0])
			// would decode the tag letter as colourless and silently drop
			// the protected colour's spend restriction at the very boundary
			// the keep exists for. An empty Color batch (the unrestricted
			// AddsNoCounter provenance shape) decodes to the C slot, so a
			// keep that protects the C slot keeps it, slot-whole, like the
			// ordinary share above.
			if r.Persistent || keep[state.ManaSlot(r.Color)] {
				kept = append(kept, r)
			}
		}
		player.RestrictedMana = kept
	}
}

// foldManaUndo folds Kind ManaUndo into state.
func foldManaUndo(g *state.Game, e *Event) {
	// The announced payment window's reversal of one mana activation
	// (CR 733.1, announce-then-pay spec §5): remove exactly the units one
	// ManaAdd put in the pool -- the same slot and snow/typed tally the
	// add credited, through the add's own fold with the amount negated --
	// and untap the named source. The rules side offers it only when the
	// pool still holds those units, so the clamp is defensive.
	if validPlayer(g, e.Player) && e.Amount > 0 {
		rm := *e // a copy: a fold must never write the event it folds
		rm.Kind, rm.Amount, rm.Text = ManaAdd, -e.Amount, ""
		idx := manaAddSlot(rm.Counter)
		if have := g.Players[e.Player].Pool[idx]; have < e.Amount {
			rm.Amount = -have
		}
		if rm.Amount < 0 {
			applyManaAdd(g, &rm)
		}
	}
	if o := g.Obj(e.Obj); e.Obj != 0 && o != nil {
		o.Tapped = false
	}
}

// foldTap folds Kind Tap into state.
func foldTap(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		o.Tapped = true
	}
}

// foldUntap folds Kind Untap into state.
func foldUntap(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		o.Tapped = false
	}
}

// foldDoorUnlock folds Kind DoorUnlock into state.
func foldDoorUnlock(g *state.Game, e *Event) {
	// CR 309.5: the unlock activation paid the locked half's mana cost as
	// a sorcery. The flag is what makes the alternate face's rules text
	// live (rules' trigger/static/ability scans) and what a Mode$
	// UnlockDoor trigger matches against. Totality: an unknown object, or
	// one already unlocked, is a no-op.
	if o := g.Obj(e.Obj); o != nil {
		fi := 1 - int(o.FaceIdx)
		if e.Amount > 0 {
			fi = int(e.Amount - 1)
		}
		if fi >= 0 && fi < 2 {
			o.LockedDoors &^= 1 << uint(fi)
			if fi != int(o.FaceIdx) {
				o.Unlocked = true
			}
		}
	}
}

func foldDoorLock(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		fi := int(e.Amount - 1)
		if fi >= 0 && fi < 2 {
			o.LockedDoors |= 1 << uint(fi)
			if fi != int(o.FaceIdx) {
				o.Unlocked = false
			}
		}
	}
}

// foldSpeedChange folds Kind SpeedChange into state.
func foldSpeedChange(g *state.Game, e *Event) {
	// One speed change (CR 702.179). The once-per-turn trigger gate
	// belongs to rules; Apply folds the delta and clamps to [0, 4].
	if validPlayer(g, e.Player) {
		g.Players[e.Player].Speed += e.Amount
		if g.Players[e.Player].Speed < 0 {
			g.Players[e.Player].Speed = 0
		}
		if g.Players[e.Player].Speed > 4 {
			g.Players[e.Player].Speed = 4
		}
	}
}

// foldRingTemptsYou folds Kind RingTemptsYou into state.
func foldRingTemptsYou(g *state.Game, e *Event) {
	// One "the Ring tempts you" action (CR 701.54a): the count rises by
	// one and the designated permanent becomes (or stays) this seat's
	// Ring-bearer. An impossible bearer choice (no creature controlled)
	// carries Obj 0 and still counts — CR 701.54d: the "Whenever the Ring
	// tempts you" trigger fires when the actions complete even if some
	// were impossible.
	if validPlayer(g, e.Player) {
		g.Players[e.Player].RingTempted++
		g.Players[e.Player].RingBearer = e.Obj
	}
}

// foldRingEmblemPush folds Kind RingEmblemPush into state.
func foldRingEmblemPush(g *state.Game, e *Event) {
	// One of the Ring emblem's four level abilities (CR 701.54c) being
	// put on the stack. The ability is minted HERE, inside Apply, so a
	// log-only replay creates the exact same object a live game did
	// (Ruling T20-a, the KeywordTriggerPush precedent): the emblem has
	// no object in any zone, so TriggerPush's face-index derivation
	// cannot carry it and the "__ring:<level>" payload rebuilds the
	// ability structurally from the event text alone.
	if !validPlayer(g, e.Player) {
		return
	}
	level := int(e.Amount)
	if rest, ok := strings.CutPrefix(e.Counter, "__ring:"); ok {
		if n, err := strconv.Atoi(rest); err == nil {
			level = n
		}
	}
	sa := ringEmblemAbility(level)
	if sa == nil {
		return
	}
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	// The emblem is not an object, so there is no Source to carry: the
	// hand-built bodies read only their controller (Defined$ You /
	// Opponent) and the live Ring-bearer designation
	// (Card.IsRingbearer+YouCtrl). A zero Source makes
	// findTriggerForAbility false, so no intervening-if recheck and no
	// OptionalDecider read runs on it -- exactly the mandatory shape
	// CR 701.54c's "whenever" abilities are.
	o.Source = 0
	o.Remembered = rememberedFrom(e.IDs)
}

// foldMonarchChange folds Kind MonarchChange into state.
func foldMonarchChange(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Monarch, g.HasMonarch = e.Player, true
	}
}

// foldInitiativeChange folds Kind InitiativeChange into state.
func foldInitiativeChange(g *state.Game, e *Event) {
	// CR 726.3: only one player can have the initiative at a time; as a
	// player takes it, the player who currently has it ceases to have it.
	// Assigning the single holder covers both halves of that transition.
	if validPlayer(g, e.Player) {
		g.Initiative, g.HasInitiative = e.Player, true
	}
}

// foldBlessingChange folds Kind BlessingChange into state.
func foldBlessingChange(g *state.Game, e *Event) {
	// CR 702.131: one-way designation latch.
	if validPlayer(g, e.Player) {
		g.Players[e.Player].Blessing = true
	}
}

// foldEnduringStoryChange folds Kind EnduringStoryChange into state.
func foldEnduringStoryChange(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Players[e.Player].EnduringStory = true
	}
}

// foldPlanarDeckShuffle folds Kind PlanarDeckShuffle into state.
func foldPlanarDeckShuffle(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		ids := append([]state.ObjID(nil), e.IDs...)
		for _, id := range ids {
			if o := g.Obj(id); o != nil {
				o.Zone, o.FaceDown = state.ZPlanarDeck, true
			}
		}
		g.SetZone(state.ZPlanarDeck, e.Player, ids)
	}
}

// foldPlanarReveal folds Kind PlanarReveal into state.
func foldPlanarReveal(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		ids := g.Zone(state.ZPlanarDeck, e.Player)
		if len(ids) > 0 && ids[0] == e.Obj {
			if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZPlanarDeck {
				o.FaceDown = false
			}
		}
	}
}

// foldPlanarWalk folds Kind PlanarWalk into state.
func foldPlanarWalk(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		ids := g.Zone(state.ZPlanarDeck, e.Player)
		if len(ids) > 0 {
			if len(e.IDs) > 0 {
				// A Defined$ planeswalk names its destination(s): move each
				// named plane to the front (in the order named), keeping
				// every other plane's relative order. The previously-current
				// plane stays in the zone where it was, which is what
				// DontPlaneswalkAway$'s "don't planeswalk away" leaves
				// behind (Norn's Seedcore); the away trigger itself is
				// suppressed separately by the event's Amount flag.
				ids = planarWalkToOrder(ids, e.IDs)
			} else if len(ids) > 1 {
				ids = append(append([]state.ObjID(nil), ids[1:]...), ids[0])
			}
			for _, id := range ids {
				if o := g.Obj(id); o != nil && o.Zone == state.ZPlanarDeck {
					o.FaceDown = true
				}
			}
			if o := g.Obj(ids[0]); o != nil && o.Zone == state.ZPlanarDeck {
				o.FaceDown = false
			}
			g.SetZone(state.ZPlanarDeck, e.Player, ids)
		}
	}
}

// foldDungeonCreate folds Kind DungeonCreate into state.
func foldDungeonCreate(g *state.Game, e *Event) {
	if !validPlayer(g, e.Player) || g.Players[e.Player].DungeonObj != 0 {
		return
	}
	def := g.Tokens[e.Text]
	if def == nil {
		return
	}
	o := g.AddObject(def, e.Player)
	// A dungeon script is stored alongside token scripts, but the dungeon
	// object itself is not a battlefield token and persists in the command
	// zone until the dungeon is completed.
	Move(g, o.ID, state.ZLibrary, state.ZCommand)
	g.Players[e.Player].DungeonObj = o.ID
	g.Players[e.Player].DungeonRoom = ""
	g.Players[e.Player].DungeonCompleted = false
}

// foldDungeonRoom folds Kind DungeonRoom into state.
func foldDungeonRoom(g *state.Game, e *Event) {
	if e.Text != "" && activeDungeon(g, e.Player, e.Obj) != nil {
		g.Players[e.Player].DungeonRoom = e.Text
	}
}

// foldDungeonComplete folds Kind DungeonComplete into state.
func foldDungeonComplete(g *state.Game, e *Event) {
	if activeDungeon(g, e.Player, e.Obj) != nil && !g.Players[e.Player].DungeonCompleted {
		g.Players[e.Player].CompletedDungeons++
		g.Players[e.Player].DungeonCompleted = true
	}
}

// foldDungeonRemove folds Kind DungeonRemove into state.
func foldDungeonRemove(g *state.Game, e *Event) {
	if o := activeDungeon(g, e.Player, e.Obj); o != nil {
		Move(g, o.ID, state.ZCommand, state.ZCeased)
		g.Players[e.Player].DungeonObj = 0
		g.Players[e.Player].DungeonRoom = ""
		g.Players[e.Player].DungeonCompleted = false
	}
}

// foldPlayerNoted folds Kind PlayerNoted into state.
func foldPlayerNoted(g *state.Game, e *Event) {
	// A DB$ Pump body noted a label onto a player (NoteCards$ <defined>
	// | NoteCardsFor$ <label> -- Seize the Spotlight, Master of
	// Ceremonies). Player is the seat and Text the label; the note is
	// read back by the shared player filter's `Player.NotedFor<label>`
	// qualifier. Appending is idempotent (a re-note of the same label
	// does not duplicate it) and preserves first-note order, so a
	// log-only replay rebuilds the exact slice. An empty label or an
	// out-of-range seat writes nothing rather than a ghost note.
	if e.Text == "" || int(e.Player) >= len(g.Players) {
		return
	}
	p := &g.Players[e.Player]
	seen := false
	for _, n := range p.Notes {
		if n == e.Text {
			seen = true
			break
		}
	}
	if !seen {
		p.Notes = append(p.Notes, e.Text)
	}
}

// foldPlayerNoteCleared folds Kind PlayerNoteCleared into state.
func foldPlayerNoteCleared(g *state.Game, e *Event) {
	// ClearNotedCardsFor$ removes exactly one label. Retaining the remaining
	// order makes the event fold deterministic and replay-equivalent.
	if e.Text == "" || int(e.Player) >= len(g.Players) {
		return
	}
	p := &g.Players[e.Player]
	out := p.Notes[:0]
	for _, label := range p.Notes {
		if label != e.Text {
			out = append(out, label)
		}
	}
	p.Notes = out
}

// foldCardNoted folds Kind CardNoted into state.
func foldCardNoted(g *state.Game, e *Event) {
	// A DB$ Pump body noted a label onto a CARD (NoteCards$ Remembered |
	// NoteCardsFor$ <label> -- Volatile Chimera, Arcane Savant, Caller of
	// the Untamed; NoteCards$ TriggeredSource -- Maelstrom Archangel
	// Avatar). Obj is the noted object and Text the label; the note is
	// read back by the shared card filter's `Card.NotedFor<label>`
	// qualifier. Appending is idempotent (a re-note of the same label does
	// not duplicate it) and preserves first-note order, so a log-only
	// replay rebuilds the exact slice. An empty label or a vanished object
	// writes nothing rather than a ghost note. A note is card-identity
	// provenance, not zone-local state -- the setup-path carriers note
	// cards sitting in exile -- so the fold never clears on a zone move.
	if e.Text == "" {
		return
	}
	noted := g.Obj(e.Obj)
	if noted == nil {
		return
	}
	seen := false
	for _, n := range noted.Notes {
		if n == e.Text {
			seen = true
			break
		}
	}
	if !seen {
		noted.Notes = append(noted.Notes, e.Text)
	}
}
