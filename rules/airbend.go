package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// airbendCastAvailable reports whether the card id -- which the caller has
// already established sits in its owner's exile zone -- may be cast for {2}
// rather than its mana cost under CR 701.65a ("for as long as it remains
// exiled, its owner may cast it by paying {2} rather than its mana cost").
//
// The permission is log-derived, never mutable per-object state: the airbend
// exile is the MoveZone whose Counter carries effects.AirbendExileCounter
// (effects/airbend.go), and the permission lasts exactly as long as that
// marker-carrying move is the card's most recent move into exile. A card
// re-exiled by anything else fails (the marker is on an older move), and a
// card airbent again simply carries the marker on the newer move. The same
// log-derived shape warpRecastAvailable and foretellCastAvailable take, so a
// replayed game derives the identical answer.
//
// The answer comes from the engine's incremental airbend index
// (airbendIndex), which folds each log event exactly once; airbendScan is the
// literal backward scan it is equivalent to (airbendIndexOff selects it, and
// airbendIndexVerify checks every answer against it in the rules test binary).
func (e *Engine) airbendCastAvailable(id state.ObjID) bool {
	if airbendIndexOff {
		return airbendScan(e.L.Events, id)
	}
	got := e.airbendIndex().available(id)
	if airbendIndexVerify {
		if want := airbendScan(e.L.Events, id); got != want {
			panic(fmt.Sprintf("rules: incremental airbend index says %v for obj %d at log %d, the scan %v", got, id, len(e.L.Events), want))
		}
	}
	return got
}

// airbendIndexOff makes airbendCastAvailable answer with the literal backward
// log scan (airbendScan) instead of the incremental index, for the
// equivalence tests' index-off arm. Default false: production indexes.
var airbendIndexOff bool

// airbendIndexVerify: see derivedMemoVerify. Set by the rules test binary.
var airbendIndexVerify = derivedMemoVerifyFlag != ""

// airbendScan is the literal one-off backward log scan: the latest MoveZone
// naming id decides the permission. An in-exile card's most recent move is by
// construction the move that brought it there, so the marker on THAT move is
// the whole of the permission; a card whose latest move was out of exile has
// none, and a card never moved has none.
func airbendScan(log []events.Event, id state.ObjID) bool {
	for i := len(log) - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind != events.MoveZone || ev.Obj != id {
			continue
		}
		return airbendMove(&ev)
	}
	return false
}

// airbendMove is one MoveZone's verdict for its object: a move into exile
// carrying the airbend marker grants the permission, every other move ends it.
func airbendMove(ev *events.Event) bool {
	return ev.To == state.ZExile && ev.Counter == effects.AirbendExileCounter
}

// airbendExileIndex is the set of object ids whose latest MoveZone is an
// airbend exile, as a bitset over ObjID. Its words are never written in
// place: a change copies them first (fold), so an index may be shared freely
// -- a clone, or entry_counters.go's by-value preview engine, holds the same
// words as the engine it came from, and either one folding its own later
// events cannot disturb the other. Changes are rare (only an airbend exile,
// or an airbent card's next move), so the copies cost nothing in practice; a
// game with no airbend never allocates.
type airbendExileIndex struct {
	bits []uint64
}

// available reports whether id's latest folded MoveZone was an airbend exile.
func (ix *airbendExileIndex) available(id state.ObjID) bool {
	w := int(id >> 6)
	return w < len(ix.bits) && ix.bits[w]&(1<<(id&63)) != 0
}

// fold applies one MoveZone's verdict to its object.
func (ix *airbendExileIndex) fold(ev *events.Event) {
	id, perm := ev.Obj, airbendMove(ev)
	if ix.available(id) == perm {
		return
	}
	w := int(id >> 6)
	n := len(ix.bits)
	if w >= n {
		n = w + 1
	}
	bits := make([]uint64, n)
	copy(bits, ix.bits)
	bits[w] ^= 1 << (id & 63)
	ix.bits = bits
}

// buildAirbendExileIndex folds a whole log from scratch: the reference
// rebuild the incremental index is equivalent to.
func buildAirbendExileIndex(log []events.Event) airbendExileIndex {
	var ix airbendExileIndex
	for i := range log {
		if log[i].Kind == events.MoveZone {
			ix.fold(&log[i])
		}
	}
	return ix
}

// airbendIndex brings the engine's incremental index up to the current log
// head and returns it. Each event is folded once: the watermark
// (airbendFolded) records how much of the append-only log the index covers,
// and a clone carries both (Clone copies the log, so the watermark still
// names the same prefix). A log shorter than the watermark -- a different
// log installed on this engine -- restarts the fold from zero, so the answer
// is always the current log's.
func (e *Engine) airbendIndex() *airbendExileIndex {
	s := &e.legalScratch
	log := e.L.Events
	if s.airbendFolded > len(log) {
		s.airbendIx, s.airbendFolded = airbendExileIndex{}, 0
	}
	for i := s.airbendFolded; i < len(log); i++ {
		if log[i].Kind == events.MoveZone {
			s.airbendIx.fold(&log[i])
		}
	}
	s.airbendFolded = len(log)
	return &s.airbendIx
}
