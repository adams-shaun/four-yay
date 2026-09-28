package main

import (
	"io"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/seat"
)

// TestSpellbenchRegistryRoundTrip resolves every name the -spellbench
// workup spells through the registry and checks each builds the seat kind
// its name names. This runs in cmd/botbench because that is where "bot"
// (hosted policy) and "az" (azmcts wiring) are registered; the sb-* names
// are registered by the registry package itself.
func TestSpellbenchRegistryRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check func(seat.Seat) string
	}{
		{"sb-uniform", wantBuiltin(builtins.Uniform)},
		{"sb-heuristic", wantBuiltin(builtins.Heuristic)},
		{"sb-first", wantBuiltin(builtins.First)},
		{"sb-uniform-manual", wantBuiltin(builtins.Uniform)},
		{"sb-heuristic-manual", wantBuiltin(builtins.Heuristic)},
		{"sb-uniform-planned", wantBuiltin(builtins.Uniform)},
		{"sb-heuristic-planned", wantBuiltin(builtins.Heuristic)},
		{"bot", func(s seat.Seat) string {
			if s == nil {
				return "nil seat"
			}
			return "" // the hosted policy seat's concrete type is host's to choose
		}},
		{"az", func(s seat.Seat) string {
			if _, ok := s.(*azmcts.Seat); !ok {
				return "not *azmcts.Seat"
			}
			return ""
		}},
	} {
		if err := registry.CheckSpec(tc.name); err != nil {
			t.Fatalf("CheckSpec(%q): %v", tc.name, err)
		}
		s, err := registry.Build(tc.name, 19)
		if err != nil {
			t.Fatalf("Build(%q): %v", tc.name, err)
		}
		if s == nil {
			t.Fatalf("Build(%q) returned a nil seat", tc.name)
		}
		if msg := tc.check(s); msg != "" {
			t.Fatalf("Build(%q) = %T: %s", tc.name, s, msg)
		}
	}
}

// wantBuiltin returns a check asserting the seat is a *builtins.Seat playing
// the named policy.
func wantBuiltin(p builtins.Policy) func(seat.Seat) string {
	return func(s seat.Seat) string {
		bs, ok := s.(*builtins.Seat)
		if !ok {
			return "not *builtins.Seat"
		}
		if bs.Policy() != p {
			return "wrong policy"
		}
		return ""
	}
}

// TestSpellbenchUnknownNameListsRegistered runs the -spellbench flag surface
// itself: an unknown name fails the run and the error names the registered
// policies.
func TestSpellbenchUnknownNameListsRegistered(t *testing.T) {
	var errOut strings.Builder
	code := spellbenchExit(sbOpts{bots: "sb-nope,bot", out: t.TempDir()}, ".cards", 1, 10, 100, "", io.Discard, &errOut)
	if code != 1 {
		t.Fatalf("spellbenchExit = %d, want 1", code)
	}
	msg := errOut.String()
	if !strings.Contains(msg, `unknown policy "sb-nope"`) {
		t.Fatalf("stderr %q does not name the unknown policy", msg)
	}
	for _, name := range []string{"sb-uniform", "sb-heuristic", "sb-first", "bot", "az"} {
		if !strings.Contains(msg, name) {
			t.Fatalf("stderr %q does not list registered policy %q", msg, name)
		}
	}
}
