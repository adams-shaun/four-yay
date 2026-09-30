package rules

// The airbend exile index's offer-surface equivalence (the airbendCastAvailable
// log-rescan brief): with the index on and off, the offered option sets and
// keys are byte-identical across an N-growth board carrying both an
// airbend-exiled card (the permission-true branch) and a plainly exiled card
// (the false branch), and the indexed read agrees with the literal backward
// scan for every exiled id at two different log lengths. The airbend
// permission is log-derived and the exile loop asks it for EVERY non-token
// exiled card, so the index exists to make that O(1) instead of one backward
// log scan per card per offer walk.
//
// This file's package-var toggling (airbendIndexOff) is why the test must
// never call t.Parallel: other tests in this package read the same package
// state.
import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// airbendMinerBoard is minerBoard plus two extra exiled cards: one moved into
// exile by a marker-carrying AIRBEND move (permission true) and one moved by
// an ordinary exile (permission false). Both are real corpus cards, so the
// exile loop's per-card airbend check visits each branch. It returns the
// engine, the Miner loop's three ids, and the two exiled ids.
func airbendMinerBoard(t *testing.T) (*Engine, []state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	names := []string{"Rakdos, the Muscle", "Phyrexian Altar", "Forsaken Miner", "Grizzly Bears", "Ornithopter"}
	e, ids := minerBoard(t, mountainDeck(t, 80), names,
		[]state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield, state.ZExile, state.ZExile})
	// The named moves above put both extra cards in exile WITHOUT a marker.
	// Stamp the airbend move on Grizzly Bears only: its latest move is now
	// the marker-carrying one, while Ornithopter's latest move stays the
	// plain exile. That is exactly the two branches the index must tell apart.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ids[3], From: state.ZExile, To: state.ZExile,
		Counter: effects.AirbendExileCounter})
	if !e.airbendCastAvailable(ids[3]) {
		t.Fatalf("precondition: Grizzly Bears (id %d) must carry the airbend permission after the marker move", ids[3])
	}
	if e.airbendCastAvailable(ids[4]) {
		t.Fatalf("precondition: Ornithopter (id %d) must NOT carry the airbend permission (plain exile)", ids[4])
	}
	return e, ids, ids[3], ids[4]
}

// fundAirbendCast puts exactly {C}{C} in seat 0's pool and re-runs the
// priority walk, so the exiled Grizzly Bears' {2} airbend recast is
// affordable and appears in the offer surface. Grizzly Bears costs {1}{G}{G},
// so the printed cost stays unpayable with no green in the pool and the {2}
// alternative is the offered route.
func fundAirbendCast(t *testing.T, e *Engine) {
	t.Helper()
	p := &e.G.Players[0]
	for c := range p.Pool {
		p.Pool[c] = 0
	}
	p.Pool[state.MC] = 2
	e.priorityRound()
}

// runAirbendMinerLoop drives the Miner cycle for n iterations, funding the
// airbend recast and capturing the offer surface at the top of every
// iteration. The board carries the airbend and plain exiled cards from
// airbendMinerBoard, so every captured offer walk runs the exile loop over
// both branches. It returns the captures, the event count and the chain head.
func runAirbendMinerLoop(t *testing.T, n int) (snapshots []string, eventCount int, head string) {
	t.Helper()
	e, ids, _, _ := airbendMinerBoard(t)
	loopDrain(t, e, 0, 0, 1)
	library := len(e.G.Zone(state.ZLibrary, 1))
	for i := 0; i < n; i++ {
		fundAirbendCast(t, e)
		snapshots = append(snapshots, capturePending(e))
		loopAction(t, e, ids[1], "activate")
		loopDrain(t, e, ids[2], 0, 1)
		if e.G.Obj(ids[2]).Zone != state.ZBattlefield {
			t.Fatalf("iteration %d: Miner in %v", i+1, e.G.Obj(ids[2]).Zone)
		}
		if len(e.G.Zone(state.ZLibrary, 1)) != max(0, library-i-1) || e.G.Players[1].Life != 20 || e.G.Over {
			t.Fatal("Miner cycle net resources wrong")
		}
	}
	return snapshots, len(e.L.Events), e.L.Head()
}

