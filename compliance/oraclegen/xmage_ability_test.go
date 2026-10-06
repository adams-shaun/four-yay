package oraclegen_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestXMageAbilityLoyaltyAndDuplicateCosts(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want []string
	}{
		{"Jace, Reality Sculptor", []string{"+1", "-3", "0"}},
		{"Chandra, Torch of Defiance", []string{"+1: Exile", "+1: Add", "-3", "-7"}},
		{"Chandra, Chill of Compliance", []string{"+1: Surveil", "+1: Add", "-X", "-6"}},
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s missing from corpus", tc.card)
		}
		got, why := oraclegen.XMageAbility(c.Faces[0])
		if why != "" {
			t.Fatalf("%s mapping ambiguous: %s", tc.card, why)
		}
		for i, prefix := range tc.want {
			if got[i] != prefix && !strings.HasPrefix(got[i], prefix) {
				t.Errorf("%s ability %d prefix = %q, want prefix %q", tc.card, i, got[i], prefix)
			}
		}
	}
}

func TestXMageAbilityIntrinsicLandMana(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Theorist's Sanctum")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("precondition: Theorist's Sanctum missing from corpus")
	}
	f := c.Faces[0]
	var intrinsic int
	found := false
	for i, sa := range f.Abilities {
		if sa.IsActivated() && sa.Line == "intrinsic: basic land mana" {
			intrinsic, found = i, true
			break
		}
	}
	if !found {
		t.Fatal("precondition: intrinsic Island mana ability is absent")
	}
	got, why := oraclegen.XMageAbility(f)
	if why != "" {
		t.Fatalf("mapping ambiguous: %s", why)
	}
	if got[intrinsic] != "{T}: Add {U}." {
		t.Fatalf("intrinsic mana prefix = %q, want %q", got[intrinsic], "{T}: Add {U}.")
	}
}

// A basic-typed land whose Oracle prints "{T}: Add {X}." (the Gates) has an
// injected intrinsic that DOES own a printed line: it must keep its ordinal
// mapping, not be dropped as a line-less intrinsic.
func TestXMageAbilityGateKeepsPrintedIntrinsicLine(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{
		"Gate of the Black Dragon", "Gate to Manorborn", "Gate to Seatower",
		"Gate to the Citadel", "Gate to Tumbledown",
	} {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s missing from corpus", name)
		}
		f := c.Faces[0]
		if len(f.Abilities) != 2 || f.Abilities[1].Line != "intrinsic: basic land mana" {
			t.Fatalf("precondition: %s should be one printed AB plus the injected intrinsic", name)
		}
		got, why := oraclegen.XMageAbility(f)
		if why != "" {
			t.Fatalf("%s mapping ambiguous: %s", name, why)
		}
		if got[1] != "{T}" {
			t.Errorf("%s intrinsic prefix = %q, want the printed line's cost {T}", name, got[1])
		}
		if !strings.HasPrefix(got[0], "{3}") {
			t.Errorf("%s printed ability prefix = %q, want its own cost", name, got[0])
		}
	}
}
