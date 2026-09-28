package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestExchangeControlConfusionTrigger drives Confusion in the Ranks' real
// trigger end to end. Its body is
//
//	DB$ ExchangeControl | Defined$ TriggeredCard |
//	TargetingPlayer$ TriggeredCardController |
//	TargetsWithDefinedController$ NonTriggeredCardController |
//	ValidTgts$ Permanent | TargetsWithSharedCardType$ TriggeredCard
//
// so the entering permanent's controller (seat 1, NOT the trigger's
// controller) answers the target ask, and the two sides are the entering
// permanent and the chosen permanent. Before this ticket the handler
// rejected any TargetingPlayer$ value outright, so the trigger always
// emitted an unimplemented Note and never exchanged anything.
func TestExchangeControlConfusionTrigger(t *testing.T) {
	t.Parallel()
	confusion := corpusCard(t, "Confusion in the Ranks")
	face := confusion.Faces[0]
	sub := cards.ResolveSVar(face.SVars, "TrigExchangeControl")
	if sub == nil || sub.API != "ExchangeControl" || sub.Params["Defined"] != "TriggeredCard" ||
		sub.Params["TargetingPlayer"] != "TriggeredCardController" {
		t.Fatalf("precondition: Confusion's TrigExchangeControl no longer the TriggeredCard/TargetingPlayer shape: %+v", sub)
	}

	e := newSeats(t, 2)
	putCard := func(p state.PlayerID, c *cards.Card) state.ObjID {
		id := e.G.AddObject(c, p).ID
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
		if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
			t.Fatalf("fixture card %d zone = %v, want battlefield", id, z)
		}
		return id
	}

	// The seat-0 creature is placed BEFORE Confusion so it does not fire the
	// trigger itself; Confusion's own self-entering trigger (an enchantment)
	// is discarded, since at that point no other player controls anything.
	mine := putCard(0, card(t, "Name:MyBear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	e.pendingTriggers = nil
	e.pending = nil
	conf := putCard(0, confusion)
	e.pendingTriggers = nil
	e.pending = nil
	if e.G.Obj(conf).Zone != state.ZBattlefield || e.G.Obj(mine).Controller != 0 {
		t.Fatalf("fixture: Confusion zone=%v bear controller=%d", e.G.Obj(conf).Zone, e.G.Obj(mine).Controller)
	}

	// The entering permanent: a creature under seat 1, controlled by a
	// player other than Confusion's controller. It shares the card type
	// Creature with the seat-0 bear.
	theirs := putCard(1, card(t, "Name:TheirElf\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"))
	if e.G.Obj(theirs).Controller != 1 || e.G.Obj(mine).Controller != 0 {
		t.Fatal("precondition: entering permanent and the offered target must start on opposite seats")
	}

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want Confusion's target decision", d)
	}
	// TargetingPlayer$ TriggeredCardController: the ENTERING permanent's
	// controller answers, not Confusion's controller (seat 0).
	if d.Player != 1 {
		t.Fatalf("Confusion target ask posed to seat %d, want the entering permanent's controller (seat 1)", d.Player)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == mine {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Confusion offer %+v does not include the seat-0 bear sharing the Creature type", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit Confusion target: %v", err)
	}
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(mine).Controller; got != 1 {
		t.Fatalf("seat-0 bear controller after exchange = %d, want 1", got)
	}
	if got := e.G.Obj(theirs).Controller; got != 0 {
		t.Fatalf("entering creature controller after exchange = %d, want 0", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ExchangeControl") {
			t.Fatalf("ExchangeControl handler not dispatched (a valid TargetingPlayer$ exchange was refused): %+v", ev)
		}
	}
}

