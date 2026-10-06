package state

// CastBattlefield is the battlefield a spell found as it was cast: the frozen
// facts a Count$LastStateBattlefieldWithFallback read ("the number of Mounts
// you controlled as you cast this spell") consults instead of the live
// battlefield at resolution. It is written ONLY by events.Apply (the
// CastBattlefield event) onto the spell's stack object and cleared by the
// move that takes the object off the stack, so a stable ObjID never carries
// it into a later cast, and a stack copy inherits it through foldStackCopy.
//
// A non-nil CastBattlefield with no permanents is an AUTHORITATIVE empty
// battlefield (the count is zero); only a nil pointer means "no snapshot was
// taken" and lets the reader fall back to the live battlefield.
//
// The struct is immutable once built: nothing mutates Game or PT after
// FreezeBattlefield returns, so Object clones, arena clones and stack copies
// share the one pointer instead of duplicating it.
type CastBattlefield struct {
	// Game is a sparse game holding only the frozen battlefield permanents
	// (deep copies, so their controller, characteristics, counters and
	// attachments are as they stood at the cast). Its arena is indexed by the
	// live ObjIDs (slots of objects not on the frozen battlefield are
	// zero-valued and in no zone list), so the effects filter grammar runs
	// against it unchanged.
	Game *Game
	// PT holds each frozen permanent's layer-derived power and toughness, in
	// capture (zone) order. The layer walk lives in rules, which gathers the
	// values; the fold stores them verbatim.
	PT []FrozenPT
}

// FrozenPT is one frozen permanent's derived power and toughness.
type FrozenPT struct {
	ID               ObjID
	Power, Toughness int32
}

// DerivedPT returns the frozen derived P/T of id; ok is false when id was not
// on the frozen battlefield.
func (b *CastBattlefield) DerivedPT(id ObjID) (power, toughness int32, ok bool) {
	for i := range b.PT {
		if b.PT[i].ID == id {
			return b.PT[i].Power, b.PT[i].Toughness, true
		}
	}
	return 0, 0, false
}

// FreezeBattlefield deep-copies the named battlefield permanents of src into
// a CastBattlefield. ids is the capture order (it fixes each controller's
// zone-list order); an id that is not a battlefield object of src is skipped.
// pt is the parallel slice of derived powers/toughnesses; an id with no
// matching pt entry simply has no frozen derived P/T, so a reader falls back
// to its own live/printed read for that id. The result shares nothing mutable
// with src.
func FreezeBattlefield(src *Game, ids []ObjID, pt []FrozenPT) *CastBattlefield {
	names := make([]string, len(src.Players))
	for i := range src.Players {
		names[i] = src.Players[i].Name
	}
	var maxID ObjID
	for _, id := range ids {
		if o := src.Obj(id); o != nil && o.Zone == ZBattlefield && int(o.Controller) < len(src.Players) && id > maxID {
			maxID = id
		}
	}
	f := NewGameInto(names, 0, 0, nil)
	f.Objs = make([]Object, maxID)
	f.NextID = maxID + 1
	f.Turn, f.Active, f.Step = src.Turn, src.Active, src.Step
	for i := range src.Players {
		f.Players[i].Lost = src.Players[i].Lost
		f.Players[i].Life = src.Players[i].Life
	}
	out := &CastBattlefield{Game: f}
	for _, id := range ids {
		o := src.Obj(id)
		if o == nil || o.Zone != ZBattlefield || int(o.Controller) >= len(f.Players) {
			continue
		}
		dst := &f.Objs[id-1]
		o.CloneDeepInto(dst)
		dst.CastBattlefield = nil
		f.SetZone(ZBattlefield, o.Controller, append(f.Zone(ZBattlefield, o.Controller), id))
		for _, p := range pt {
			if p.ID == id {
				out.PT = append(out.PT, p)
				break
			}
		}
	}
	return out
}
