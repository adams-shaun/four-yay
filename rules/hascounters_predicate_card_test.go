package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestHasCountersPredicateUnlocksTargeting is the gameplay-visible leaf for
// the HasCounters `predicate` family: a real corpus card whose printed
// behaviour was broken when `Creature.HasCounters` matched NOTHING.
//
// Razorfin Abolisher reads:
//
//	{1}{U}, {T}: Return target creature with a counter on it to its owner's
//	hand.
//
// Its compiled ability is `A:AB$ ChangeZone | Cost$ 1 U T | ValidTgts$
// Creature.HasCounters`. With the HasCounters predicate implemented, the
// ability is offered and offers exactly the creatures that carry a counter;
// without it `Creature.HasCounters` matched NOTHING, so the ability had zero
// legal targets and was withheld entirely -- "{1}{U}, {T}: return target
// creature with a counter" could never be activated. The uncountered Grizzly
// Bears must never be offered.
func TestHasCountersPredicateUnlocksTargeting(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	abolisher := mustCorpusCard(t, reg, "Razorfin Abolisher") // {1}{U},{T}: return target creature with a counter on it
	bearA := mustCorpusCard(t, reg, "Grizzly Bears")
	bearB := mustCorpusCard(t, reg, "Grizzly Bears")

	cfg := Config{Seed: 3, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{abolisher, bearA, bearB}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
	}
	e := New(cfg)
	e.Advance()

	// Put all three on the battlefield with logged moves. The two Bears share
	// a name, so collect ids by position in the placement order rather than
	// by name.
	var srcID, counteredID, plainID state.ObjID
	for _, c := range []*cards.Card{abolisher, bearA, bearB} {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner == 0 && o.Card == c {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				if c == abolisher {
					srcID = o.ID
				} else if counteredID == 0 {
					counteredID = o.ID
				} else {
					plainID = o.ID
				}
				break
			}
		}
	}
	if srcID == 0 || counteredID == 0 || plainID == 0 {
		t.Fatalf("setup failed to place three distinct objects: src=%d countered=%d plain=%d", srcID, counteredID, plainID)
	}

	// Summoning sickness only gates a {T} activation, so clear it on the
	// source (as if it had been under its controller's control since the
	// start of the turn), and put a +1/+1 counter on one target via the
	// events path (never mutate object state directly).
	e.G.Obj(srcID).SummonSick = false
	e.emit(events.Event{Kind: events.CounterChange, Obj: counteredID, Counter: "P1P1", Amount: 1})

	// Fund {1}{U} and drive to the priority decision.
	addMana(t, e, 0, "CU")
	opt := abilityOption(t, e, srcID, 0)
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision after activating, got %+v", d)
	}
	counteredIdx, offeredPlain, offeredSrc := -1, false, false
	for _, o := range d.Options {
		switch o.Obj {
		case counteredID:
			counteredIdx = o.Index
		case plainID:
			offeredPlain = true
		case srcID:
			offeredSrc = true
		}
	}
	if counteredIdx < 0 {
		t.Fatalf("the countered creature must be offered as a target: %+v", d.Options)
	}
	if offeredPlain {
		t.Fatalf("the uncountered Grizzly Bears must not be offered: %+v", d.Options)
	}
	if offeredSrc {
		t.Fatalf("Razorfin Abolisher (no counter) must not be offered as its own target: %+v", d.Options)
	}

	// Complete the activation so the ability resolves; the engine treats the
	// source as spendable (fixture setup, like combat_test's direct IsAttacking
	// assignment), and the return-to-hand effect is observable.
	submitChoices(t, e, counteredIdx)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(counteredID).Zone; z != state.ZHand {
		t.Fatalf("countered creature zone = %v, want hand (Razorfin's return-to-hand resolved)", z)
	}
}
