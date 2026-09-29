package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPsychicTransferPreventionDoesNotHalfExchange is the atomicity pin for
// the real card path (CR 701.20b): a corpus Psychic Transfer cast through the
// full cast flow must reach the engine's lifeExchangeTransaction seam, not the
// old two-bare-h.Emit fallback in effects/life.go. With a CantGainLife static
// (Leyline of Punishment) consuming the gaining side, the losing side must
// still apply and the exchange must settle whole -- no half exchange.
func TestPsychicTransferPreventionDoesNotHalfExchange(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	psychic := mustCorpusCard(t, reg, "Psychic Transfer")
	leyline := mustCorpusCard(t, reg, "Leyline of Punishment")

	e := newSeats(t, 2)
	// Seat 0 at 15, seat 1 at 20: Psychic Transfer's ConditionSVarCompare$ LE5
	// gate (difference <= 5) is satisfied, and seat 0 must gain 5 while seat 1
	// loses 5.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5})
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: lives = %d/%d, want 15/20 (difference 5)",
			e.G.Players[0].Life, e.G.Players[1].Life)
	}

	// Leyline of Punishment's CantGainLife static sits on seat 0's battlefield
	// so seat 0's (the controller's) gaining side is consumed.
	leylineID := onBoardCard(t, e, 0, leyline)
	if o := e.G.Obj(leylineID); o == nil || o.Zone != state.ZBattlefield || o.Face() == nil ||
		len(o.Face().Statics) == 0 || o.Face().Statics[0].Mode != "CantGainLife" {
		t.Fatalf("precondition: Leyline of Punishment's CantGainLife static not on seat 0's battlefield: %+v", o)
	}
	if !e.lifeGainForbidden(0) {
		t.Fatal("precondition: seat 0 life gain is not forbidden by the active CantGainLife static")
	}

	id := putInHand(t, e, 0, psychic)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Psychic Transfer not in seat 0's hand: %+v", o)
	}
	addMana(t, e, 0, "UUUUU")

	submitChoices(t, e, castCardOption(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after cast = %+v, want Psychic Transfer's target ask", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("opponent (seat 1) not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Drive to an idle priority so the stack resolves.
	for i := 0; i < 40 && (len(e.G.Stack) > 0 || e.Pending() == nil || e.Pending().Kind != decision.KPriority); i++ {
		p := e.Pending()
		if p == nil {
			e.Advance()
			continue
		}
		if p.Kind != decision.KPriority {
			t.Fatalf("unexpected ask during Psychic Transfer: %s %+v", p.Kind, p)
		}
		pass := -1
		for _, o := range p.Options {
			if o.Kind == "pass" {
				pass = o.Index
				break
			}
		}
		if pass < 0 {
			t.Fatal("no pass option")
		}
		submitChoices(t, e, pass)
	}

	// The effect must have reached the engine transaction. The losing side
	// applied (seat 1 20 -> 15) and the gaining side was consumed (seat 0 stays
	// at 15). The lives alone do not discriminate here -- Leyline forbids every
	// player's gain, so the old two-bare-h.Emit fallback reaches the same two
	// totals -- the Note below is the transaction's own settle marker and is
	// absent on the bare fallback (measured).
	if got := e.G.Players[1].Life; got != 15 {
		t.Fatalf("losing side = %d, want 15 (seat 1 lost 5)", got)
	}
	if got := e.G.Players[0].Life; got != 15 {
		t.Fatalf("gaining side = %d, want 15 (seat 0's gain consumed, no half exchange)", got)
	}
	if !hasNoteText(e, "ExchangeLife settled with a replaced life-change side") {
		t.Fatal("missing Note for the exchange's consumed life-change side")
	}
	// The effect must not have fallen back to the unimplemented API Note.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API ExchangeLife" {
			t.Fatal("ExchangeLife reached the unimplemented API fallback")
		}
	}
}
