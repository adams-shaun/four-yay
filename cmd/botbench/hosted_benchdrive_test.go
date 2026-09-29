package main

// BP-16 (spec §11): botbench drives the hosted search-family policies through
// the bench driver's SearchSeat feed branch. If an adapter (or the bench entry
// built from it) loses that surface, the bench falls back to the plain View
// branch and the policy plays something other than a search; these assertions
// fail loudly instead.

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestBenchSearchFamilyPoliciesAreSearchSeats(t *testing.T) {
	// Precondition: the tactical registry is wired, or sb-tactical /
	// sb-search-lite-atk cannot be built at all.
	setTacticalRegistry(testutil.CorpusRegistry(t))
	for _, name := range []string{"search", "az-redeal", "sb-search-lite-atk"} {
		ctor, ok := policies[name]
		if !ok {
			t.Fatalf("%s: no botbench policy", name)
		}
		s := ctor(7)
		if s == nil {
			t.Fatalf("%s: policy built a nil seat", name)
		}
		if _, ok := s.(searchseat.SearchSeat); !ok {
			t.Errorf("%s: bench seat is not searchseat.SearchSeat; the bench's feed branch would never run and the policy would play the plain View branch: %T", name, s)
		}
	}
}

// The SpellBench hooks (planner hand-off, the fallback's *builtins.Seat
// assertion, the stats read) still reach the wrapped seats through the
// registry's recursive UnwrapSeat, unchanged from the bare *builtins.Seat and
// *sbsearch.Seat the bench registered before BP-16. Without the adapters'
// UnwrapSeat the hand-off silently misses and the bench's sb-tactical plans
// its payments without the live engine.
func TestBenchHostedSBPoliciesUnwrapToTheBuiltin(t *testing.T) {
	setTacticalRegistry(testutil.CorpusRegistry(t))
	for _, name := range []string{"sb-tactical", "sb-search-lite-atk"} {
		s := policies[name](7)
		if s == nil {
			t.Fatalf("%s: policy built a nil seat", name)
		}
		inner, ok := registry.UnwrapSeat(s).(*builtins.Seat)
		if !ok {
			t.Fatalf("%s: registry.UnwrapSeat did not reach *builtins.Seat (got %v); the bench's planner hand-off would silently miss", name, registry.UnwrapSeat(s))
		}
		if inner.Policy() != builtins.Tactical {
			t.Fatalf("%s: unwrapped seat is %v, want Tactical", name, inner.Policy())
		}
	}
}

// A policy name the hosted registry does not know must still be constructible
// here: this is the negative half of the dispatch — the bench keeps its
// bench-native entries ("cast-profile" with its -profile overlay, the
// manual/planned arms, bot-auto-pay) beside bots.Names().
func TestBenchKeepsBenchNativePolicies(t *testing.T) {
	setTacticalRegistry(testutil.CorpusRegistry(t))
	// "az" is excluded: its literal needs a -az-world, which azFrontDoor
	// validates before any game (azcost_test.go covers that door).
	for _, name := range []string{"cast-profile", "cast-profile-auto-pay", "bot-auto-pay", "sb-tactical-planned"} {
		if _, ok := policies[name]; !ok {
			t.Errorf("%s: bench-native policy missing from the policies map", name)
		} else if s := policies[name](11); s == nil {
			t.Errorf("%s: policy built a nil seat", name)
		}
	}
}