// containsAirbendCast reports whether a capturePending snapshot's option
// side fields carry an airbend_cast option for id. capturePending's side
// block writes `i=N key=... perm=... ctrl=... grant=...` per option and the
// wire JSON carries Mode, so the mode marker plus the object id pin it.
func containsAirbendCast(snap string, id state.ObjID) bool {
	return strings.Contains(snap, `"mode":"airbend_cast"`) && strings.Contains(snap, `"obj":`+strconv.Itoa(int(id)))
}

// TestAirbendIndexOfferSurface is the equivalence arm: the index on and off
// produce byte-identical offer surfaces across the N-growth board, the event
// stream and head are untouched, and the index answer equals the literal scan
// for every exiled id at two log lengths.
func TestAirbendIndexOfferSurface(t *testing.T) {
	oldOff := airbendIndexOff
	t.Cleanup(func() { airbendIndexOff = oldOff })

	airbendIndexOff = true
	offSnaps, offEvents, offHead := runAirbendMinerLoop(t, 20)
	airbendIndexOff = false
	onSnaps, onEvents, onHead := runAirbendMinerLoop(t, 20)

	// Preconditions: the board really carries both airbend branches, the log
	// is non-empty, the airbend recast is actually OFFERED (the walk consults
	// the index), and the offered surface is non-empty and CHANGES from
	// iteration 1 to 20 -- an empty or static surface would let an index that
	// offers nothing pass.
	e, _, airbent, plain := airbendMinerBoard(t)
	if len(e.L.Events) == 0 {
		t.Fatal("precondition: the board's log is empty")
	}
	if !e.airbendCastAvailable(airbent) {
		t.Fatal("precondition: the airbend branch is not exercised")
	}
	if e.airbendCastAvailable(plain) {
		t.Fatal("precondition: the plain-exile branch is not exercised")
	}
	fundAirbendCast(t, e)
	d := e.Pending()
	if d == nil {
		t.Fatal("precondition: no pending decision on the airbend miner board")
	}
	if len(d.Options) == 0 {
		t.Fatal("precondition: the offer surface is empty")
	}
	airbendOffered := false
	for _, o := range d.Options {
		if o.Mode == "airbend_cast" && o.Obj == airbent {
			airbendOffered = true
		}
	}
	if !airbendOffered {
		t.Fatalf("precondition: exileCastsWalk did not offer the airbend recast for id %d; options: %+v", airbent, d.Options)
	}
	if len(onSnaps) != 20 || len(offSnaps) != 20 {
		t.Fatalf("precondition: captured %d/%d snapshots, want 20 each", len(onSnaps), len(offSnaps))
	}
	if onSnaps[0] == onSnaps[19] {
		t.Fatal("precondition: the offer surface did not change from iteration 1 to 20; the accumulating shape is not exercised")
	}
	airbendInSurface := false
	for i := range onSnaps {
		if containsAirbendCast(onSnaps[i], airbent) {
			airbendInSurface = true
			break
		}
	}
	if !airbendInSurface {
		t.Fatal("precondition: no captured iteration offered the airbend recast")
	}

	// The equivalence: option sets and keys byte-identical, and the engine's
	// event stream and head untouched by the index.
	for i := range onSnaps {
		if onSnaps[i] != offSnaps[i] {
			t.Fatalf("iteration %d: offer surface diverged with the index on\noff: %s\non:  %s", i+1, offSnaps[i], onSnaps[i])
		}
	}
	if onEvents != offEvents || onHead != offHead {
		t.Fatalf("index changed the event stream: off events=%d head=%s, on events=%d head=%s", offEvents, offHead, onEvents, onHead)
	}

	// The direct unit equivalence: for every exiled id, the indexed map and
	// the literal backward scan agree -- at two different log lengths, so an
	// index built from a stale or truncated log would be caught.
	for _, logLen := range []int{len(e.L.Events) / 2, len(e.L.Events)} {
		ix := buildAirbendExileIndex(e.L.Events[:logLen])
		for _, id := range e.G.Zone(state.ZExile, e.G.Active) {
			if got, want := ix.available(id), airbendScan(e.L.Events[:logLen], id); got != want {
				t.Fatalf("log len %d: index available(%d)=%v, scan=%v", logLen, id, got, want)
			}
		}
	}
}
