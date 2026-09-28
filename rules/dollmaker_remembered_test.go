package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Dedicated Dollmaker's ETB ("exile up to one other target nonland, nontoken
// permanent; its controller creates a token copy of it") reads DB$
// CopyPermanent | Defined$ Remembered after a RememberChanged$ exile. The
// trigger's own event capture (the Dollmaker itself, seeded into
// Ctx.Remembered) used to be copied too: one extra Dollmaker per ETB, and
// with no target the copy was the Dollmaker ALONE -- whose token's ETB did
// the same, an unbounded chain the corpus fuzzer hit as a hang.

func dollmakerGame(t *testing.T, withBear bool) (*Engine, *cards.Card, *cards.Card, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	doll, ok := reg.Lookup("Dedicated Dollmaker")
	if !ok {
		t.Fatal("missing corpus card Dedicated Dollmaker")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("missing corpus card Runeclaw Bear")
	}
	deck := append(mountainDeck(t, 40), doll, bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, deck}}))
	e.Advance()
	var bid state.ObjID
	if withBear {
		bid = crAbortMove(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	}
	e.pending = nil
	crAbortMove(t, e, 0, "Dedicated Dollmaker", state.ZBattlefield)
	e.priorityRound()
	if len(e.G.Stack) == 0 && (e.Pending() == nil || e.Pending().Kind != decision.KTarget) {
		t.Fatalf("Dedicated Dollmaker's ETB did not trigger: pending %+v", e.Pending())
	}
	return e, doll, bear, bid
}

// drainDollmaker answers the ETB's up-to-one target ask with want (0 = no
// target) and passes until the stack is empty, failing on a runaway chain.
func drainDollmaker(t *testing.T, e *Engine, want state.ObjID) {
	t.Helper()
	for i := 0; i < 50; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return
		}
		if d.Kind == decision.KTarget {
			choice := []int{}
			for _, o := range d.Options {
				if want != 0 && o.Obj == want {
					choice = []int{o.Index}
				}
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choice}); err != nil {
				t.Fatalf("target answer: %v", err)
			}
			continue
		}
		if len(e.G.Stack) == 0 {
			return
		}
		passUntilStackEmpty(t, e, 1)
	}
	t.Fatalf("Dedicated Dollmaker's ETB never settled: stack depth %d", len(e.G.Stack))
}

func tokenCopies(e *Engine, c *cards.Card) int {
	n := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.IsToken && o.Card == c && o.Zone == state.ZBattlefield {
			n++
		}
	}
	return n
}

func TestDollmakerCopiesOnlyTheExiledPermanent(t *testing.T) {
	t.Parallel()
	e, doll, bear, bid := dollmakerGame(t, true)
	drainDollmaker(t, e, bid)
	if got := e.G.Obj(bid).Zone; got != state.ZExile {
		t.Fatalf("bear zone %v, want exile", got)
	}
	if n := tokenCopies(e, bear); n != 1 {
		t.Fatalf("%d token copies of the exiled bear, want 1", n)
	}
	if n := tokenCopies(e, doll); n != 0 {
		t.Fatalf("%d token copies of Dedicated Dollmaker itself, want 0", n)
	}
}

func TestDollmakerWithNoTargetCopiesNothing(t *testing.T) {
	t.Parallel()
	e, doll, _, _ := dollmakerGame(t, false)
	drainDollmaker(t, e, 0)
	if n := tokenCopies(e, doll); n != 0 {
		t.Fatalf("%d token copies of Dedicated Dollmaker with no target, want 0", n)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CopyToken {
			t.Fatalf("a token copy was minted with nothing exiled: %+v", ev)
		}
	}
}
