package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 118.6: a card with no mana cost (Forge's `ManaCost:no cost`) can't be
// cast by paying its mana cost -- only through an alternative cost or a
// "without paying its mana cost" permission. Gaea's Will used to be offered
// as a {0} plain cast, and under its own graveyard MayPlay static it recast
// itself from the graveyard forever (the corpus fuzzer's livelock).

func castOptionsFor(e *Engine, id state.ObjID) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == id {
			out = append(out, o)
		}
	}
	return out
}

func TestNoManaCostCardIsNotCastFromHand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	// Gaea's Will (Suspend 4--{G}) and Lotus Bloom (Suspend 3--{0}): neither
	// may be offered as a plain cast; Lotus Bloom's free Suspend (a special
	// action, not a cast) must stay offered.
	for _, name := range []string{"Gaea's Will", "Lotus Bloom"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing corpus card %s", name)
		}
		e := mayPlayBase(t)
		id := handCard(e, c, 0)
		opts := castOptionsFor(e, id)
		suspend := false
		for _, o := range opts {
			if o.Mode == "suspend" {
				suspend = true
				continue
			}
			t.Fatalf("%s: no-mana-cost card offered as %q (mode %q); only Suspend may be", name, o.Label, o.Mode)
		}
		if name == "Lotus Bloom" && !suspend {
			t.Fatalf("Lotus Bloom's {0} Suspend must stay offered, got %+v", opts)
		}
	}
}

func TestNoManaCostCardIsNotCastFromGraveyardByPaying(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	will, ok := reg.Lookup("Gaea's Will")
	if !ok {
		t.Fatal("missing corpus card Gaea's Will")
	}
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, "Name:Grave Grant\nManaCost:2\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.YouOwn | AffectedZone$ Graveyard | MayPlay$ True | Description$ x\nOracle:x\n")
	id := graveCard(e, will, 0, 0)
	if opts := castOptionsFor(e, id); len(opts) != 0 {
		t.Fatalf("no-mana-cost card offered from the graveyard under a paying may-play grant: %+v", opts)
	}
}

func TestNoManaCostCardIsCastWithoutPayingItsManaCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	will, ok := reg.Lookup("Gaea's Will")
	if !ok {
		t.Fatal("missing corpus card Gaea's Will")
	}
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, "Name:Free Grant\nManaCost:2\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.YouOwn | AffectedZone$ Graveyard | MayPlay$ True | MayPlayWithoutManaCost$ True | Description$ x\nOracle:x\n")
	id := graveCard(e, will, 0, 0)
	opts := castOptionsFor(e, id)
	if len(opts) != 1 || opts[0].Mode != "mayplay" {
		t.Fatalf("a without-paying-its-mana-cost permission must still offer the cast (CR 118.6), got %+v", opts)
	}
}
