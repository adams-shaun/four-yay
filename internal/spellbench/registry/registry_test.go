package registry

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
)

// TestRegistrySpellbenchNamesBuild resolves every sb-* name the -spellbench
// round robin spells and checks each builds the builtin seat its name names
// (the policy inside is observable; the mana surface is not, and is pinned
// end to end by cmd/botbench's digest goldens).
func TestRegistrySpellbenchNamesBuild(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy builtins.Policy
	}{
		{"sb-uniform", builtins.Uniform},
		{"sb-heuristic", builtins.Heuristic},
		{"sb-first", builtins.First},
		{"sb-uniform-manual", builtins.Uniform},
		{"sb-heuristic-manual", builtins.Heuristic},
		{"sb-uniform-planned", builtins.Uniform},
		{"sb-heuristic-planned", builtins.Heuristic},
	} {
		s, err := Build(tc.name, 19)
		if err != nil {
			t.Fatalf("Build(%q): %v", tc.name, err)
		}
		bs, ok := s.(*builtins.Seat)
		if !ok {
			t.Fatalf("Build(%q) = %T, want *builtins.Seat", tc.name, s)
		}
		if bs.Policy() != tc.policy {
			t.Fatalf("Build(%q) policy = %v, want %v", tc.name, bs.Policy(), tc.policy)
		}
	}
}

// TestRegistryUnknownNameListsRegistered errors on an unknown name and the
// error names the registered policies -- the -spellbench flag error surfaces
// this message verbatim.
func TestRegistryUnknownNameListsRegistered(t *testing.T) {
	_, err := Build("sb-nope", 19)
	if err == nil {
		t.Fatal("Build(\"sb-nope\") succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "unknown policy \"sb-nope\"") {
		t.Fatalf("error %q does not name the unknown policy", err)
	}
	for _, name := range []string{"sb-uniform", "sb-heuristic", "sb-first", "bot"} {
		// bot is registered only by cmd/botbench; here it is legitimately
		// absent, so the assertion is over the sb-* names this package
		// registers.
		if name == "bot" {
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error %q does not list registered policy %q", err, name)
		}
	}
	if Has("sb-nope") || Has("policynet") {
		t.Fatal("Has accepted an unregistered name")
	}
}

// TestRegistrySpecParsing covers the spec grammar: a composed spec builds,
// a spec with no base errors, and an unknown decorator errors listing the
// registered decorators.
func TestRegistrySpecParsing(t *testing.T) {
	s, err := Build("sb-uniform+passguard", 19)
	if err != nil {
		t.Fatalf("Build(\"sb-uniform+passguard\"): %v", err)
	}
	if s == nil {
		t.Fatal("Build(\"sb-uniform+passguard\") returned a nil seat")
	}
	if _, ok := s.(seat.Seat); !ok {
		t.Fatalf("Build(\"sb-uniform+passguard\") = %T, not a seat.Seat", s)
	}
	if !Has("sb-uniform+passguard") {
		t.Fatal("Has rejected a valid spec")
	}
	for _, tc := range []struct {
		spec, wantFragment string
	}{
		{"+passguard", "has no base policy"},
		{"sb-uniform+nope", "unknown decorator \"nope\""},
		{"sb-uniform+", "has an empty decorator"},
		{"nope+passguard", "unknown policy \"nope\""},
		// a policy name is not a decorator, and a decorator name is not a
		// policy: one namespace.
		{"sb-uniform+sb-uniform", "unknown decorator \"sb-uniform\""},
		{"passguard", "unknown policy \"passguard\""},
	} {
		err := CheckSpec(tc.spec)
		if err == nil {
			t.Fatalf("CheckSpec(%q) succeeded, want an error", tc.spec)
		}
		if !strings.Contains(err.Error(), tc.wantFragment) {
			t.Fatalf("CheckSpec(%q) error %q lacks %q", tc.spec, err, tc.wantFragment)
		}
		// The decorator errors list the registered decorators; the
		// base-policy errors list the registered policies.
		if strings.Contains(tc.wantFragment, "decorator") && !strings.Contains(err.Error(), "passguard") {
			t.Fatalf("CheckSpec(%q) error %q does not list the registered decorators", tc.spec, err)
		}
		if Has(tc.spec) {
			t.Fatalf("Has accepted invalid spec %q", tc.spec)
		}
	}
}
