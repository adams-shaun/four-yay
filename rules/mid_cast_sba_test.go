// CR 704.3: state-based actions are checked only when a player would receive
// priority, and nobody receives priority while a spell is being cast
// (CR 601.2). The CR 601.2g mana window re-enters payCast for each mana
// ability activated, so a source whose activation drops the payer to 0 or less
// (Ancient Tomb's own 2 damage) must not end the game between payment steps.
//
// This is the reverse ordering of TestManaPaymentContinuesBelowZeroLife in
// mana_negative_life_test.go: there the Island is tapped first and the tomb
// last, so the loss fires on the same Submit that settles the payment and the
// pre-fix bug stayed hidden. Here the tomb is tapped FIRST, crossing a Submit
// boundary at life -1 while the cast is still mid-window -- the gap this
// ticket closes. A loss recorded before the Island is tapped is the failure
// the test asserts against; the loss at the priority boundary after the cast
// completes is the contract.
//
// Every card is a REAL corpus card (Keep Watch, Ancient Tomb) or the authored
// Island fixture; no Forge .txt text is committed here.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMidCastSBADoesNotEndGameDuringManaWindow is the ticket's repro: at 1
// life the caster pays a {2}{U} instant with Ancient Tomb tapped FIRST ({C}{C}
// plus its own 2 damage, life 1 -> -1) and the Island LAST, both inside the
// CR 601.2g payment window. The tomb's damage lands on the first Submit; the
// caster must still be alive there (no state-based action mid-cast, CR 704.3),
// the window must re-pose with the Island still offered, and only after the
// Island settles the payment does the state-based action at the priority
// boundary record the loss.
func TestMidCastSBADoesNotEndGameDuringManaWindow(t *testing.T) {
	t.Parallel()
	e, tombID, islandID, watchID := nlCastEngine(t, 423, 1, "")
	// Preconditions: the caster really is at 1 life with the spell in hand,
	// and both mana sources really are on the battlefield where the window
	// offers them. A vacuous board would make the whole test meaningless.
	if got := e.G.Players[0].Life; got != 1 {
		t.Fatalf("precondition: caster life = %d, want 1", got)
	}
	if o := e.G.Obj(tombID); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Ancient Tomb not an untapped battlefield permanent: %+v", o)
	}
	if o := e.G.Obj(islandID); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Island not an untapped battlefield permanent: %+v", o)
	}
	if o := e.G.Obj(watchID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Keep Watch not in hand: %+v", o)
	}
	if c := e.G.Obj(watchID).Face().ManaCost; c != "2 U" {
		t.Fatalf("precondition: Keep Watch mana cost = %q, want %q", c, "2 U")
	}
	// The pool-only offer gate never offers a cast the empty pool cannot pay,
	// so the cast is begun directly, exactly as mana_negative_life_test does.
	if castOffered(e, watchID) {
		t.Fatal("precondition: the empty pool cannot pay {2}{U}, yet the pool-only gate offered the cast")
	}
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: watchID})
	e.Advance()
	if w := e.Pending(); w == nil || w.Kind != decision.KChoose {
		t.Fatalf("after beginning the cast = %+v, want the CR 601.2g mana window", w)
	}
	// The spell was announced before payment (CR 601.2a), so it is already on
	// the stack while the window is open.
	if !nlPutOnStack(e, watchID) {
		t.Fatal("precondition: Keep Watch is not on the stack after the announcement")
	}

	// Tap Ancient Tomb FIRST: its {C}{C} and its 2 damage resolve, dropping
	// the caster to -1 while the cast is still mid-window. The pre-fix bug
	// ran the state-based action here and recorded a loss before the Island
	// could be tapped.
	afterTomb := len(e.L.Events)
	if !cwActivateInWindow(t, e, tombID) {
		t.Fatalf("the window did not offer Ancient Tomb: %+v", e.Pending())
	}
	// The precondition the fix turns on: the tomb really did damage the
	// caster to -1 on this Submit.
	if got := e.G.Players[0].Life; got != -1 {
		t.Fatalf("after the tomb: life = %d, want -1", got)
	}
	if e.G.Players[0].Lost {
		t.Fatal("the caster is lost mid-cast; CR 704.3 forbids a state-based action inside the payment window")
	}
	for _, ev := range e.L.Events[afterTomb:] {
		if ev.Kind == events.PlayerLost {
			t.Fatalf("a PlayerLost was recorded inside the payment window: %+v", ev)
		}
	}
	// The spell survives the mid-window boundary: the loss did not reverse
	// or resolve it.
	if !nlPutOnStack(e, watchID) {
		t.Fatal("Keep Watch left the stack mid-window; the cast was disturbed by the damage")
	}
	// The window re-posed with the Island still offered: the cast is live.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after the tomb = %+v, want the window re-posed", d)
	}
	wantIsland := false
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == islandID {
			wantIsland = true
		}
	}
	if !wantIsland {
		t.Fatalf("the re-posed window does not offer the Island: %+v", d.Options)
	}

	// Tap the Island LAST: the payment settles inside this answer. The
	// state-based action runs at the priority boundary the completed cast
	// reaches, and the loss names the life total.
	if !cwActivateInWindow(t, e, islandID) {
		t.Fatalf("the window did not offer the Island: %+v", e.Pending())
	}
	if !nlPutOnStack(e, watchID) {
		t.Fatal("Keep Watch never reached the stack; the payment did not complete")
	}
	// The loss is the FIRST PlayerLost after the tomb boundary, and it is the
	// state-based life loss -- never a mid-window loss.
	lost := -1
	for i := afterTomb; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.PlayerLost && e.L.Events[i].Player == 0 {
			lost = i
			break
		}
	}
	if lost < 0 {
		t.Fatalf("no PlayerLost after the payment completed; events=%v", e.L.Events[afterTomb:])
	}
	if got := e.L.Events[lost].Text; got != "life total is 0 or less" {
		t.Fatalf("player loss = %q, want %q", got, "life total is 0 or less")
	}
	if got := e.G.Players[0].Life; got != -1 {
		t.Fatalf("caster life after the payment = %d, want -1", got)
	}
	if !e.G.Players[0].Lost {
		t.Fatal("the caster was not lost at the priority boundary after the payment")
	}
}
