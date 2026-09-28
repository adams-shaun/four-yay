package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The dynamic Draw-cost ticket (cost:Draw). ParseCost used to model only the
// literal Draw<N/Spec> form; Forge's non-literal amount Draw<X/Spec> fell
// through the nonManaCost regex to the unrecognised-symbol fallback, which
// substituted ONE GENERIC MANA for the whole cost and reported the head (the
// parameter census's `cost:Draw` label). Champion of Wits is the class's
// canonical carrier:
//
//	T:Mode$ ChangesZone | ... | Execute$ TrigDraw
//	SVar:TrigDraw:AB$ Discard | Defined$ You | Mode$ TgtChoose |
//	    NumCards$ 2 | Cost$ Draw<X/You>
//	SVar:X:Count$CardPower
//
// "When CARDNAME enters, you may draw cards equal to its power. If you do,
// discard two cards." The cost's X is not a cast announcement -- it is the
// card's SVar:X (Count$CardPower), resolved at payment from the source face
// exactly the way fixLifeXCost resolves an SVar-valued PayLife<X>. The
// trigger-executed AB joins the ordinary triggered-cost window (Untap /
// ImmediateTrigger / CopySpellAbility): the pay answer settles the draw, the
// decline leaves the discard body unexecuted, and a Draw-bearing trigger
// effect no longer runs for free.
//
// The helpers come from search_library_test.go (searchTestRegistry,
// searchEngine, searchMoveByName), resolution_test.go (passUntilNonPriority),
// cast_test.go (submitChoices) and replacement_updated_test.go
// (passUntilStackEmpty) -- all the same package. Champion of Wits is in no
// repo deck, so no chain head or ratchet row depends on it.

// witsPayAsk advances to the trigger-cost pay/decline ask the entering
// Champion of Wits poses and returns it.
func witsPayAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the trigger-cost pay ask, got %+v", d)
	}
	return d
}

// witsPayOption returns the index of the window's pay option, or -1.
func witsPayOption(d *decision.Decision) int {
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_pay" {
			return o.Index
		}
	}
	return -1
}

// TestChampionOfWitsDrawsItsPowerThenDiscardsTwo pins the whole chain on the
// real corpus card: an entering 2/1 offers the Draw<X/You> cost, paying it
// draws exactly two cards (X = power 2), and the chained discard takes
// exactly two.
func TestChampionOfWitsDrawsItsPowerThenDiscardsTwo(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Champion of Wits")
	searchMoveByName(t, e, "Champion of Wits", state.ZBattlefield)

	pay := witsPayAsk(t, e)
	payIdx := witsPayOption(pay)
	if payIdx < 0 {
		t.Fatalf("the Draw<X/You> cost was not offered as payable: %+v", pay.Options)
	}

	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	mark := len(e.L.Events)
	submitChoices(t, e, payIdx)

	discard := e.Pending()
	if discard == nil || discard.ResumeKind != "discard" {
		t.Fatalf("expected the discard ask after paying the draw cost, got %+v", discard)
	}
	if discard.Min != 2 || discard.Max != 2 {
		t.Fatalf("discard ask is %d..%d, want 2..2", discard.Min, discard.Max)
	}
	picks := make([]int, 0, 2)
	for _, o := range discard.Options {
		if len(picks) < 2 {
			picks = append(picks, o.Index)
		}
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 20)

	draws, discards := 0, 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
		if events.IsDiscard(ev) {
			discards++
		}
	}
	if draws != 2 {
		t.Fatalf("draw events = %d, want 2 (X = Champion of Wits' power 2)", draws)
	}
	if discards != 2 {
		t.Fatalf("discard events = %d, want 2", discards)
	}
	if got := libBefore - len(e.G.Zone(state.ZLibrary, 0)); got != 2 {
		t.Fatalf("library shrank by %d cards, want 2 (the cost's draw)", got)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 2 {
		t.Fatalf("graveyard holds %d cards, want 2 (the chained discard)", got)
	}
}

// TestChampionOfWitsDeclinedDrawCostSkipsTheDiscard pins the decline arm: the
// "you may draw ... If you do, discard" cost is a real pay/decline, and a
// decline draws nothing and discards nothing.
func TestChampionOfWitsDeclinedDrawCostSkipsTheDiscard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Champion of Wits")
	searchMoveByName(t, e, "Champion of Wits", state.ZBattlefield)

	pay := witsPayAsk(t, e)
	declineIdx := -1
	for _, o := range pay.Options {
		if o.Kind == "trigger_cost_decline" {
			declineIdx = o.Index
		}
	}
	if declineIdx < 0 {
		t.Fatalf("no decline option in the pay ask: %+v", pay.Options)
	}

	mark := len(e.L.Events)
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	gyBefore := len(e.G.Zone(state.ZGraveyard, 0))
	submitChoices(t, e, declineIdx)
	passUntilStackEmpty(t, e, 20)

	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			t.Fatalf("a declined Draw cost drew a card")
		}
		if events.IsDiscard(ev) {
			t.Fatalf("a declined Draw cost still discarded")
		}
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore {
		t.Fatalf("library = %d after a declined cost, want unchanged %d", got, libBefore)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != gyBefore {
		t.Fatalf("graveyard = %d after a declined cost, want unchanged %d", got, gyBefore)
	}
}

// TestParseCostModelsDynamicDraw is the ratchet-side pin: the dynamic form is
// a MODELLED cost part (no Unknown entry, no substituted generic mana), and
// the same for the unless-cost parser. This is what removes the parameter
// census's `cost:Draw` label for every Draw<X/...> carrier.
func TestParseCostModelsDynamicDraw(t *testing.T) {
	t.Parallel()
	c := ParseCost("Draw<X/You>")
	if len(c.Unknown) != 0 {
		t.Fatalf("Draw<X/You> still reports unmodelled tokens: %v", c.Unknown)
	}
	if c.Generic != 0 {
		t.Fatalf("Draw<X/You> substituted %d generic mana", c.Generic)
	}
	if len(c.Draw) != 1 || c.Draw[0].Dyn != "X" || c.Draw[0].Spec != "You" {
		t.Fatalf("Draw<X/You> parsed as %+v, want one Dyn X part for You", c.Draw)
	}

	// The literal form keeps its existing shape.
	lit := ParseCost("Draw<2/You>")
	if len(lit.Draw) != 1 || lit.Draw[0].N != 2 || lit.Draw[0].Dyn != "" {
		t.Fatalf("Draw<2/You> parsed as %+v, want one literal N=2 part", lit.Draw)
	}

	// The unless-cost parser deliberately stays strict about an UNFOLDED draw
	// amount: ParseUnlessCost has no source SVar table to resolve Draw<X/...>
	// against at the parse site (the ParseUnlessCost contract -- pinned by
	// TestParseUnlessCostDrawComponents), so it still declines rather than
	// pricing an unresolved amount. Only the cast/activation ParseCost path
	// models the dynamic part; the trigger-cost window resolves it at payment
	// from the source's SVar table.
	if _, ok := ParseUnlessCost("Draw<X/You>"); ok {
		t.Fatal("ParseUnlessCost priced Draw<X/You>; an unfolded draw amount must decline")
	}
}
