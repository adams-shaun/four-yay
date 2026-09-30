package deck

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestManifestCarriesArchetype is the ticket's proof that the deck file's
// authoring archetype survives into the manifest (and therefore into
// view.View.OwnDeck): NewManifest stamps it, Clone preserves it, and it
// marshals only when declared. Without the Manifest.Archetype field this
// test cannot compile -- the field's absence is the failure the botobs
// ratchet measures from the type.
func TestManifestCarriesArchetype(t *testing.T) {
	alpha := parseManifestCard(t, "Alpha")
	m := NewManifest("deck", "control", []*cards.Card{alpha}, nil, nil)

	// The precondition the rest depends on: a non-empty archetype actually
	// reached the field. A signature that dropped it would leave "" here and
	// the equality below would be vacuous only if we asserted "".
	if m.Archetype != "control" {
		t.Fatalf("manifest archetype = %q, want %q", m.Archetype, "control")
	}

	clone := m.Clone()
	if clone.Archetype != "control" {
		t.Fatalf("clone archetype = %q, want %q", clone.Archetype, "control")
	}

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"archetype":"control"`) {
		t.Fatalf("manifest JSON = %s, want an archetype field", b)
	}

	// A deck that declares no archetype keeps the pre-field wire shape: the
	// empty string is omitted rather than marshalled as "".
	none, err := json.Marshal(NewManifest("plain", "", []*cards.Card{alpha}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(none), "archetype") {
		t.Fatalf("no-archetype manifest JSON = %s, want no archetype field", none)
	}
}
