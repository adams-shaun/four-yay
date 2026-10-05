package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("UnlockDoor", effUnlockDoor) }

type unlockDoorMode uint8

const (
	unlockDoorUnknown unlockDoorMode = iota
	unlockDoorPlain
	unlockDoorChoice
)

// Forge's mode spelling is case-insensitive; only these two shapes can
// identify a Room door operation. A lookup, not string dispatch at resolution.
var unlockDoorModes = map[string]unlockDoorMode{
	"unlock":       unlockDoorPlain,
	"lockorunlock": unlockDoorChoice,
}

// effUnlockDoor resolves Forge's `DB$ UnlockDoor` effect (CR 709.5f): the
// resolving player chooses a locked half of a Room they control, and that
// permanent is given the matching unlocked designation -- one DoorUnlock
// event, whose Apply updates its per-face designation and whose event the existing
// Mode$ UnlockDoor / Mode$ FullyUnlock trigger paths observe.
//
// `Mode$ Unlock` is the plain form (Ghostly Dancers, Ghostly Keybearer):
// unlock a locked half.
//
// `Mode$ LockOrUnlock` is the printed lock OR unlock choice (Keys to the
// House, Marina Vendrell, CR 709.5f/709.5g). It poses that choice to the
// seat before doing anything, so the effect never silently takes one half.
// Each door's designation is independent: a lock can affect the cast face
// even if the alternate face remains unlocked.
//
// The candidate pool is the ability's explicit target (`ValidTgts$`) when it
// was given one, otherwise the script's `Choices$` card pool, otherwise every
// Room of the resolving player's. It narrows to doors eligible for the chosen
// action. More than one door is a seat choice (a tape-answered KChoose).
func effUnlockDoor(h Host, c *Ctx, sa *cards.SA) {
	mode := strings.TrimSpace(sa.ParamStr(cards.PKMode))
	doorMode := unlockDoorModes[strings.ToLower(mode)]
	if doorMode == unlockDoorUnknown {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "UnlockDoor: unsupported Mode$ " + mode})
		return
	}

	pool, _ := unlockDoorPool(h, c, sa)
	if len(pool) == 0 {
		return
	}
	lock := doorMode == unlockDoorChoice && !askUnlockDoorHalf(h, c, sa)
	// Offer doors rather than Rooms: once a Room has been relocked, either
	// face may be the locked one. Stable pool order and face order determine
	// the R-9 no-host answer without depending on map iteration.
	type door struct {
		obj  *state.Object
		face int
	}
	var candidates []door
	for _, o := range pool {
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != c.Controller || o.Card == nil {
			continue
		}
		for fi, f := range o.Card.Faces {
			if f.IsRoom() && o.DoorUnlocked(fi) == lock {
				candidates = append(candidates, door{o, fi})
			}
		}
	}
	if len(candidates) == 0 {
		return
	}
	picked := candidates[0]
	if len(candidates) > 1 {
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Source: c.Source,
			Min: 1, Max: 1, ResumeKind: "choice", ResumeSA: sa,
			Prompt: "Choose a Room door"}
		for j, candidate := range candidates {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "card", Obj: candidate.obj.ID, Label: candidate.obj.Card.Faces[candidate.face].Name, Player: c.Controller})
		}
		if ans, ok := AskTape(h, d); ok && len(ans) != 0 {
			for j, option := range d.Options {
				if option.Index == ans[0].Index {
					picked = candidates[j]
					break
				}
			}
		}
	}
	kind := events.DoorUnlock
	if lock {
		kind = events.DoorLock
	}
	// Amount identifies the printed face (+1); zero is reserved for the
	// original paid-unlock event's alternate-face encoding.
	h.Emit(events.Event{Kind: kind, Obj: picked.obj.ID, Amount: int32(picked.face + 1)})
}