// TestExchangeControlGrantLayering pins the control-effect ORDERING the
// exchange's grants must join (CR 613.7). It brackets the exchange with two
// temporary steals of A and expires both:
//
//	g1: A 0 -> 2, EOT        (a prior steal)
//	exchange A<->B           (B is owned by 1; A -> 1, B -> 2)
//	g3: A 1 -> 0, EOT        (a later steal)
//
// At cleanup both EOT steals end and the exchange's PERMANENT grant is the
// latest survivor, so A returns to the controller the exchange gave it (1) --
// NOT to g1's Previous (0), the pre-steal owner. That difference is the
// observable proof the exchange registered a real grant in the layer order: a
// handler that only emitted ControlChange (no RegisterControl) leaves g1/g3
// as the only records, so expireControl's empty-survivor base (g1.Previous =
// 0) hands A back to seat 0 and this assertion fails.
func TestExchangeControlGrantLayering(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	a := putBattlefield(t, e, 0, "Name:A\nTypes:Creature\nPT:1/1\nOracle:x\n")
	b := putBattlefield(t, e, 1, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(a).Controller != 0 || e.G.Obj(b).Controller != 1 {
		t.Fatalf("precondition: the pair must start on opposite seats (A=%d B=%d)",
			e.G.Obj(a).Controller, e.G.Obj(b).Controller)
	}

	// g1: prior temporary steal of A, seat 0 -> seat 2, until end of turn.
	// This is the same ControlChange + RegisterControl pair effGainControl does.
	e.emit(events.Event{Kind: events.ControlChange, Obj: a, Player: 2})
	e.RegisterControl(effects.ControlGrant{Obj: a, ObjStamp: e.G.Obj(a).Timestamp,
		Previous: 0, Controller: 2, You: 0, Duration: effects.ControlDuration{EOT: true}})
	if e.G.Obj(a).Controller != 2 {
		t.Fatalf("precondition: prior EOT steal did not take A: controller %d, want 2", e.G.Obj(a).Controller)
	}

	// Exchange A (seat 2) and B (seat 1): A -> 1, B -> 2.
	eff := &cards.SA{Kind: "SP", API: "ExchangeControl", Params: map[string]string{}}
	effects.Resolve(e, &effects.Ctx{Controller: 0, Targets: []state.Target{{Obj: a}, {Obj: b}}}, eff)
	if e.G.Obj(a).Controller != 1 || e.G.Obj(b).Controller != 2 {
		t.Fatalf("exchange controllers = A:%d B:%d, want 1,2 (events=%+v)",
			e.G.Obj(a).Controller, e.G.Obj(b).Controller, e.L.Events)
	}
	// Precondition for the layered assertion: the exchange really appended a
	// grant for A on top of g1, not merely emitted an event.
	exchGrants := 0
	for _, g := range e.controlGrants {
		if g.Obj == a && g.Previous == 2 && g.Controller == 1 && g.Duration.Permanent() {
			exchGrants++
		}
	}
	if exchGrants != 1 {
		t.Fatalf("exchange did not register a permanent grant for A (grants=%+v)", e.controlGrants)
	}

	// g3: later temporary steal of A, seat 1 -> seat 0, until end of turn.
	e.emit(events.Event{Kind: events.ControlChange, Obj: a, Player: 0})
	e.RegisterControl(effects.ControlGrant{Obj: a, ObjStamp: e.G.Obj(a).Timestamp,
		Previous: 1, Controller: 0, You: 1, Duration: effects.ControlDuration{EOT: true}})
	if e.G.Obj(a).Controller != 0 {
		t.Fatalf("precondition: later EOT steal did not take A: controller %d, want 0", e.G.Obj(a).Controller)
	}

	// Both EOT steals end at cleanup; the exchange grant is the latest
	// survivor, so A returns to seat 1 (the exchanged controller), not seat 0.
	e.expireControl(controlAtCleanup)
	if got := e.G.Obj(a).Controller; got != 1 {
		t.Fatalf("A controller after both EOT grants expired = %d, want 1 (the exchange grant must be the latest survivor)", got)
	}
	if got := e.G.Obj(b).Controller; got != 2 {
		t.Fatalf("B controller after cleanup = %d, want 2 (its permanent exchange grant)", got)
	}
}
