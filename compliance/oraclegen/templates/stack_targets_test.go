package templates

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// loadGenRegistry loads the corpus the generator needs. The corpus is
// gitignored; without it these tests skip rather than pass vacuously (a
// missing corpus is not a passing generator).
// loadGenRegistry is the process-shared corpus (cards.SharedCorpus): a
// registry is read-only after open, and a fresh decode per test cost every
// test seconds and ~400 MB, and re-derived every card-keyed memo.
func loadGenRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	reg, err := cards.SharedCorpus(filepath.Join("..", "..", "..", ".cards"))
	if err != nil {
		t.Fatalf("the generator needs the corpus (make fetch-cards compile-cards): %v", err)
	}
	return reg
}

func faceOf(t *testing.T, reg *cards.Registry, name string) *cards.Face {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("%s not in corpus", name)
	}
	return c.Faces[0]
}

// TestStackTargetSlotsAreEncoded: a slot whose target names a spell or
// ability on the stack must carry the @Stack suffix (so the fixture knows it
// needs a spell on the stack), and a slot that also names the battlefield
// must NOT (it is served by a battlefield candidate).
func TestStackTargetSlotsAreEncoded(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		card      string
		wantSlots []string
	}{
		{"Bilbo's Gambit", []string{"Card.inZoneStack@Stack"}},
		{"Swallowed by Leviathan", []string{"Card@Stack"}},
		// A mixed zone (TgtZone$ Stack,Battlefield, or Origin$ Battlefield,Stack)
		// is a battlefield slot, not a stack-only one.
		{"Jeskai Revelation", []string{"Permanent,Card.inZoneStack@Stack,Battlefield", "Any"}},
		{"Swat Away", []string{"Creature,Card.inZoneStack@Battlefield,Stack"}},
		{"Bolt Bend", []string{"Card,Emblem@Stack"}},
	}
	for _, tc := range cases {
		got := oraclegen.TargetSlots(faceOf(t, reg, tc.card))
		if strings.Join(got, "|") != strings.Join(tc.wantSlots, "|") {
			t.Errorf("%s: TargetSlots = %v, want %v", tc.card, got, tc.wantSlots)
		}
	}
}

// TestSlotIsStackOnly: SlotIsStack is true only for a stack-only slot.
func TestSlotIsStackOnly(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   bool
	}{
		{"Card.inZoneStack@Stack", true},
		{"Card@Stack", true},
		{"Permanent,Card.inZoneStack@Stack,Battlefield", false},
		{"Creature,Card.inZoneStack@Battlefield,Stack", false},
		{"Creature", false},
	} {
		if got := oraclegen.SlotIsStack(tc.filter); got != tc.want {
			t.Errorf("SlotIsStack(%q) = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

// TestStackTargetCardsGenerate: every stack-targeting card the earlier
// generator skipped must now get a scenario. A card whose slot is stack-only
// puts a spell on the stack for its target by casting a precast first (the
// extra cast step, CR 117.3c); a card whose slot also names the battlefield
// (Jeskai Revelation) needs no precast, its target is a permanent.
func TestStackTargetCardsGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	cards := []struct {
		name      string
		wantCasts int
	}{
		{"Bilbo's Gambit", 2},         // ChangeZone returns a spell
		{"Swallowed by Leviathan", 2}, // Surveil, then counter the spell
		{"Jeskai Revelation", 1},      // spell-or-permanent: permanent
		{"Choreographed Sparks", 2},   // charm copying a spell
		{"Bolt Bend", 2},              // change a single-target spell
		{"Return the Favor", 2},       // spree copy/change-target
	}
	for _, tc := range cards {
		it, skip := Generate(reg, tc.name)
		if skip != nil {
			t.Errorf("%s: %s", tc.name, skip.Reason)
			continue
		}
		casts := 0
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" {
				casts++
			}
		}
		if casts < tc.wantCasts {
			t.Errorf("%s: scenario has %d cast steps, want at least %d", tc.name, casts, tc.wantCasts)
		}
	}
}
