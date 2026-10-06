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
		// A shared self-referential cost is compared and extended in the
		// {this}-rewritten text, so the two abilities get distinct prefixes.
		{"Escape Tunnel", []string{"{T}, Sacrifice {this}: Search", "{T}, Sacrifice {this}: Target"}},
		{"Spike Tiller", []string{"{2}, Remove a +1/+1 counter from {this}: Put", "{2}, Remove a +1/+1 counter from {this}: Target"}},
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s missing from corpus", tc.card)
		}
		got, why := oraclegen.XMageAbility(c.Faces[0])
		if why != "" {
			t.Fatalf("%s mapping ambiguous: %s", tc.card, why)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s mapped %d abilities %q, want %d", tc.card, len(got), got, len(tc.want))
		}
		for i, prefix := range tc.want {
			if got[i] != prefix {
				t.Errorf("%s ability %d prefix = %q, want %q", tc.card, i, got[i], prefix)
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

// TestXMageAbilityUnchangedFaces pins the mapping main produced before the
// loyalty/shared-cost/intrinsic change for the activate template's test cards
// (compliance/oraclegen/templates/activate*_test.go) plus faces whose cost is
// only a string prefix of another line's cost or that name themselves in the
// rule text. Only an exactly shared cost is extended, so none of these move.
func TestXMageAbilityUnchangedFaces(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		{"Alacrian Jaguar", map[int]string{0: "Saddle 1"}},
		{"Axgard Cavalry", map[int]string{0: "{T}"}},
		{"Basilisk Collar", map[int]string{0: "Equip {2}"}},
		{"Cathar Commando", map[int]string{0: "{1}, Sacrifice {this}"}},
		{"Druid of the Cowl", map[int]string{0: "{T}"}},
		{"Edgar, Ancient Bloodlord", map[int]string{0: "{2}, Sacrifice another creature or planeswalker"}},
		{"Gallia, Tragic Host", map[int]string{0: "{4}{B}, Exile another creature card from your graveyard"}},
		{"Great Gilded Boat", map[int]string{0: "Crew 2"}},
		{"Hallway Heckler", map[int]string{0: "{T}, Discard a card"}},
		{"Hexhaven Dueling Arena", map[int]string{0: "{T}", 1: "{2}, {T}", 2: "{4}, {T}"}},
		{"Hungering Puppetbeast", map[int]string{0: "{1}, Sacrifice another artifact"}},
		{"Kithkeeper", map[int]string{0: "Tap three untapped creatures you control"}},
		{"Liliana the Faultless", map[int]string{0: "{1}, {T}, Discard a card"}},
		{"Llanowar Elves", map[int]string{0: "{T}"}},
		{"Marwyn, the Clearcutter", map[int]string{0: "{2}, {T}, Sacrifice an artifact or land"}},
		{"Murmuring Volume", map[int]string{0: "{T}", 1: "{2}, {T}, Discard a card"}},
		{"Nessian Asp", map[int]string{0: "{6}{G}"}},
		{"Sol Ring", map[int]string{0: "{T}"}},
		{"Solitary Cell", map[int]string{0: "{1}, {T}, Discard a legendary card"}},
		{"Sureshot Sower", map[int]string{0: "{3}{G}, Discard this card"}},
		{"Tenured Tethermage", map[int]string{0: "Tap two untapped artifacts you control"}},
		{"The Echoverse Fulcrum", map[int]string{0: "{5}, {T}, Exile {this}"}},
		{"Theoretical Necromancer", map[int]string{0: "{3}{B}, Exile this card from your graveyard"}},
		{"Tomik, Orzhov Lawmage", map[int]string{0: "{T}"}},
		{"Yoshimaru, Beloved Companion", map[int]string{0: "{6}"}},
		{"Abandoned Outpost", map[int]string{0: "{T}", 1: "{T}, Sacrifice {this}"}},
		{"Blinkmoth Nexus", map[int]string{0: "{T}", 1: "{1}", 2: "{1}, {T}"}},
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s missing from corpus", tc.card)
		}
		got, why := oraclegen.XMageAbility(c.Faces[0])
		if why != "" {
			t.Errorf("%s mapping ambiguous: %s", tc.card, why)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s mapped %q, want %q", tc.card, got, tc.want)
			continue
		}
		for i, want := range tc.want {
			if got[i] != want {
				t.Errorf("%s ability %d prefix = %q, want %q", tc.card, i, got[i], want)
			}
		}
	}
}
