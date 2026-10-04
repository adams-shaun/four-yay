package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGenerousPatronSupportPutsCountersAndDrawsOncePerBatch pins the Patron
// end to end: the ETB support asks "up to two other target creatures" per CR
// 701.41a's permanent half ("Support N" ON A PERMANENT means "up to N OTHER
// target creatures" — the Patron itself excluded, one counter per creature —
// not two on one), the
// answered put on the OPPONENT's creature queues exactly one draw trigger,
// and a later TWO-counter batch on the same creature draws exactly one more
// — one trigger per counter-placing event, never per counter.
func TestGenerousPatronSupportPutsCountersAndDrawsOncePerBatch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	patron, ok := reg.Lookup("Generous Patron")
	if !ok {
		t.Fatal("corpus has no Generous Patron")
	}
	e := layerEngine(t)
	own := onBoard(t, e, 0, "Name:Goblin Skirmisher\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	o := e.G.AddObject(patron, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))

	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZBattlefield})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after Patron entered = %d, want 1 (the ETB support trigger)", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	handAfterEntry := len(e.G.Zone(state.ZHand, 0))

	// The support ask: "up to two other target creatures" — the Patron must
	// NOT be offered (the Other exclusion), Min 0 ("up to").
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" {
		t.Fatalf("after the ETB trigger resolved: %+v, want the support counter_pick ask", d)
	}
	if d.Player != 0 || d.Min != 0 || d.Max != 2 {
		t.Fatalf("support ask player/range = seat %d %d..%d, want seat 0 0..2", d.Player, d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("support ask options = %d, want 2 (the goblin and the bear, never the Patron)", len(d.Options))
	}
	if d.Options[0].Obj != own || d.Options[1].Obj != bear {
		t.Fatalf("support options = [%d %d], want [%d %d] in zone order", d.Options[0].Obj, d.Options[1].Obj, own, bear)
	}
	for _, opt := range d.Options {
		if opt.Obj == o.ID {
			t.Fatal("the Patron itself was offered as a support target")
		}
	}
	// Support puts ONE counter per creature: answer with only the opponent's
	// creature (the YouDontCtrl half).
	submitChoices(t, e, d.Options[1].Index)
	kr5Settle(e)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("bear +1/+1 counters after support 2 on one creature = %d, want 1", got)
	}
	if got := e.G.Obj(own).Counter("P1P1"); got != 0 {
		t.Fatalf("own goblin +1/+1 counters = %d, want 0 (it was not chosen)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handAfterEntry+1 {
		t.Fatalf("hand after the support put = %d, want %d (exactly one draw for the opponent-creature put)", got, handAfterEntry+1)
	}

	// The batch pin: a real two-counter put on the same creature (one
	// CounterChange event, Amount 2) queues ONE trigger and draws ONE card.
	twoCounter := card(t, "Name:Double Count\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ PutCounter | Cost$ R | CounterType$ P1P1 | ValidTgts$ Creature | CounterNum$ 2\nOracle:x\n")
	sp := e.G.AddObject(twoCounter, 0)
	sp.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), sp.ID))
	addMana(t, e, 0, "R")
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after addMana: %+v, want seat 0's priority", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == sp.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the two-counter spell: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after casting: %+v, want the spell's target ask", d)
	}
	tIdx := -1
	for _, opt := range d.Options {
		if opt.Obj == bear {
			tIdx = opt.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("the bear was not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bear).Counter("P1P1"); got != 3 {
		t.Fatalf("bear +1/+1 counters after the two-counter put = %d, want 3", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handAfterEntry+2 {
		t.Fatalf("hand after the two-counter batch = %d, want %d (ONE draw for the batch, not one per counter)", got, handAfterEntry+2)
	}
}

// kr5Settle continues the engine after a kernel probe's answering Submit:
// the probe re-executes only its own function, so the turn loop that a real
// priority pass would run afterwards (queued triggers onto the stack, the
// next priority ask) runs here when nothing is posed.
func kr5Settle(e *Engine) {
	if e.Pending() == nil && !e.G.Over {
		e.Advance()
	}
}