// askUnlockDoorHalf poses the printed `Mode$ LockOrUnlock` choice (CR
// 709.5f/709.5g) to the resolving player and reports whether the UNLOCK half
// was elected. The option order is fixed -- index 0 unlocked, index 1 locked
// -- so the R-9 no-answer stand-in (the productive unlock half) is option 0,
// the same default every KChoose seat stand-in takes. A seat that elects the
// lock half returns false; the caller reports the unimplemented half loudly.
func askUnlockDoorHalf(h Host, c *Ctx, sa *cards.SA) bool {
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Source: c.Source,
		Min: 1, Max: 1, ResumeKind: "choice", ResumeSA: sa,
		Prompt: "Lock or unlock a door?",
		Options: []decision.Option{
			{Index: 0, Kind: "unlock", Label: "Unlock a door", Player: c.Controller},
			{Index: 1, Kind: "lock", Label: "Lock a door", Player: c.Controller},
		}}
	ans, ok := AskTape(h, d)
	if !ok {
		return true
	}
	return len(ans) == 0 || ans[0].Index != 1
}

// unlockDoorPool is the candidate set before the "still has a locked door"
// narrowing, plus whether the ability declared its own `ValidTgts$` targeting
// (the flag the caller reports on when the pool comes back empty).
//
// A `ValidTgts$`-targeted ability's pool is EXACTLY the announcement/placement
// ask's answer, never the Choices$/all-controlled fallbacks: a Min-0 chooser
// that elected ZERO targets leaves Ctx.Targets empty (and, for a mid-
// resolution pre-ask, Ctx.PickedTargets empty-but-non-nil), and falling
// through to the wider pools would unlock a Room the player did NOT choose
// (Ghostly Keybearer's `ValidTgts$ Room.YouCtrl | TargetMin$ 0 | TargetMax$ 1`).
// Ctx.PickedTargets is the pre-ask's own set (the ONE home for that
// precedence: effects/context.go Defined, effects/counters.go moveCounterChosen);
// it outranks Ctx.Targets because Ctx.Targets is either the outer SA's
// (CLOBBER-inherited) or empty.
func unlockDoorPool(h Host, c *Ctx, sa *cards.SA) ([]*state.Object, bool) {
	g := h.Game()
	// Ctx.TargetsOffered is resolution-wide, while Ctx.Targets may belong
	// to an outer ability. Only consume the target list when this SA itself
	// owns the announcement ask, or the current pre-ask explicitly recorded
	// its answer. A nested non-targeted UnlockDoor must use its own Choices$
	// or controlled-Room pool, not its parent's target.
	ownAnnouncementTargets := TargetsOf(sa).Targeted() && !isLaterTargetedSub(c, sa)
	ownOfferedTargets := c.TargetsOffered && c.OfferedSA != nil && sa.Line == c.OfferedSA.Line
	if ownAnnouncementTargets || ownOfferedTargets || c.PickedTargets != nil {
		ts := c.Targets
		if c.PickedTargets != nil {
			ts = c.PickedTargets
		}
		out := make([]*state.Object, 0, len(ts))
		for _, t := range ts {
			if t.IsPlayer {
				continue
			}
			if o := g.Obj(t.Obj); o != nil {
				out = append(out, o)
			}
		}
		return out, true
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" {
		choices := cardChoices(h, c, sa, c.Controller)
		out := make([]*state.Object, 0, len(choices))
		for _, t := range choices {
			if o := g.Obj(t.Obj); o != nil {
				out = append(out, o)
			}
		}
		return out, false
	}
	out := make([]*state.Object, 0, len(g.Objs))
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone == state.ZBattlefield && o.Controller == c.Controller {
			out = append(out, o)
		}
	}
	return out, false
}

// doorUnlocked reports whether face fi has an unlocked designation (CR 709.5c).
// The cast face starts unlocked, but DoorLock may later lock either face.
func doorUnlocked(o *state.Object, fi int) bool {
	if o == nil || o.Card == nil || fi < 0 || fi >= len(o.Card.Faces) {
		return false
	}
	return o.DoorUnlocked(fi)
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
//
// Oracle divergence (flagged for controller triage, not a code bug): XMage's
// CentralElevatorPromisingStairs.UnlockedDoorNamesYouControlCount iterates
// every Room you control and adds BOTH printed names regardless of lock
// state, so it answers the number of door names among Rooms, not among
// UNLOCKED doors. CR 709.5j's card text ("different names among unlocked
// doors of Rooms you control") supports this reading -- gorge is likely
// right and XMage wrong -- but a compliance scenario will show the gap and
// the controller owns the xmage_wrong verdict (compliance/verdicts.go).
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
