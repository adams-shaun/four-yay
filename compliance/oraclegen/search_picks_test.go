package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// A hidden library search gorge's fallback declined but which offered a legal
// card is queued back to gorge with that card (Terramorphic Expanse), and the
// re-run decision then scripts XMage's target with the card's name instead of
// the [target_skip] TargetCardInLibrary rejects. A search with no eligible
// card keeps the decline pair, and a declined non-search choose is untouched.
func TestSearchPicksForcesDeclinedLibrarySearch(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "activate"}, {Op: "resolve"}}}
	declined := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", Resume: "search",
		Options: 34, First: "Forest", GorgeKind: "choose", Min: 0, Max: 1}

	// Precondition: the decline pair is what an unforced decline scripts.
	if got := routed(t, []rules.OracleDecision{declined}, 2); !reflect.DeepEqual(got,
		[]XAnswer{{0, "choice", "no"}, {0, "target", "[target_skip]"}}) {
		t.Fatalf("precondition: unforced decline scripts %v, want the skip pair", got)
	}

	got, changed := SearchPicks(sc, []rules.OracleDecision{declined})
	if !changed {
		t.Fatal("a declined search with a legal card must queue a pick")
	}
	want := []Answer{{Kind: "choose", Pick: []string{"Forest"}}}
	if !reflect.DeepEqual(got.Steps[1].Answers, want) {
		t.Fatalf("answers = %+v, want %+v", got.Steps[1].Answers, want)
	}

	// The move leaves the activate step alone: the pick belongs to the step
	// that posed the search.
	if len(got.Steps[0].Answers) != 0 {
		t.Fatalf("activate step got answers %+v, want none", got.Steps[0].Answers)
	}

	// With the pick recorded, XMage is scripted the card by name, not a skip.
	picked := declined
	picked.Picks = []string{"Forest"}
	picked.PickIdx = []int{0}
	picked.PickRefs = []string{"p0:Forest"}
	picked.ObjectPicks = []string{"p0:Forest"}
	picked.PickKinds = []string{"search"}
	picked.Via = "answer"
	if got := routed(t, []rules.OracleDecision{picked}, 2); !reflect.DeepEqual(got,
		[]XAnswer{{0, "target", "Forest"}}) {
		t.Fatalf("picked search scripts %v, want the Forest target", got)
	}

	// No eligible card: keep the decline pair.
	empty := declined
	empty.Options, empty.First = 0, ""
	if _, changed := SearchPicks(sc, []rules.OracleDecision{empty}); changed {
		t.Fatal("a search with no eligible card must not queue a pick")
	}

	// A declined non-search choose is not this helper's business.
	other := declined
	other.Resume = "choice"
	if _, changed := SearchPicks(sc, []rules.OracleDecision{other}); changed {
		t.Fatal("a declined non-search choice must not be forced")
	}

	// An already-answered search has nothing to queue.
	if _, changed := SearchPicks(sc, []rules.OracleDecision{picked}); changed {
		t.Fatal("an answered search must not queue a second pick")
	}
}
