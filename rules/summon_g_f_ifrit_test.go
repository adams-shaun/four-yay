package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestSummonGFIfritChapterCostDiscardsBeforeDrawing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Summon: G.F. Ifrit")

	// Ensure a real card is in hand before the Saga enters and queues chapter I.
	var discard state.ObjID
	for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(zone, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				discard = id
				if zone != state.ZHand {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: zone, To: state.ZHand})
				}
				break
			}
		}
		if discard != 0 {
			break
		}
	}
	if discard == 0 || e.G.Obj(discard).Zone != state.ZHand {
		t.Fatal("precondition: no Grizzly Bears card available in hand to pay chapter I")
	}

	saga := searchMoveByName(t, e, "Summon: G.F. Ifrit", state.ZBattlefield)
	if o := e.G.Obj(saga); o == nil || o.Zone != state.ZBattlefield || o.Counter("LORE") != 1 {
		t.Fatalf("precondition: Ifrit did not enter with chapter-I lore: %+v", o)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: chapter I was not put on the stack")
	}

	// Pass priority only while the chapter remains on the stack. This stops at
	// its mid-resolution cost ask; without the fix, the body resolves freely
	// and the loop exits instead of drifting into a later cleanup decision.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while chapter I was on the stack")
		}
		if d.Kind != decision.KPriority {
			break
		}
		pass := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				pass = option.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision has no pass option: %+v", d)
		}
		submitChoices(t, e, pass)
	}
	d := e.Pending()
	pay := -1
	for _, option := range d.Options {
		if option.Kind == "trigger_cost_pay" {
			pay = option.Index
		}
	}
	if pay < 0 {
		kind := "none"
		if d != nil {
			kind = string(d.Kind)
		}
		t.Fatalf("chapter I resolved without offering its Discard<1/Card> cost: pending decision=%s, discard zone=%s", kind, e.G.Obj(discard).Zone)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("paying chapter I should ask for exactly one discard, got %+v", d)
	}
	var discardOption int = -1
	for _, option := range d.Options {
		if option.Obj == discard {
			discardOption = option.Index
			break
		}
	}
	if discardOption < 0 {
		t.Fatalf("the discard ask did not offer the precondition card %d: %+v", discard, d.Options)
	}
	submitChoices(t, e, discardOption)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(discard); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("cost card zone = %+v, want graveyard after chapter payment", got)
	}
	if got := e.G.Obj(saga); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("Ifrit left the battlefield while chapter I resolved: %+v", got)
	}
	discardAt, drawAt := -1, -1
	for i, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.Obj == discard && ev.To == state.ZGraveyard {
			discardAt = i
		}
		if ev.Kind == events.Draw && ev.Player == 0 {
			drawAt = i
		}
	}
	if discardAt < 0 {
		t.Fatal("chapter I did not move the chosen card as a discard cost")
	}
	if drawAt < 0 {
		t.Fatal("chapter I did not draw after paying its discard cost")
	}
	if discardAt >= drawAt {
		t.Fatalf("chapter body drew before paying: discard event index %d, draw event index %d", discardAt, drawAt)
	}
}
