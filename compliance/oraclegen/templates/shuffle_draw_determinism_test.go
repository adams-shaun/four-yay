package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestWeftwalkingGeneratedScenarioDrawsUniformHand is the outcome pin behind
// the route census. Weftwalking's static level-B scenario shuffles its hand
// and graveyard into its library and then draws seven; gorge's shuffle order
// is seeded and XMage's is not, so before the uniform fixture the drawn hand
// differed between the engines (the driver-batch-20261006T140143Z flake named
// snapshots[2]/players[0]/hand[0]).
//
// The census test only asserts the item takes the "uniform library" route. A
// regression that filled the library with several distinct names would still
// look uniform by that measure yet put a random hand back on the board. This
// test runs the generated scenario through gorge and asserts the hand the
// shuffle actually produces: seven copies of the one shuffled name, whatever
// the shuffle order. It fails if the fixture is removed or the library stops
// being one name.
func TestWeftwalkingGeneratedScenarioDrawsUniformHand(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	card, ok := reg.Lookup("Weftwalking")
	if !ok {
		t.Fatal("precondition: Weftwalking is absent from the corpus")
	}

	var item oraclegen.Item
	for _, req := range levelb.Requirements(card) {
		if req.Gap != "" {
			continue
		}
		it, skip := GenerateB(reg, "Weftwalking", req)
		if skip != nil || it.ID != "Weftwalking/static#0.0/v1" {
			continue
		}
		item = it
	}
	if item.ID != "Weftwalking/static#0.0/v1" {
		t.Fatal("precondition: Weftwalking generated no static#0.0 level-B item")
	}
	if hasCompare(item.Compare, oraclegen.CompareHandCount) {
		t.Fatalf("the uniform fixture must remove the need for hand-count compare; compare = %v", item.Compare)
	}

	// Precondition: the scenario must carry a uniform, non-empty p0 library.
	// Otherwise there is nothing for the shuffle to draw deterministically and
	// the assertion below would pass vacuously on an untouched setup.
	lib := item.Scenario.Setup["p0"].Library
	if len(lib) == 0 {
		t.Fatal("precondition: p0 library is empty, so nothing was made uniform")
	}
	filler := lib[0]
	for _, n := range lib {
		if n != filler {
			t.Fatalf("precondition: p0 library holds %q and %q; a non-uniform library draws a random hand", filler, n)
		}
	}

	res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
	if err != nil {
		t.Fatalf("the generated Weftwalking scenario did not run: %v", err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge failed the scenario: %v", res.Fails)
	}

	// After the cast resolves, the hand must be seven copies of the filler.
	// Locate the resolution snapshot by its checkpoint (step 1 is the resolve)
	// rather than by index, so a step-list change names itself.
	sawPostShuffle := false
	for _, s := range res.Snapshots {
		if !strings.Contains(s.Checkpoint, "resolve") {
			continue
		}
		sawPostShuffle = true
		p0 := s.Players[0]
		if len(p0.Hand) != 7 {
			t.Errorf("%s: p0 hand has %d cards, want the drawn seven: %v", s.Checkpoint, len(p0.Hand), p0.Hand)
		}
		for _, n := range p0.Hand {
			if n != filler {
				t.Errorf("%s: p0 hand holds %q, want the uniform filler %q", s.Checkpoint, n, filler)
			}
		}
		if len(p0.Graveyard) != 0 {
			t.Errorf("%s: p0 graveyard = %v, want empty (shuffled back)", s.Checkpoint, p0.Graveyard)
		}
	}
	if !sawPostShuffle {
		t.Fatal("no post-shuffle resolve snapshot; the scenario never resolved the shuffle")
	}
}
