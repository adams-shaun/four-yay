package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("UnlockDoor", effUnlockDoor) }

// effUnlockDoor resolves Forge's `DB$ UnlockDoor` effect (CR 709.5f): the
// resolving player chooses a locked half of a Room they control, and that
// permanent is given the matching unlocked designation -- one DoorUnlock
// event, whose Apply flips state.Object.Unlocked and whose event the existing
// Mode$ UnlockDoor / Mode$ FullyUnlock trigger paths observe.
//
// `Mode$ Unlock` is the plain form (Ghostly Dancers, Ghostly Keybearer).
// `Mode$ LockOrUnlock` is the Keys to the House / Marina Vendrell form whose
// printed choice is lock OR unlock (CR 709.5f/709.5g). The engine's Room model
// keeps a single `Unlocked` designation for the alternate half, on top of the
// cast face that is unlocked from entry (CR 709.5d); at any state exactly one
// of the two printed actions is representable -- a Room with a locked
// alternate half can be unlocked, and only a fully unlocked Room could be
// locked. Locking a half is NOT modelled: a LockOrUnlock instruction that
// lands on a fully unlocked Room reports that loudly (one Note naming the
// unimplemented half) rather than silently doing nothing.
//
// The candidate pool is the ability's explicit target (`ValidTgts$`) when it
// was given one, otherwise the script's `Choices$` card pool, otherwise every
// Room of the resolving player's. Either way the effect narrows it to Rooms
// that still have a locked door. More than one candidate is a seat choice
// (a tape-answered KChoose); exactly one is forced.
func effUnlockDoor(h Host, c *Ctx, sa *cards.SA) {
	mode := strings.TrimSpace(sa.ParamStr(cards.PKMode))
	lockOrUnlock := strings.EqualFold(mode, "LockOrUnlock")
	if !strings.EqualFold(mode, "Unlock") && !lockOrUnlock {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "UnlockDoor: unsupported Mode$ " + mode})
		return
	}

	var candidates []*state.Object
	for _, o := range unlockDoorPool(h, c, sa) {
		if roomHasLockedDoor(o, c.Controller) {
			candidates = append(candidates, o)
		}
	}
	if len(candidates) == 0 {
		// A LockOrUnlock ability whose only target is fully unlocked has only
		// the lock half of its printed choice left, which is not modelled.
		if lockOrUnlock && len(c.Targets) != 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "UnlockDoor: Mode$ LockOrUnlock cannot lock a half in this model"})
		}
		return
	}

	picked := candidates
	if len(candidates) > 1 {
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Source: c.Source,
			Min: 1, Max: 1, ResumeKind: "choice", ResumeSA: sa,
			Prompt: sa.ParamStr(cards.PKChoiceTitle)}
		if d.Prompt == "" {
			d.Prompt = "Choose a Room door to unlock"
		}
		for j, o := range candidates {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "card", Obj: o.ID, Player: c.Controller})
		}
		if ans, ok := AskTape(h, d); ok {
			picked = nil
			for _, t := range ChoiceAnswerTargets(ans) {
				if t.IsPlayer {
					continue
				}
				if o := h.Game().Obj(t.Obj); o != nil {
					picked = append(picked, o)
				}
			}
		} else {
			// No tape/host: the deterministic stand-in is the first candidate
			// in object-id order (unlockDoorPool is already in that order).
			picked = candidates[:1]
		}
	}
	for _, o := range picked {
		if roomHasLockedDoor(o, c.Controller) {
			h.Emit(events.Event{Kind: events.DoorUnlock, Obj: o.ID})
		}
	}
}

// unlockDoorPool is the candidate set before the "still has a locked door"
// narrowing: the ability's chosen targets, else its Choices$ card pool, else
// every battlefield permanent the resolving player controls.
func unlockDoorPool(h Host, c *Ctx, sa *cards.SA) []*state.Object {
	g := h.Game()
	if len(c.Targets) != 0 {
		out := make([]*state.Object, 0, len(c.Targets))
		for _, t := range c.Targets {
			if t.IsPlayer {
				continue
			}
			if o := g.Obj(t.Obj); o != nil {
				out = append(out, o)
			}
		}
		return out
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" {
		choices := cardChoices(h, c, sa, c.Controller)
		out := make([]*state.Object, 0, len(choices))
		for _, t := range choices {
			if o := g.Obj(t.Obj); o != nil {
				out = append(out, o)
			}
		}
		return out
	}
	out := make([]*state.Object, 0, len(g.Objs))
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone == state.ZBattlefield && o.Controller == c.Controller {
			out = append(out, o)
		}
	}
	return out
}

// doorUnlocked reports whether face fi of o carries an unlocked designation
// (CR 709.5c). The cast face (FaceIdx) is given the designation as the Room
// enters (CR 709.5d); the alternate half is unlocked iff the permanent's
// Unlocked flag is set by the DoorUnlock fold. A face index outside the
// printed faces is never unlocked.
func doorUnlocked(o *state.Object, fi int) bool {
	if o == nil || o.Card == nil || fi < 0 || fi >= len(o.Card.Faces) {
		return false
	}
	return fi == int(o.FaceIdx) || o.Unlocked
}

// roomHasLockedDoor reports whether o is a battlefield Room controlled by
// controller that still has at least one locked door face -- the exact
// precondition of an unlock instruction (CR 709.5f).
func roomHasLockedDoor(o *state.Object, controller state.PlayerID) bool {
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != controller || o.Card == nil {
		return false
	}
	for fi, f := range o.Card.Faces {
		if f.IsRoom() && !doorUnlocked(o, fi) {
			return true
		}
	}
	return false
}

// countUnlockedDoors is the ONE home for the Count$UnlockedDoors and
// Count$DistinctUnlockedDoors heads. It counts unlocked door FACES among the
// Room permanents player p controls: a partially unlocked Room contributes
// its cast face, a fully unlocked one both faces (CR 709.5j, "a door is a
// half of a Room permanent"). distinct collapses the faces to their names, so
// two fully unlocked copies of one Room count once.
func countUnlockedDoors(g *state.Game, p state.PlayerID, distinct bool) int32 {
	var n int32
	names := make(map[string]struct{})
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone != state.ZBattlefield || o.Controller != p || o.Card == nil {
			continue
		}
		for fi, f := range o.Card.Faces {
			if !f.IsRoom() || !doorUnlocked(o, fi) {
				continue
			}
			if distinct {
				names[f.Name] = struct{}{}
			} else {
				n++
			}
		}
	}
	if distinct {
		return int32(len(names))
	}
	return n
}
