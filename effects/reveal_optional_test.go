package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// revealBoard builds a 2-seat game with a source object for seat 0, an
// instant and a land on seat 0's library (instant on top), and returns the
// host plus the object ids (instant first, then the land, then the source).
func revealBoard(t *testing.T) (*fakeHost, []state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	bolt := mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:3 damage\n")
	mtn := mkCard(t, "Name:Mountain\nTypes:Land\nOracle:x\n")
	fix := mkCard(t, "Name:Fixture\nTypes:Creature\nPT:1/1\nOracle:x\n")
	var ids []state.ObjID
	ids = append(ids, h.g.AddObject(bolt, 0).ID)
	ids = append(ids, h.g.AddObject(mtn, 0).ID)
	ids = append(ids, h.g.AddObject(fix, 0).ID)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{ids[0], ids[1]})
	for _, id := range ids[:2] {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	return h, ids
}

// TestRevealOptionalNoHostKeepsTheMandatoryReveal pins the R-9 fallback: a
// host that cannot ask keeps the pre-ask behaviour — the mandatory reveal,
// RememberRevealed$ fired — deterministically.
func TestRevealOptionalNoHostKeepsTheMandatoryReveal(t *testing.T) {
	h, ids := revealBoard(t)
	ctx := &Ctx{Controller: 0, Source: ids[2], Remembered: []state.Target{{Obj: ids[2]}}}
	sa := sa(t, "SP$ PeekAndReveal | Defined$ You | NumCards$ 1 | RevealOptional$ True | RememberRevealed$ True")
	Resolve(h, ctx, sa)
	found := false
	for _, e := range h.log {
		if e.Kind == events.Note && len(e.IDs) == 1 && e.IDs[0] == ids[0] {
			found = true
		}
	}
	if !found {
		t.Fatal("the no-host fallback did not reveal")
	}
	if len(ctx.Remembered) != 2 || ctx.Remembered[1].Obj != ids[0] {
		t.Fatalf("Remembered = %+v, want the fallback reveal remembered", ctx.Remembered)
	}
}
