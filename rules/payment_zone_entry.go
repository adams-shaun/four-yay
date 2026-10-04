package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The engine's zone-entry index: paymentSourceZoneSeq for every object, kept
// up to date incrementally over the append-only event log.
//
// paymentSourceZoneSeqScan's answer for object id is the Seq of the LATEST
// event naming id that is a MoveZone into id's current zone, or (on the
// battlefield) a TokenCreate; GenesisZoneSeq when there is none. The index
// records, per object, the log position of its latest MoveZone (with that
// move's destination) and of its latest TokenCreate, folding each event once
// as the log grows. The latest MoveZone overall is the latest MoveZone into
// the current zone whenever its destination IS the current zone; when it is
// not (a probe moved the object without an event, castprobe.go), the lookup
// falls back to the scan. The TokenCreate position is compared by log
// position, so the answer is exactly the scan's.
//
// It replaces the per-query backward pass (one full log scan per planner
// query scope, measured ~1.1% of the random SpellBench leg) and the scans
// the executor ran outside any query (~1.2%).
//
// Validity: the log is append-only history (events.Log), so a prefix once
// folded never changes. The index is engine scratch (Clone and a new engine
// start it empty) and additionally pins the address of the last folded
// event: any reallocation or replacement of the log array (an append that
// regrows it, host trimLog's exact copy) or a shorter log refolds from the
// start rather than trusting positions in another array. Verify mode
// (walkCacheVerify) compares every lookup with the scan.
type zoneEntryIndex struct {
	n    int           // events folded
	tail *events.Event // &log[n-1] when n > 0
	pool *hypSparePool // the pool whose buffer recs is
	recs []zoneEntryRec
}

// zoneEntryRec is one object's latest MoveZone (log position + 1, 0 for
// none, and its destination) and latest TokenCreate (position + 1).
type zoneEntryRec struct {
	move   int32
	tok    int32
	moveTo state.Zone
}

// zoneEntrySync folds every event appended since the last sync and returns
// the up-to-date records.
func (e *Engine) zoneEntrySync() []zoneEntryRec {
	x := &e.zoneEntry
	log := e.L.Events
	pl := e.hypPool()
	if x.pool != pl || len(log) < x.n || (x.n > 0 && &log[x.n-1] != x.tail) {
		// A new engine, a new pool or a moved log: refold from the start in
		// the pool's recycled buffer.
		x.pool = pl
		x.n, x.tail = 0, nil
		pl.zoneEntry = resizeCleared(pl.zoneEntry, len(e.G.Objs)+1)
		x.recs = pl.zoneEntry
	}
	if x.n == len(log) {
		return x.recs
	}
	recs := x.recs
	for i := x.n; i < len(log); i++ {
		ev := &log[i]
		if ev.Kind != events.MoveZone && ev.Kind != events.TokenCreate {
			continue
		}
		id := ev.Obj
		if id == 0 {
			continue
		}
		if _, player := id.PlayerRef(); player {
			continue // a player reference never names an object.
		}
		if int(id) >= len(recs) {
			recs = growZoneEntry(recs, int(id)+1, len(e.G.Objs)+1)
		}
		r := &recs[id]
		if ev.Kind == events.MoveZone {
			r.move, r.moveTo = int32(i+1), ev.To
		} else {
			r.tok = int32(i + 1)
		}
	}
	x.recs, pl.zoneEntry = recs, recs
	x.n = len(log)
	x.tail = &log[x.n-1]
	return recs
}

// growZoneEntry extends recs to at least n entries, doubling; every entry
// past the old length is zero. Within capacity the extension is cleared
// explicitly: the array is the pool's recycled buffer, whose tail still
// holds an earlier index's records (a previous game's, through a Spare).
func growZoneEntry(recs []zoneEntryRec, n, hint int) []zoneEntryRec {
	if n <= cap(recs) {
		old := len(recs)
		recs = recs[:n]
		clear(recs[old:])
		return recs
	}
	c := max(n, hint, 2*cap(recs))
	out := make([]zoneEntryRec, n, c)
	copy(out, recs)
	return out
}

// zoneEntrySeq is paymentSourceZoneSeqScan's answer for id through the index.
func (e *Engine) zoneEntrySeq(id state.ObjID) uint64 {
	o := e.G.Obj(id)
	if o == nil {
		return decision.GenesisZoneSeq
	}
	recs := e.zoneEntrySync()
	if int(id) >= len(recs) {
		return decision.GenesisZoneSeq // no event has named id.
	}
	r := recs[id]
	best := int32(0)
	if r.move != 0 {
		if r.moveTo != o.Zone {
			return pay.PaymentSourceZoneSeqScan(asPayer(e), id)
		}
		best = r.move
	}
	if o.Zone == state.ZBattlefield && r.tok > best {
		best = r.tok
	}
	if best == 0 {
		return decision.GenesisZoneSeq
	}
	return e.L.Events[best-1].Seq
}
