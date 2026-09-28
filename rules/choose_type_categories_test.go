package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task ct1: the as-enters ChooseType ask (the ETBReplacement path) is
// category-aware too. Before the fix its "type" arm built the owner-scoped
// creature-type list for EVERY Type$ category, so a real corpus Basic Land
// carrier asked the wrong question. These tests drive the real carriers
// through the entry boundary and assert the offered list is the category's
// own.

// etbTypeChoiceLabels drives id from the hand to the battlefield and returns
// the posed ETB type decision, failing if the ask did not happen or offered
// no options.
func etbTypeChoiceLabels(t *testing.T, e *Engine, id state.ObjID) []string {
	t.Helper()
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatalf("precondition: card zone = %s, want hand", e.G.Obj(id).Zone)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.ResumeKind != "etb" || len(d.Options) == 0 || d.Options[0].Kind != "type" {
		t.Fatalf("entry ask = %+v, want a non-empty ETB type choice", d)
	}
	out := make([]string, 0, len(d.Options))
	for _, o := range d.Options {
		out = append(out, o.Label)
	}
	return out
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// TestETBBasicLandTypeChoiceOffersTheFiveBasicLandTypes uses the real corpus
// carrier Realmwright ("As Realmwright enters, choose a basic land type") and
// asserts the entry ask offers the five basic land types, not creature
// types.
func TestETBBasicLandTypeChoiceOffersTheFiveBasicLandTypes(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Realmwright"))
	id := e.G.Zone(state.ZHand, 0)[0]
	labels := etbTypeChoiceLabels(t, e, id)
	want := []string{"Forest", "Island", "Mountain", "Plains", "Swamp"}
	if len(labels) != len(want) {
		t.Fatalf("Basic Land entry options = %v, want the five basic land types %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("Basic Land entry options = %v, want %v", labels, want)
		}
	}
	if containsLabel(labels, "Human") {
		t.Fatalf("Basic Land entry ask leaked the creature fallback: %v", labels)
	}
}

// TestETBPlaneswalkerTypeChoiceOffersPlaneswalkerTypes uses the real corpus
// carrier Deification ("As Deification enters, choose a planeswalker type")
// and asserts the entry ask offers planeswalker subtypes, never a creature
// type. It also asserts the answer is recorded on the entering object.
func TestETBPlaneswalkerTypeChoiceOffersPlaneswalkerTypes(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Deification"))
	id := e.G.Zone(state.ZHand, 0)[0]
	labels := etbTypeChoiceLabels(t, e, id)
	if !containsLabel(labels, "Jace") || !containsLabel(labels, "Chandra") {
		t.Fatalf("Planeswalker entry options = %v, want the planeswalker vocabulary", labels)
	}
	if containsLabel(labels, "Human") || containsLabel(labels, "Goblin") {
		t.Fatalf("Planeswalker entry ask leaked a creature type: %v", labels)
	}
	// Answer the first option and assert it is recorded as the chosen type.
	d := e.Pending()
	first := d.Options[0].Label
	submitChoices(t, e, d.Options[0].Index)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("after entry answer zone = %s, want battlefield", o.Zone)
	}
	if o.ChosenType != first {
		t.Fatalf("ChosenType = %q, want the answered %q", o.ChosenType, first)
	}
}
