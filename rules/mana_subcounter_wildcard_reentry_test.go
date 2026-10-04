package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestManaSubCounterWildcardCompletesOnReentry drives the announced/anchored
// SubCounter stage's re-entry shape: a mana ability whose wildcard
// RemoveAnyCounter<2/Any/Creature> part asks for its first unit (two units
// legal), then finds exactly one unit left on re-entry from the answer and
// takes it without asking. Before E7 flow slice 3 this silently dropped the
// activation twice over: the re-entry reserved the permanent the first unit
// came from (so no candidate was left and the payment was dropped), and past
// that the stage read the stale pending-choice marker (still naming the
// answered sub-counter ask) as "asked", parking the election with no
// decision posed. The cost must settle, the mana be added and no choice stay
// parked.
func TestManaSubCounterWildcardCompletesOnReentry(t *testing.T) {
	t.Parallel()
	const src = "Name:Wild Well\nManaCost:0\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ T RemoveAnyCounter<2/Any/Creature> | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"
	const bear = "Name:Bear A\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, srcID := newFixtureDeck(t, 7811, src, bear)
	bearID := putCreature(t, e, 0, bear)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "CHARGE", Amount: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: srcID, From: e.G.Obj(srcID).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("fixture: pool starts at %d, want 0", e.G.Players[0].Pool.Total())
	}
	submitChoices(t, e, activateOption(t, e, srcID))

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("first wildcard unit ask = %+v, want a two-unit KChoose", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearID && o.Counter == "P1P1" {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("no P1P1 unit option: %+v", d.Options)
	}
	submitChoices(t, e, pick)

	if e.ManaCost != nil {
		t.Fatalf("mana cost election still parked after the last unit auto-picked (choosing=%d, pending=%+v)", e.choosing, e.Pending())
	}
	if e.manaCostChoicePending() {
		t.Fatalf("stale mana cost marker %d left after the cost settled", e.choosing)
	}
	if o := e.G.Obj(bearID); o.Counter("P1P1") != 0 || o.Counter("CHARGE") != 0 {
		t.Fatalf("counters after payment = P1P1 %d CHARGE %d, want 0/0", o.Counter("P1P1"), o.Counter("CHARGE"))
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool total = %d, want 1", got)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the activation, pending = %+v, want priority", d)
	}
	replayCheck(t, e, cfg)
}
