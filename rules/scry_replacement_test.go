package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The R:Event$ Scry replacement class (task scryrepl). The corpus carries
// exactly two carriers at the pin:
//
//   - Kenessos, Priest of Thassa: "If you would scry a number of cards, scry
//     that many cards plus one instead."  DB$ ReplaceEffect | VarName$ Num |
//     VarValue$ X, SVar:X ReplaceCount$Num/Plus.1
//   - Eligeth, Crossroads Augur: "If you would scry a number of cards, draw
//     that many cards instead."  DB$ Draw | Defined$ You | NumCards$ X,
//     SVar:X ReplaceCount$Num
//
// (/usr/bin/grep -rlE 'R:Event\$ Scry' .cards/cardsfolder -> 2 files.)
//
// The replacement is applied at the INSTRUCTION boundary, before any card is
// looked at (CR 614.4), which is why Host.Scry proposes the count and the
// completed events.Scry record (the bottom-card marker trig:Scry matches) is
// emitted afterwards, outside the replacement pass.

// scryUnimplementedNotes returns the loud "unimplemented Scry replacement"
// Notes in the log. A test that expects a replacement to have worked asserts
// this is empty, so a body falling to the fail-loud arm cannot pass as
// "nothing happens".
func scryUnimplementedNotes(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented Scry replacement") {
			out = append(out, ev)
		}
	}
	return out
}

// TestKenessosReplacesScryCountBeforeLooking pins the count-rewrite shape.
// The observable is the ARRANGE WINDOW: the base Scry 3 offers 3 cards, and
// Kenessos's "plus one" must widen it to 4 BEFORE the options are built (a
// replacement applied after the look could not add a card to a window already
// offered).
func TestKenessosReplacesScryCountBeforeLooking(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 8212)
	kenessos := onBoardCard(t, e, 0, corpusCard(t, "Kenessos, Priest of Thassa"))
	if o := e.G.Obj(kenessos); o == nil || o.Zone != state.ZBattlefield ||
		len(o.Face().Repls) == 0 || o.Face().Repls[0].Event != "Scry" {
		t.Fatalf("precondition: Kenessos's R:Event$ Scry replacement is not live on the battlefield: %+v", o)
	}
	if n := len(e.G.Zone(state.ZLibrary, 0)); n < 5 {
		t.Fatalf("precondition: seat 0's library has %d cards, want >= 5 so the 4-card window and a remainder both exist", n)
	}
	d := scryDecision(t, e, id)
	// The base window is 3. Assert the DIFFERENCE so the test fails with the
	// replacement unregistered (it would offer 3, not 4).
	if d.Max != 4 || len(d.Options) != 4 {
		t.Fatalf("Kenessos Scry 3 arranged %d options (max %d), want 4", len(d.Options), d.Max)
	}
	if notes := scryUnimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("Kenessos's replacement fell to the unimplemented Note: %+v", notes)
	}
}

// TestKenessosDoesNotReplaceTheCompletedScryRecord pins the CR 614.4 window's
// EXIT: the completed events.Scry record carries the number of cards actually
// put on the bottom, and the same Kenessos replacement must NOT be applied to
// it a second time. Looked at 4 (Scry 3 + Kenessos), kept 1 on top, bottomed
// 3: the record must read 3. Re-applying Kenessos would read 4.
func TestKenessosDoesNotReplaceTheCompletedScryRecord(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 8215)
	kenessos := onBoardCard(t, e, 0, corpusCard(t, "Kenessos, Priest of Thassa"))
	if o := e.G.Obj(kenessos); o == nil || o.Zone != state.ZBattlefield ||
		len(o.Face().Repls) == 0 || o.Face().Repls[0].Event != "Scry" {
		t.Fatalf("precondition: Kenessos's R:Event$ Scry replacement is not live on the battlefield: %+v", o)
	}
	d := scryDecision(t, e, id)
	if d.Max != 4 {
		t.Fatalf("precondition: Kenessos did not widen the window: Max = %d, want 4 (a base Scry 3 look)", d.Max)
	}
	// Keep only the first offered card; the other three go to the bottom.
	submitChoices(t, e, d.Options[0].Index)

	marks := scryMarkers(e)
	if len(marks) != 1 {
		t.Fatalf("log carries %d Scry records after one scry, want exactly 1 (0 = the record was never emitted)", len(marks))
	}
	if marks[0].Amount != 3 {
		t.Fatalf("completed Scry record Amount = %d, want 3 (the cards actually bottomed; 4 = Kenessos was re-applied to the record, 2 = the base window was used)", marks[0].Amount)
	}
	if marks[0].Player != 0 || marks[0].Obj != id {
		t.Fatalf("completed Scry record = %+v, want player 0 and source %d", marks[0], id)
	}
}

// TestEligethDrawsInsteadOfScrying pins the whole-instruction replacement: no
// library is looked at, no KArrange is posed, no events.Scry record is logged
// (so no Mode$ Scry trigger can fire), and the three scried cards become three
// drawn cards.
func TestEligethDrawsInsteadOfScrying(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 8213)
	eligeth := onBoardCard(t, e, 0, corpusCard(t, "Eligeth, Crossroads Augur"))
	if o := e.G.Obj(eligeth); o == nil || o.Zone != state.ZBattlefield ||
		len(o.Face().Repls) == 0 || o.Face().Repls[0].Event != "Scry" {
		t.Fatalf("precondition: Eligeth's R:Event$ Scry replacement is not live on the battlefield: %+v", o)
	}
	if n := len(e.G.Zone(state.ZLibrary, 0)); n < 3 {
		t.Fatalf("precondition: seat 0's library has %d cards, want >= 3 so the three draws do not deck the player", n)
	}
	before := len(e.G.Zone(state.ZHand, 0))
	castFromPriority(t, e, id)
	passUntilStackEmpty(t, e, 20)

	if d := e.Pending(); d != nil && d.Kind == decision.KArrange {
		t.Fatalf("replaced scry asked for an arrangement: %+v", d)
	}
	// Cast one from hand (-1), draw three instead (+3) = +2. A scry would be
	// net -1 (the SpellDescription says nothing is drawn).
	if got := len(e.G.Zone(state.ZHand, 0)) - before; got != 2 {
		t.Fatalf("net hand change = %d, want +2 (cast one, draw three instead of scrying)", got)
	}
	if marks := scryMarkers(e); len(marks) != 0 {
		t.Fatalf("replaced scry logged %d Scry records and may fire Scry triggers: %+v", len(marks), marks)
	}
	if notes := scryUnimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("Eligeth's replacement fell to the unimplemented Note: %+v", notes)
	}
}
