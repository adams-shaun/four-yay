package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
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
