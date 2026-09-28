package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// UnlessPayer$ Player.targetedBy and the Draw<N/Spec> unless-cost component,
// on Kuroki, Thief of Talents's real end-step trigger: "target opponent may
// draw four cards. If they do, look at that player's hand and you may cast a
// spell from their hand without paying its mana cost. If they don't, put two
// +1/+1 counters on NICKNAME." TrigReveal carries UnlessCost$
// Draw<4/Player.targetedBy> | UnlessPayer$ Player.targetedBy |
// UnlessSwitched$ True — paying CAUSES the reveal/cast body, and the payer
// is the player the ability targeted.

// driveKurokiTrig fires Kuroki's real TrigReveal by seeding the ordinary
// stack events — the trigger pushed for seat 0, its target bound to the
// targeted opponent (seat 1) — the same way
// TestUnlessCostTresserhornPaysSacLifeAndDraw drives its carrier. The
// trigger is not reached through Phase$ matching: this build's Phase$
// matcher only admits spellings that are substrings of the hyphenated step
// name, so TrigReveal's "End of Turn" (927 corpus trigger lines) stays a
// documented approximation and the test does not depend on it.
func driveKurokiTrig(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 741)
	kuroki := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Kuroki, Thief of Talents"))
	sa := cards.ResolveSVar(e.G.Obj(kuroki).Face().SVars, "TrigReveal")
	if sa == nil || sa.Params["UnlessPayer"] != "Player.targetedBy" || sa.Params["UnlessCost"] != "Draw<4/Player.targetedBy>" {
		t.Fatalf("Kuroki TrigReveal = %+v, want the targeted unless-draw shape", sa)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: kuroki, Player: 0, Amount: 0})
	ability := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, Player: 1, Amount: 1})
	e.resolveTop()
	if d := e.Pending(); d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the unless-pay ask for seat 1", d)
	}
	return e, kuroki
}

// TestUnlessDrawCostKurokiPay pins the binding and the payment: the pay
// decision is offered to the TARGETED opponent (seat 1, not Kuroki's
// controller), and paying draws that opponent exactly four cards — the cost
// the corpus spells as Draw<4/Player.targetedBy>.
func TestUnlessDrawCostKurokiPay(t *testing.T) {
	t.Parallel()
	e, kuroki := driveKurokiTrig(t)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want the unless-pay ask", d)
	}
	if d.Player != 1 {
		t.Fatalf("payer = seat %d, want the targeted opponent seat 1", d.Player)
	}
	before := countDraw(e)
	answerUnlessPay(t, e, true)
	if got := countDraw(e) - before; got != 4 {
		t.Fatalf("pay drew %d cards for the opponent, want 4", got)
	}
	if o := e.G.Obj(kuroki); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Kuroki zone = %v, want kept", o)
	}
}

// TestUnlessDrawCostKurokiDecline is the mirror: the opponent declines, no
// cards move, and the switched orientation skips the reveal/cast body.
func TestUnlessDrawCostKurokiDecline(t *testing.T) {
	t.Parallel()
	e, _ := driveKurokiTrig(t)
	before := countDraw(e)
	answerUnlessPay(t, e, false)
	if got := countDraw(e) - before; got != 0 {
		t.Fatalf("decline drew %d cards, want 0", got)
	}
}

