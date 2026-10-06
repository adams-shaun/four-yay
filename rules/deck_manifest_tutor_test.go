package rules

// The engine-internal half of the seat-deck-manifest acceptance (ticket
// seat-deck-03-acceptance). The cross-interface contract leaf moved to
// seat/seatdeck_manifest_acceptance_test.go so the rules test binary no longer
// imports seat (and, through it, internal/policynet, internal/spellbench and
// internal/traceboard). This leaf keeps its home in package rules because it
// drives the engine's internal search fixture (searchEngine, addMana,
// castFixture, moveHiddenForTest) and does not need seat.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSeatDeckManifestTutorPromptIsAuthoritative pins the spec's tutor rule:
// the pending decision — the engine's legal candidate list — is the
// authority, and the manifest cannot make an absent card legal or expose the
// current library order. The fixture forces the two apart: a card the
// manifest names is drawn OUT of the library, so it is no longer a legal
// candidate, and the search must not offer it.
func TestSeatDeckManifestTutorPromptIsAuthoritative(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Demonic Tutor", "Grizzly Bears")
	manifest := e.OwnDeck(0)
	if manifest == nil || !manifestNames(manifest, "Grizzly Bears") {
		t.Fatalf("precondition: manifest does not name Grizzly Bears: %#v", manifest)
	}

	// Move every Grizzly Bears from the library to hand: the manifest still
	// counts them (it is genesis config), the library no longer holds them.
	bears := 0
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			e.moveHiddenForTest(id, state.ZLibrary, state.ZHand)
			bears++
		}
	}
	if bears == 0 {
		t.Fatal("precondition: no Grizzly Bears in the library to remove")
	}
	if !manifestNames(e.OwnDeck(0), "Grizzly Bears") {
		t.Fatal("precondition: manifest stopped naming a card moved out of the library")
	}

	tutor := searchMoveByName(t, e, "Demonic Tutor", state.ZHand)
	addMana(t, e, 0, "WUBRGCCCCCCCC")
	castFixture(t, e, tutor, -1)
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("precondition: no search KChoose: %+v", d)
	}

	// The authority: the option list is exactly the library's current
	// eligible cards, and no option is a Grizzly Bears. A candidate list
	// derived from the manifest would still offer the bears.
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(d.Options) != len(lib) {
		t.Fatalf("search offered %d options for a %d-card library; the prompt is not the library", len(d.Options), len(lib))
	}
	offered := optionNames(e, d)
	if slices.Contains(offered, "Grizzly Bears") {
		t.Fatalf("search offered a manifest card absent from the library: %v", offered)
	}
	for _, id := range optionIDs(d) {
		if !slices.Contains(lib, id) {
			t.Fatalf("search offered object %d which is not in the current library", id)
		}
	}
	// The manifest is unchanged by the whole exchange.
	if !manifestNames(e.OwnDeck(0), "Grizzly Bears") {
		t.Fatal("manifest changed when its card left the library")
	}
}

// moveHiddenForTest emits the zone move a hidden-zone card needs, then
// re-asks priority, mirroring search_library_test.go's searchMoveByName. It is
// fixture setup, not a rule under test.
func (e *Engine) moveHiddenForTest(id state.ObjID, from, to state.Zone) {
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
	e.pending = nil
	e.priorityRound()
}

func optionNames(e *Engine, d *decision.Decision) []string {
	out := make([]string, 0, len(d.Options))
	for _, o := range d.Options {
		obj := e.G.Obj(o.Obj)
		if obj == nil || obj.Face() == nil {
			out = append(out, "")
			continue
		}
		out = append(out, obj.Face().Name)
	}
	return out
}

// manifestNames reports whether m counts name in any of its lists.
func manifestNames(m *deck.Manifest, name string) bool {
	for _, r := range m.Main {
		if r.Name == name {
			return true
		}
	}
	for _, r := range m.Sideboard {
		if r.Name == name {
			return true
		}
	}
	return slices.Contains(m.Commanders, name)
}
