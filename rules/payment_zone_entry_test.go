package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The zone-entry index's buffer lives in the engine's hypothetical pool,
// which a Spare carries into the next game: a later game must never read an
// earlier game's records. Game A seats many permanents and moves each one;
// game B, built on A's Spare, indexes a smaller arena first, then seats new
// permanents (ids inside A's recycled records) and moves only the last one,
// which grows the index into the recycled tail. Every object B seated
// without a move answers the genesis sentinel, exactly as the log scan does.
func TestZoneEntryIndexSurvivesSpareReuse(t *testing.T) {
	t.Parallel()
	const land = "Name:Index Land\nTypes:Land\nOracle:x\n"
	a, cfg, _ := newFixtureDeck(t, 9311, srchSpell("U"))
	for range 40 {
		id := onBoard(t, a, 0, land)
		a.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
		a.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZBattlefield})
	}
	for i := range a.G.Objs {
		if id := a.G.Objs[i].ID; id != 0 && a.zoneEntrySeq(id) != a.paymentSourceZoneSeqScan(id) {
			t.Fatalf("game A: object %d disagrees with the scan", id)
		}
	}
	spare := a.Release()
	cfg.Spare = &spare
	b := New(cfg)
	b.Advance()
	if b.zoneEntrySeq(1) != b.paymentSourceZoneSeqScan(1) {
		t.Fatal("game B: object 1 disagrees with the scan")
	}
	var seated []state.ObjID
	for range 20 {
		seated = append(seated, onBoard(t, b, 0, land))
	}
	last := seated[len(seated)-1]
	b.emit(events.Event{Kind: events.MoveZone, Obj: last, From: state.ZBattlefield, To: state.ZExile})
	b.emit(events.Event{Kind: events.MoveZone, Obj: last, From: state.ZExile, To: state.ZBattlefield})
	for _, id := range seated {
		if got, want := b.zoneEntrySeq(id), b.paymentSourceZoneSeqScan(id); got != want {
			t.Fatalf("game B: object %d index seq %d, log scan %d (a recycled record leaked)", id, got, want)
		}
	}
}
