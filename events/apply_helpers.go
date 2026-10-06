// Shared fold helpers for the event switch: identity membership, commander
// indexing, remembered-target shaping, ring-emblem abilities, prepared-copy
// grants and control changes. Split out of events/apply.go: code moved
// verbatim, no behaviour change.
package events

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// containsObjID reports whether ids already holds want. Walked by index so
// the result never depends on map iteration order.
func containsObjID(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// commanderDenseIndex returns id's match-wide dense commander index - (valid
// commanders in every seat before its owner) plus (its position within its
// owner's Commanders list) - and whether id is a commander at all, mirroring
// the exact indexing rules.New assigns at genesis (see rules/engine.go):
// seat B's CmdDamage holds a slot for seat A's commander at A's commander's
// dense index, so the k-th commander of seat p is index
// sum(len(Commanders[s]) for s<p) + k. Walked deterministically by index over
// the seat slice and each seat's Commanders slice - never a map - so the
// order cannot and does not matter to the result.
func commanderDenseIndex(g *state.Game, id state.ObjID) (int, bool) {
	if id == 0 {
		return 0, false
	}
	for p := range g.Players {
		for k, c := range g.Players[p].Commanders {
			if c == id {
				idx := 0
				for s := 0; s < p; s++ {
					idx += len(g.Players[s].Commanders)
				}
				return idx + k, true
			}
		}
	}
	return 0, false
}

// rememberedFrom decodes an event's IDs into the Remembered list an ability
// object carries: a real object id becomes {Obj: id}, and a PlayerRef
// sentinel (state.PlayerRef, rules.pushTrigger) becomes {Player: p,
// IsPlayer: true}. TriggerPush and AbilityPush both use it so the two mint
// paths stay symmetric.
func rememberedFrom(ids []state.ObjID) []state.Target {
	var out []state.Target
	for _, id := range ids {
		if p, ok := id.PlayerRef(); ok {
			out = append(out, state.Target{Player: p, IsPlayer: true})
			continue
		}
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// ringEmblemAbility rebuilds one of the Ring emblem's four level abilities
// (CR 701.54c) from its level alone. The emblem has no corpus script text
// and no object in any zone, so these bodies are hand-built here in events,
// exactly as the granted ward/afflict payloads (KeywordTriggerPush) are:
// Apply rebuilds from the "__ring:<level>" payload so a log-only replay
// mints the identical ability object a live game did. Level N is active iff
// the tempted seat's RingTempted >= N; lower levels stay active as the count
// rises (the emitter gates, this function only builds).
//
//  1. "Whenever your Ring-bearer attacks, draw a card."
//  2. "Whenever your Ring-bearer becomes blocked, discard a card. If you
//     can't, sacrifice it." The discard is TgtChoose (the discarding
//     player's own choice); its RememberDiscarded$ records what (if
//     anything) went, and the chained Sacrifice is gated on that set being
//     EMPTY (ConditionDefined$ Remembered | ConditionPresent$ Card |
//     ConditionCompare$ EQ0) -- the corpus's exact "if you can't" shape
//     (Davriel, Soul Broker). effDiscard's strict-supersets rule means an
//     empty or too-small hand discards nothing and asks nothing, which IS
//     the "can't" arm.
//  3. "Whenever your Ring-bearer deals combat damage to a player,
//     sacrifice it." The SacValid$ reads the LIVE designation, so a bearer
//     already dead from the combat damage leaves nothing eligible: no ask,
//     no-op ("sacrifice it" of something that no longer exists).
//  4. "Whenever the Ring tempts you, each opponent loses 1 life."
func ringEmblemAbility(level int) *cards.SA {
	switch level {
	case 1:
		return &cards.SA{Kind: "DB", API: "Draw", Params: map[string]string{
			"Defined": "You",
		}}
	case 2:
		sac := &cards.SA{Kind: "DB", API: "Sacrifice", Params: map[string]string{
			"Defined": "You", "SacValid": "Card.IsRingbearer+YouCtrl", "Amount": "1",
			"ConditionDefined": "Remembered", "ConditionPresent": "Card", "ConditionCompare": "EQ0",
		}}
		return &cards.SA{Kind: "DB", API: "Discard", Params: map[string]string{
			"Defined": "You", "NumCards": "1", "Mode": "TgtChoose", "RememberDiscarded": "True",
		}, Sub: sac}
	case 3:
		return &cards.SA{Kind: "DB", API: "Sacrifice", Params: map[string]string{
			"Defined": "You", "SacValid": "Card.IsRingbearer+YouCtrl", "Amount": "1",
		}}
	case 4:
		return &cards.SA{Kind: "DB", API: "LoseLife", Params: map[string]string{
			"Defined": "Opponent", "LifeAmount": "1",
		}}
	}
	return nil
}

// grantPreparedCopy implements CR 722.3c's prepared grant: the battlefield
// permanent o gains the prepared designation (folded by the AlterAttribute
// case above) and its controller creates, in exile, a copy that carries only
// its prepare-spell face's characteristics. The copy is minted here, inside
// Apply, so a log-only replay mints the identical object from the same
// event; it is IsCopy (a spell copy, never a card) and PreparedSource names
// o so the cast offer and the cast-time unprepare can find the permanent.
//
// Any earlier copy still linked to o is invalidated (PreparedSource = 0)
// before the new one is minted, so a permanent that becomes prepared a second
// time never offers a stale copy. Every read from o is snapshotted BEFORE
// AddObject, which may reallocate g.Objs under the pointer.
func grantPreparedCopy(g *state.Game, o *state.Object) {
	src, card, ctrl := o.ID, o.Card, o.Controller
	for i := range g.Objs {
		if cp := &g.Objs[i]; cp.PreparedSource == src {
			cp.PreparedSource = 0
			g.ClearPreparedSource()
		}
	}
	cp := g.AddObject(card, ctrl)
	cp.IsCopy = true
	cp.SetFaceIdx(1)
	cp.PreparedSource = src
	g.NotePreparedSource()
	Move(g, cp.ID, state.ZLibrary, state.ZExile)
}

// Move relocates an object between zones, preserving zone order and the
// one-object-one-zone invariant.
//
// The zone an object is removed from is always o.Zone — the object's own
// recorded location — never the caller-supplied from. from (and Event.From
// in the log) exist for the client and for replay to read, but a caller that
// gets it wrong must not be able to leave the object in its real zone while
// also adding it to to: that would put it in two zones at once, and a
// repeat of the same wrong move would duplicate it within one zone.
//
// Moving an object to the zone it is already in is not special-cased: it is
// removed from that zone and appended again, so it ends up at the end of the

func changeControl(g *state.Game, o *state.Object, p state.PlayerID) {
	if o.Controller == p {
		return
	}
	if o.Zone == state.ZBattlefield {
		remove(g, o.ID, state.ZBattlefield, o.Controller)
		g.SetZone(state.ZBattlefield, p, append(g.Zone(state.ZBattlefield, p), o.ID))
		// The same tombstone walk as move's, skipped the same way while no
		// blocker is live (state.Game.BlockersLive).
		if live := g.BlockersLive(); live || ArenaSkipVerify {
			for i := range g.Objs {
				other := &g.Objs[i]
				if len(other.BlockedBy) == 0 || other.ID == o.ID {
					continue
				}
				if !live {
					panic(fmt.Sprintf("events: obj %d has BlockedBy %v while no blocker is live (a BlockedBy write skipped state.Game.NoteBlockers)", other.ID, other.BlockedBy))
				}
				for j, blocker := range other.BlockedBy {
					if blocker == o.ID {
						other.BlockedBy[j] = 0
					}
				}
			}
		}
		o.IsAttacking = false
		o.AttackingBattle = 0
		o.BlockedBy = nil
		o.SummonSick = true
		// kw:Echo's gate stamp (CR 702.35a): a battlefield control change is
		// a fresh "came under your control" moment for the new controller,
		// so the acquisition tuple re-stamps here. The echo trigger's own
		// ValidPlayer$ You keeps it firing only during the controller's
		// upkeep, so this record is read against the new controller's
		// Player.LastUpkeepTurn.
		o.AcqTurn = g.Turn
		o.AcqStep = g.Step
	}
	o.Controller = p
}

// validPlayer reports whether p indexes an existing seat.
func validPlayer(g *state.Game, p state.PlayerID) bool {
	return int(p) < len(g.Players)
}

// activeDungeon rejects malformed lifecycle events, including Obj 0 when no
// dungeon is active. A departed dungeon cannot accrue completions or rooms.
func activeDungeon(g *state.Game, p state.PlayerID, id state.ObjID) *state.Object {
	if !validPlayer(g, p) || id == 0 || g.Players[p].DungeonObj != id {
		return nil
	}
	o := g.Obj(id)
	if o == nil || o.Owner != p || o.Zone != state.ZCommand {
		return nil
	}
	return o
}

// manaClearKeepSlots parses the keep-mask Text the stat:UnspentMana emitter
// rides on a ManaClear event: one WUBRGC letter per pool slot whose unspent
// mana the boundary must not empty (rules/turn.go's unspentManaKeep). An
// empty Text (every historical event, and every game without a live
// carrier) keeps nothing. The answer is a fixed-size mask over the pool
// slot order, so the fold is a slice test, not a map lookup.
