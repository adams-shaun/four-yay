package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBreathstealersCryptReplacedPlayerPay drives the crypt's real
// replacement ("If a player would draw a card, instead they draw a card and
// reveal it. If it's a creature card, that player discards it unless they pay
// 3 life."): the replaced draw's body re-draws, the creature is revealed, and
// the UnlessPayer$ ReplacedPlayer ask reaches the draw-ER. Paying costs 3
// life and keeps the card.
func TestBreathstealersCryptReplacedPlayerPay(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Breathstealer's Crypt"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)

	draws := countDraw(e)
	kr5EmitDraw(t, e, 1)
	// The replacement discarded the original Draw event; the body drew one.
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("draw events after replacement = %d, want 1 (the body's re-draw)", got)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the ReplacedPlayer unless-pay ask for seat 1", d)
	}
	life := e.G.Players[1].Life
	answerUnlessPay(t, e, true)
	if got := e.G.Players[1].Life; got != life-3 {
		t.Fatalf("draw-er life = %d, want %d (paid 3)", got, life-3)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZHand {
		t.Fatalf("drawn creature zone = %v, want kept in hand (paid)", o)
	}
}

// TestBreathstealersCryptReplacedPlayerDecline is the mirror: declining
// discards the revealed creature, and no life moves.
func TestBreathstealersCryptReplacedPlayerDecline(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Breathstealer's Crypt"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)

	kr5EmitDraw(t, e, 1)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the ReplacedPlayer unless-pay ask for seat 1", d)
	}
	life := e.G.Players[1].Life
	answerUnlessPay(t, e, false)
	if got := e.G.Players[1].Life; got != life {
		t.Fatalf("draw-er life = %d, want %d (declined)", got, life)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("drawn creature zone = %v, want discarded", o)
	}
}

// TestZursWeirdingNonReplacedPlayerPays drives Zur's Weirding ("If a player
// would draw a card, they reveal it instead. Then any other player may pay 2
// life. If a player does, put that card into its owner's graveyard.
// Otherwise, that player draws a card."): the UnlessPayer$ NonReplacedPlayer
// ask reaches the OTHER player; paying mills the revealed card (UnlessSwitched$
// True: paying CAUSES the mill) and the WhenNotPaid DBDraw sub is skipped;
// declining skips the mill and the sub draws the card.
func TestZursWeirdingNonReplacedPlayerPays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Zur's Weirding"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)
	addMana(t, e, 0, "CC")

	draws := countDraw(e)
	kr5EmitDraw(t, e, 1)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 0 {
		t.Fatalf("pending = %+v, want the NonReplacedPlayer unless-pay ask for seat 0", d)
	}
	life := e.G.Players[0].Life
	answerUnlessPay(t, e, true)
	if got := e.G.Players[0].Life; got != life-2 {
		t.Fatalf("payer life = %d, want %d", got, life-2)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("revealed card zone = %v, want milled to the graveyard (paid)", o)
	}
	if got := countDraw(e) - draws; got != 0 {
		t.Fatalf("draw delta = %d, want 0 (the WhenNotPaid draw sub was skipped)", got)
	}
}

// TestZursWeirdingNonReplacedPlayerDeclines is the mirror: declining skips
// the mill and the WhenNotPaid sub draws the card for the draw-er.
func TestZursWeirdingNonReplacedPlayerDeclines(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Zur's Weirding"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)

	draws := countDraw(e)
	kr5EmitDraw(t, e, 1)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 0 {
		t.Fatalf("pending = %+v, want the NonReplacedPlayer unless-pay ask for seat 0", d)
	}
	answerUnlessPay(t, e, false)
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZHand {
		t.Fatalf("revealed card zone = %v, want drawn (declined)", o)
	}
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("draw delta = %d, want 1 (the WhenNotPaid sub drew)", got)
	}
}

// kr5EmitDraw is emitDraw run as a kernel probe from a quiet engine: a
// draw replacement's body asks (an unless-pay election) are posed and the
// answering Submit re-executes the draw with the answer served.
func kr5EmitDraw(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	e.pending = nil
	e.probe(func() { emitDraw(t, e, p) })
}
