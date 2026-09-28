package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// peacekeeperUnlessFixture puts real corpus Peacekeeper ("At the beginning of
// your upkeep, sacrifice Peacekeeper unless you pay {1}{W}"), Farrelite Priest
// ("{1}: Add {W}", a non-tapping pool converter) and `plains` Plains on seat
// 0's battlefield, drives to seat 0's next upkeep, elects to pay, and returns
// the engine at the resulting unless_mana window with the Peacekeeper and
// Priest ids.
func peacekeeperUnlessFixture(t *testing.T, plains int) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	extras := []*cards.Card{lookup(t, reg, "Peacekeeper"), lookup(t, reg, "Farrelite Priest")}
	for i := 0; i < plains; i++ {
		extras = append(extras, lookup(t, reg, "Plains"))
	}
	e := corpusEngine(t, reg, extras, nil)
	pk := moveByName(t, e, 0, "Peacekeeper", state.ZBattlefield)
	priest := moveByName(t, e, 0, "Farrelite Priest", state.ZBattlefield)
	for i := 0; i < plains; i++ {
		moveByName(t, e, 0, "Plains", state.ZBattlefield)
	}
	for i := 0; i < 400; i++ {
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil || e.G.Over {
			t.Fatal("engine stalled before Peacekeeper's upkeep unless-pay ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			submitChoices(t, e, d.Options[0].Index) // pay
			if w := e.Pending(); w == nil || w.ResumeKind != "unless_mana" {
				t.Fatalf("paying Peacekeeper's {1}{W} from an empty pool did not open the unless_mana window: %+v", w)
			}
			return e, pk, priest
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %v before Peacekeeper's upkeep ask: %+v", d.Kind, d)
		}
		castFirst(t, e, "pass")
	}
	t.Fatal("Peacekeeper's upkeep unless-pay ask was never posed")
	return nil, 0, 0
}

// TestUnlessManaWindowClosesOncePayable pins the engine half of the cardfuzz
// batch3 lines 9/11 livelock: the mid-resolution unless-cost window offers
// sources only while the pool cannot pay the charge (the predicate that
// opened it, and the one the cast and cumulative windows close on), and each
// offered source carries the beyond-tap cost marker every other "activate"
// offer carries, so an answerer can tell the Priest's non-tapping "{1}" from
// a Plains' tap.
func TestUnlessManaWindowClosesOncePayable(t *testing.T) {
	t.Parallel()
	e, pk, priest := peacekeeperUnlessFixture(t, 3)
	tapPlains := func() {
		t.Helper()
		d := e.Pending()
		for _, o := range d.Options {
			if o.Kind == "activate" && e.G.Obj(o.Obj).Face().Name == "Plains" {
				if o.Cost != "" {
					t.Fatalf("Plains' window option carries cost marker %q, want none", o.Cost)
				}
				submitChoices(t, e, o.Index)
				return
			}
		}
		t.Fatalf("no untapped Plains offered: %+v", d.Options)
	}
	tapPlains()
	// One {W} does not pay {1}{W}: the window stays open, and now that the
	// pool holds mana the Priest's "{1}" is activatable too.
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_mana" {
		t.Fatalf("one {W} does not pay {1}{W}; the window must stay open: %+v", d)
	}
	sawPriest := false
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == priest {
			sawPriest = true
			if o.Cost != "1" {
				t.Fatalf("Farrelite Priest's window option carries cost marker %q, want \"1\"", o.Cost)
			}
		}
	}
	if !sawPriest {
		t.Fatalf("the unless_mana window omitted Farrelite Priest with {W} floating: %+v", d.Options)
	}
	tapPlains()
	d = e.Pending()
	if d == nil || d.ResumeKind != "unless_mana" || len(d.Options) != 1 || d.Options[0].Kind != "done" {
		t.Fatalf("the pool pays {1}{W}; the window must offer only Done, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(pk).Zone; z != state.ZBattlefield {
		t.Fatalf("Peacekeeper's {1}{W} was paid but it went to %s", z)
	}
}

// TestBotNeverLoopsAConverterInAPaymentWindow is the bot half, end to end
// through botpolicy.Decide, with two Plains. Once the first Plains' {W}
// floats, the window offers Farrelite Priest ("{1}: Add {W}") ahead of the
// second Plains (battlefield order), and the Priest pays its {1} with that
// {W} and adds it straight back. The bot used to take the first "activate"
// option for as long as one was offered -- the Priest is never tapped, so
// forever (the cardfuzz batch3 lines 9/11 cycle). It must never activate the
// Priest, tap both Plains and pay: Peacekeeper survives.
func TestBotNeverLoopsAConverterInAPaymentWindow(t *testing.T) {
	t.Parallel()
	e, pk, priest := peacekeeperUnlessFixture(t, 2)
	r := newTestBot(1).r
	for i := 0; e.Pending() != nil && e.Pending().ResumeKind == "unless_mana"; i++ {
		if i > 10 {
			t.Fatal("the bot is still answering the unless_mana window after 10 decisions")
		}
		d := e.Pending()
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit %+v: %v", in, err)
		}
	}
	activations := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ManaActivate && ev.Obj == priest
	})
	if activations != 0 {
		t.Fatalf("the bot activated the net-zero converter %d time(s) in the payment window", activations)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(pk).Zone; z != state.ZBattlefield {
		t.Fatalf("two Plains pay {1}{W}, but Peacekeeper went to %s", z)
	}
}