// TestUnlessCostTresserhornPaysSacLifeAndDraw exercises the selected-cost
// continuation on Tresserhorn's Lord, Returned: its controller must
// sacrifice three creatures, pay 3 life, and have its targeted opponent
// draw three cards. All three cost components must happen before Forge's
// switched Sacrifice body resolves.
func TestUnlessCostTresserhornPaysSacLifeAndDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 742)
	lord := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Tresserhorn's Lord, Returned"))
	creatures := []state.ObjID{
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Goblin Piledriver")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
	}
	life := e.G.Players[0].Life

	// Forge spells this carrier as ValidTarget$ rather than ValidTgts$, so
	// target offering is a separate gap. Seed the ordinary stack target event
	// to exercise the real corpus SA with the target binding it requires.
	e.emit(events.Event{Kind: events.TriggerPush, Obj: lord, Player: 0, Amount: 0})
	ability := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, Player: 1, Amount: 1})
	e.resolveTop()
	answerUnlessPay(t, e, true)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "unless_cost" || d.Min != 3 || d.Max != 3 {
		t.Fatalf("pending = %+v, want exact three-creature cost choice", d)
	}
	choices := make([]int, 0, len(creatures))
	for _, want := range creatures {
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("creature %d absent from sacrifice-cost options: %+v", want, d.Options)
		}
		choices = append(choices, found)
	}
	before := countDraw(e)
	submitChoices(t, e, choices...)

	for _, id := range creatures {
		if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
			t.Fatalf("cost creature %d zone = %v, want graveyard", id, got)
		}
	}
	if got := e.G.Players[0].Life; got != life-3 {
		t.Fatalf("payer life = %d, want %d", got, life-3)
	}
	if got := countDraw(e) - before; got != 3 {
		t.Fatalf("targeted opponent drew %d cards, want 3", got)
	}
	// Forge's handleUnlessCost resolves the main body when paid ==
	// UnlessSwitched, so this script's switched Sacrifice body still puts the
	// Lord in the graveyard after the compound cost is paid.
	if got := e.G.Obj(lord).Zone; got != state.ZGraveyard {
		t.Fatalf("Lord zone = %v, want graveyard after the switched paid cost", got)
	}
}

// TestUnlessCostUnresolvableDrawerDeclinesWhole pins the ordering the Draw
// fix exists for: a Draw<N/Spec> whose role the unless context cannot
// resolve makes the whole cost unpayable BEFORE any mana or life is
// charged — never a partial payment followed by a silently omitted draw.
// The mirror half pins that a resolvable role (You, the payer) still pays
// and draws in one pass.
func TestUnlessCostUnresolvableDrawerDeclinesWhole(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 743)
	cost := Cost{Life: 2, Draw: []CostPart{{N: 1, Spec: "Player.NoSuchRole"}}}
	life := e.G.Players[0].Life
	before := len(e.L.Events)
	if e.payUnlessCost(0, cost, &effects.Ctx{Controller: 0}, 0) {
		t.Fatal("payUnlessCost paid a cost whose Draw role cannot resolve")
	}
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("payer life = %d, want the uncharged %d", got, life)
	}
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Draw {
			t.Fatalf("a declined cost drew: %+v", ev)
		}
	}
	if len(e.L.Events) != before {
		t.Fatalf("a declined cost emitted %d events, want none", len(e.L.Events)-before)
	}
	cost.Draw[0].Spec = "You"
	if !e.payUnlessCost(0, cost, &effects.Ctx{Controller: 0}, 0) {
		t.Fatal("payUnlessCost declined a cost whose Draw role is the payer")
	}
	if got := e.G.Players[0].Life; got != life-2 {
		t.Fatalf("payer life = %d, want %d", got, life-2)
	}
	draws := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Draw {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("draws = %d, want 1", draws)
	}
}

// TestParseUnlessCostDrawComponents pins the strict parser: a fixed
// Draw<N/Spec> token is priceable (paid by drawing), a Draw<X/...> unfolded
// amount is not, and every unmodelled verb still declines.
func TestParseUnlessCostDrawComponents(t *testing.T) {
	t.Parallel()
	if c, ok := ParseUnlessCost("Draw<4/Player.targetedBy>"); !ok || len(c.Draw) != 1 ||
		c.Draw[0].N != 4 || c.Draw[0].Spec != "Player.targetedBy" {
		t.Fatalf("Draw<4/Player.targetedBy> = %+v ok=%v, want one 4-card targeted part", c, ok)
	}
	if _, ok := ParseUnlessCost("Draw<X/You>"); ok {
		t.Fatal("Draw<X/You> priced; an unfolded draw amount must decline")
	}
	if c, ok := ParseUnlessCost("PayLife<2> Draw<1/You>"); !ok || c.Life != 2 || len(c.Draw) != 1 {
		t.Fatalf("PayLife<2> Draw<1/You> = %+v ok=%v, want life 2 and one draw part", c, ok)
	}
	if _, ok := ParseUnlessCost("TapXType<1/Creature>"); ok {
		t.Fatal("TapXType priced; an unmodelled verb must decline")
	}
}
