package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAskRejectsMisindexedDecision is the proof that the Option.Index / list
// position identity (finding bi) is enforced at the engine's choke point --
// Engine.ask -- and not only in the decision.New constructor that only two
// construction sites reach. A Decision whose Options[i].Index != i makes a
// client's intent and Chosen resolve a DIFFERENT option than the one named;
// ask is the one place every decision that can reach a seat flows (it sets
// the pending decision), so a guard there covers every construction site at
// once -- the struct literals in rules/combat.go, cast.go, stack.go, turn.go,
// trigger_queue.go and the two mid-resolution asks in effects/ -- whether or
// not a future edit remembers to call decision.New. This test drives a
// deliberately mis-indexed struct-literal Decision through ask and asserts
// it is rejected; it does not use decision.New, because a test that only
// calls New proves nothing about the sites that build the struct literal.
func TestAskRejectsMisindexedDecision(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 1, Names: names, Decks: decks})
	e.Advance()

	assertPanics := func(name string, d *decision.Decision) {
		t.Helper()
		before := e.Pending()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s: ask accepted a mis-indexed Decision without panicking", name)
			}
			// The rejection must happen before the decision is scheduled: ask
			// never assigned it a Seq and never made it pending, so a seat was
			// never offered it.
			if d.Seq != 0 {
				t.Fatalf("%s: ask assigned Seq %d to a mis-indexed Decision before panicking", name, d.Seq)
			}
			if e.Pending() != before {
				t.Fatalf("%s: ask changed the pending decision after panicking on a mis-indexed one", name)
			}
		}()
		e.ask(d)
	}

	// A single option whose Index has drifted off its position zero, built the
	// way the twelve struct-literal sites build their decisions.
	assertPanics("single drifted index", &decision.Decision{
		Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 7, Kind: "no", Label: "Don't pay", Player: 0}},
	})

	// The first option in a list, flowing options after it.
	assertPanics("first option drifted", &decision.Decision{
		Player: 0, Kind: decision.KTriggerOrder, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 4, Kind: "trigger", Label: "A", Player: 0},
			{Index: 1, Kind: "trigger", Label: "B", Player: 0},
		},
	})

	// A later option drifting away from the options before it.
	assertPanics("later option drifted", &decision.Decision{
		Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "keep", Label: "Keep", Player: 0},
			{Index: 9, Kind: "mulligan", Label: "Mulligan", Player: 0},
		},
	})
}

// TestAskAcceptsWellIndexedDecision pins the normal path: a Decision whose
// options carry the correct position indices still reaches ask unchanged, so
// the guard rejects only the broken state, never the good one.
func TestAskAcceptsWellIndexedDecision(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()

	d := &decision.Decision{Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes", Player: 0},
			{Index: 1, Kind: "no", Label: "No", Player: 0},
		}}
	e.ask(d)
	if e.Pending() != d {
		t.Fatalf("ask did not make the well-indexed decision pending: got %+v, want %+v", e.Pending(), d)
	}
	// Clean up so the mutated engine doesn't trip a later same-engine check.
	e.pending = nil
}
