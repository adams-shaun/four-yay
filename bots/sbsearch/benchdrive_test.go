package sbsearch

// BP-16: botbench drives this adapter's own decisions through the bench
// driver's SearchSeat feed branch, and its SpellBench hooks through the
// registry's recursive UnwrapSeat. Both surfaces are asserted here; losing
// either silently changes the bench's sb-search-lite-atk behavior.

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/seat"
)

// New must produce the shape the bench driver dispatches: a full
// searchseat.SearchSeat (DecideSearch for the live-feed branch, DecideBoard
// for a stopped feed), exactly as the bare *sbsearch.Seat was before BP-16.
func TestHostedSeatIsASearchSeat(t *testing.T) {
	s, err := New(bots.Options{Seed: 3, Deps: bots.Deps{Cards: &cards.Registry{}}}, LiteAtk())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ss, ok := s.(searchseat.SearchSeat)
	if !ok {
		t.Fatalf("hosted seat is not searchseat.SearchSeat: the bench's feed branch would never run: %T", s)
	}
	if _, ok := ss.(seat.Seat); !ok {
		t.Fatalf("hosted seat is not seat.Seat: %T", ss)
	}
}

// UnwrapSeat must expose the wrapped search seat so registry.UnwrapSeat's
// chain reaches the inner sb-tactical (the planner hand-off, the fallback's
// *builtins.Seat assertion, the stats read).
func TestHostedSeatUnwrapsToTheSearchSeat(t *testing.T) {
	s, err := New(bots.Options{Seed: 3, Deps: bots.Deps{Cards: &cards.Registry{}}}, LiteAtk())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	uw, ok := s.(interface {
		UnwrapSeat() seat.Seat
	})
	if !ok {
		t.Fatalf("hosted seat has no UnwrapSeat: the bench's planner hand-off silently misses the inner seat")
	}
	if _, ok := uw.UnwrapSeat().(*sbsearch.Seat); !ok {
		t.Fatalf("UnwrapSeat = %T, want *sbsearch.Seat", uw.UnwrapSeat())
	}
}
