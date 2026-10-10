package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticValidAfterStackLibraryTop: Glarb, Calamity's Augur's "you may play
// lands and cast spells with mana value 4 or greater from the top of your
// library" (MayPlay$ True, ValidAfterStack$ Spell.cmcGE4) skipped with the
// generic reason because the compiled-predicate sidecar answered a definite No
// for `Spell.cmcGE4` off the stack and ignored the AsStack override, so a real
// game never offered the cast (effects/compiled_predicate.go). The row is now
// observed as the offered cast of a library-top probe. The precondition
// asserts the offered probe really sits on top of the library and has mana
// value 4 or more: a cheaper probe cannot satisfy the printed gate, so the
// test cannot pass on a grant that ignores ValidAfterStack.
func TestStaticValidAfterStackLibraryTop(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const card, key = "Glarb, Calamity's Augur", "static#0.1"
	it, skip := zoneItem(t, card, key)
	if skip != nil {
		t.Fatalf("%s %s skipped: %s", card, key, skip.Reason)
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
	}
	if last.Expect[0].Offered.Kind != "cast" {
		t.Fatalf("offered kind = %q, want a cast of a spell", last.Expect[0].Offered.Kind)
	}
	probe := strings.TrimPrefix(last.Expect[0].Offered.Card, "p0:")
	if !slices.Contains(it.Setup["p0"].LibraryTop, probe) {
		t.Fatalf("offered probe %q is not on top of p0's library: %v", probe, it.Setup["p0"].LibraryTop)
	}
	pc, ok := reg.Lookup(probe)
	if !ok {
		t.Fatalf("probe %s not in the corpus", probe)
	}
	if mv := pc.Faces[0].ManaValue(); mv < 4 {
		t.Fatalf("offered probe %s has mana value %d, want >= 4 (the printed ValidAfterStack gate)", probe, mv)
	}
}
