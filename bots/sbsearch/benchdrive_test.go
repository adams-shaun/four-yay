package sbsearch

// BP-16: botbench drives this adapter's own decisions through the bench
// driver's SearchSeat feed branch, and its SpellBench hooks through the
// registry's recursive UnwrapSeat. Both surfaces are asserted here; losing
// either silently changes the bench's sb-search-lite-atk behavior.
//
// The two benches are the split this file guards (spec §5.1): the HOST
// surface (`New`, what `bots.New` returns) is the plain View adapter and must
// NOT satisfy searchseat.SearchSeat or seat.BoardSeat; the BENCH surface
// (`NewBench`) is the wrapper the driver's feed branch dispatches on.

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/seat"
)

// NewBench must produce the shape the bench driver dispatches: a full
// searchseat.SearchSeat (DecideSearch for the live-feed branch, DecideBoard
// for a stopped feed), exactly as the bare *sbsearch.Seat was before BP-16.
func TestHostedSeatIsASearchSeat(t *testing.T) {
	s, err := NewBench(bots.Options{Seed: 3, Deps: bots.Deps{Cards: &cards.Registry{}}}, LiteAtk())
	if err != nil {
		t.Fatalf("NewBench: %v", err)
	}
	ss, ok := s.(searchseat.SearchSeat)
	if !ok {
		t.Fatalf("bench seat is not searchseat.SearchSeat: the bench's feed branch would never run: %T", s)
	}
	if _, ok := ss.(seat.Seat); !ok {
		t.Fatalf("bench seat is not seat.Seat: %T", ss)
	}
}

// NewBench's UnwrapSeat must expose the wrapped search seat so
// registry.UnwrapSeat's chain reaches the inner sb-tactical (the planner
// hand-off, the fallback's *builtins.Seat assertion, the stats read).
func TestHostedSeatUnwrapsToTheSearchSeat(t *testing.T) {
	s, err := NewBench(bots.Options{Seed: 3, Deps: bots.Deps{Cards: &cards.Registry{}}}, LiteAtk())
	if err != nil {
		t.Fatalf("NewBench: %v", err)
	}
	uw, ok := s.(interface {
		UnwrapSeat() seat.Seat
	})
	if !ok {
		t.Fatalf("bench seat has no UnwrapSeat: the bench's planner hand-off silently misses the inner seat")
	}
	if _, ok := uw.UnwrapSeat().(*sbsearch.Seat); !ok {
		t.Fatalf("UnwrapSeat = %T, want *sbsearch.Seat", uw.UnwrapSeat())
	}
}

// The HOST surface is deliberately NOT a searchseat.SearchSeat and NOT a
// seat.BoardSeat (§5.1). DecideBoard's signature is byte-identical to
// seat.BoardSeat's only method, so a hostedSeat carrying it would be
// type-asserted by any host code looking for a BoardSeat and routed through
// DecideBoard instead of the intended Decide/DecideEnv path. This is the
// negative half of the split: `bots.New` returns this plain adapter.
func TestHostedNewIsNotASearchOrBoardSeat(t *testing.T) {
	s, err := New(bots.Options{Seed: 3, Deps: bots.Deps{Cards: &cards.Registry{}}}, LiteAtk())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := s.(seat.BoardSeat); ok {
		t.Fatalf("bots.New seat satisfies seat.BoardSeat: %T (DecideBoard routes it away from the plain View path)", s)
	}
	if _, ok := s.(searchseat.SearchSeat); ok {
		t.Fatalf("bots.New seat satisfies searchseat.SearchSeat: %T", s)
	}
	if _, ok := s.(bots.EnvSeat); !ok {
		t.Fatalf("bots.New seat is not bots.EnvSeat: the host's Env dispatch would break: %T", s)
	}
	// Precondition: the two constructors are genuinely different types, so
	// the assertions above cannot pass by both being the same seat.
	bench, err := NewBench(bots.Options{Seed: 3, Deps: bots.Deps{Cards: &cards.Registry{}}}, LiteAtk())
	if err != nil {
		t.Fatalf("NewBench: %v", err)
	}
	if _, ok := bench.(seat.BoardSeat); !ok {
		t.Fatalf("NewBench seat is not seat.BoardSeat: %T (the bench's DecideBoard fallback must exist)", bench)
	}
}
