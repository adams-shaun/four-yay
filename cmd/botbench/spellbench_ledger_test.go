package main

// Fix-round tests (findings-r2.md): sbDisplayName must mark a COMPOSED az
// spec as clairvoyant (the old bare-name check left `az+passguard` unmarked
// in the ledger and untaged by rate.py), and the runner's refused-answer
// fallback / stats collection must reach the builtin underneath a decorated
// sb-* spec (the registry's Unwrapper contract).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/seat"
)

// TestSpellbenchDisplayNameMarksComposedAz asserts the clairvoyant marker
// covers az and every az-composed spec, and leaves every other spec's
// ledger name untouched.
func TestSpellbenchDisplayNameMarksComposedAz(t *testing.T) {
	marker := fmt.Sprintf("az-clairvoyant-sims%d", azCfg.Search.Sims)
	if marker == "az" || !strings.HasPrefix(marker, "az-") {
		t.Fatalf("precondition: marker %q does not carry the rate.py name tag", marker)
	}
	for _, tc := range []struct{ spec, want string }{
		{"az", marker},
		{"az+passguard", marker + "+passguard"},
		// a multi-decorator spec keeps the whole tail
		{"az+passguard+passguard", marker + "+passguard+passguard"},
		// every other spec's name is itself
		{"bot", "bot"},
		{"bot+passguard", "bot+passguard"},
		{"sb-uniform", "sb-uniform"},
		{"sb-heuristic-planned", "sb-heuristic-planned"},
	} {
		if got := sbDisplayName(tc.spec); got != tc.want {
			t.Errorf("sbDisplayName(%q) = %q, want %q", tc.spec, got, tc.want)
		}
	}
	if got := sbDisplayName("az+passguard"); !strings.HasPrefix(got, "az-") {
		t.Errorf("sbDisplayName(az+passguard) = %q: no leading az- tag, rate.py would not classify it as a search agent", got)
	}
}

// TestSpellbenchFallbackReachesWrappedBuiltin asserts the seat the fallback
// and stats collection inspect is the builtin underneath the decoration:
// sb-first+passguard unwraps to the *builtins.Seat playing First, so a
// decorated sb-* spec keeps the refused-answer fallback exactly as the bare
// name has it.
func TestSpellbenchFallbackReachesWrappedBuiltin(t *testing.T) {
	s, err := registry.Build("sb-first+passguard", 7)
	if err != nil {
		t.Fatalf("Build(sb-first+passguard): %v", err)
	}
	// Precondition: the decorated seat is NOT itself the builtin, so this
	// test can only pass through the unwrap.
	if _, ok := s.(*builtins.Seat); ok {
		t.Fatalf("sb-first+passguard built a bare *builtins.Seat; the fixture does not decorate")
	}
	if _, ok := s.(registry.Unwrapper); !ok {
		t.Fatalf("passguardSeat does not implement registry.Unwrapper; the fallback cannot see through it")
	}
	b, ok := registry.UnwrapSeat(s).(*builtins.Seat)
	if !ok {
		t.Fatalf("UnwrapSeat(sb-first+passguard) = %T, want *builtins.Seat", registry.UnwrapSeat(s))
	}
	if b.Policy() != builtins.First {
		t.Fatalf("unwrapped seat plays %v, want First", b.Policy())
	}
	// An undecorated seat passes through unchanged.
	if got := registry.UnwrapSeat(b); got != seat.Seat(b) {
		t.Fatalf("UnwrapSeat on an undecorated seat returned a different seat")
	}
}
