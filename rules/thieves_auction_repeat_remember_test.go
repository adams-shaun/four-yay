package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestThievesAuctionRepeatEachForgetShrinksOuterRemembered drives the real
// corpus Thieves' Auction chain -- RepeatDefined$ Remembered | RepeatPresent$
// Card wrapping a RepeatEach whose body is a ChooseCard with ForgetChosen$
// True -- and pins that the outer Repeat terminates. Before the
// rememberIteration fix the body's ForgetChosen$ did not shrink the outer
// set, so the Repeat's RepeatPresent$ Card gate held forever: the engine
// livelocked (the watcher aborts by panicking, so this test failed with a
// repeating choose/Note cycle at the same object).
//
// The card is looked up from the corpus, never inlined (the GPL licence rule).
func TestThievesAuctionRepeatEachForgetShrinksOuterRemembered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ta := lookup(t, reg, "Thieves' Auction")
	bearA := card(t, "Name:Forget Bear A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bearB := card(t, "Name:Forget Bear B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{ta, bearA}, []*cards.Card{bearB})

	spell := moveByName(t, e, 0, "Thieves' Auction", state.ZHand)
	idA := moveByName(t, e, 0, "Forget Bear A", state.ZBattlefield)
	idB := moveByName(t, e, 1, "Forget Bear B", state.ZBattlefield)
	// Precondition: two distinct nontoken permanents on two different
	// players' battlefields, so ChangeZoneAll remembers at least two cards
	// and the outer Repeat has a real budget to exhaust.
	if idA == idB {
		t.Fatalf("precondition: both bears resolved to object %d", idA)
	}
	for id, wantSeat := range map[state.ObjID]state.PlayerID{idA: 0, idB: 1} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != wantSeat {
			t.Fatalf("precondition: bear %d = %+v, want a battlefield creature controlled by seat %d", id, o, wantSeat)
		}
	}
	addMana(t, e, 0, "RRRRRRR") // Thieves' Auction costs 4RRR
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("precondition: Thieves' Auction not castable with 4RRR available: %+v", d.Options)
	}
	submitChoices(t, e, cast)

	// Drive decisions (pass on priority, first option otherwise) until the
	// spell has resolved (left the stack for the graveyard). The old code
	// livelocks inside one Submit and the watcher panics, so this loop is
	// never reached.
	const cap = 400
	var steps int
	for steps = 0; steps < cap; steps++ {
		if o := e.G.Obj(spell); o != nil && o.Zone == state.ZGraveyard {
			break
		}
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			passIdx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					passIdx = o.Index
				}
			}
			if passIdx < 0 {
				t.Fatalf("step %d: priority decision with no pass option: %+v", steps, d)
			}
			submitChoices(t, e, passIdx)
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("step %d: %s decision with no options", steps, d.Kind)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if steps >= cap {
		t.Fatalf("the Thieves' Auction resolution did not complete within %d decisions", cap)
	}
	if e.G.Obj(idA).Zone != state.ZBattlefield || e.G.Obj(idB).Zone != state.ZBattlefield {
		t.Fatalf("after the resolution bears are at %v/%v, want both back on the battlefield",
			e.G.Obj(idA).Zone, e.G.Obj(idB).Zone)
	}

	// The body's ForgetChosen$ must have fired for each card it chose, and the
	// fold back into the outer set must have carried those removals -- the
	// observable proof that the Repeat's remembered budget drained.
	var forgets, choseBack int
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "forget-remembered" {
			forgets++
		}
		if ev.Kind == events.MoveZone && ev.From == state.ZExile && ev.To == state.ZBattlefield {
			choseBack++
		}
	}
	if forgets < 2 {
		t.Fatalf("ForgetChosen$ fired %d times, want at least 2 (one per chosen card)", forgets)
	}
	if choseBack < 2 {
		t.Fatalf("only %d cards left exile for the battlefield, want at least 2", choseBack)
	}
}
