package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The RearrangeTopOfLibrary primitive's MayShuffle$ read (task
// inbox-paramcensus-final-stragglers, Ponder entry): after the KArrange is
// applied, the effect poses the may-shuffle ask; a "yes" answer
// (arrange_mayshuffle, applied in place) emits the Shuffle event, and the
// chained SubAbility$ (Ponder's draw) runs after the shuffle.

// ponderSrc is Ponder's real script shape (the SP$ line verbatim).
const ponderSrc = "Name:Ponder\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3 | MayShuffle$ True | SubAbility$ DBDraw | SpellDescription$ Look at the top three cards of your library, then put them back in any order. You may shuffle. Draw a card.\n" +
	"SVar:DBDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"

// TestPonderMayShuffleAsksAndShuffles drives the full three-ask sequence:
// cast, arrange, shuffle yes, draw. The Shuffle event must sit between the
// arrange's LibraryOrder and the resolution's completion, and the chained
// draw must still run after the shuffle.
func TestPonderMayShuffleAsksAndShuffles(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 96, ponderSrc)
	addMana(t, e, 0, "U")
	before := len(e.G.Zone(state.ZHand, 0))
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	logBefore := len(e.L.Events)
	castFixture(t, e, id, -1)

	// The arrange ask: Min == Max == 3 over the top three cards.
	d := e.Pending()
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected the KArrange ask, got %+v", d)
	}
	submitChoices(t, e, 2, 0, 1) // pick all three, reversed order

	// The may-shuffle ask after the arrange.
	sd := e.Pending()
	if sd == nil || sd.Kind != decision.KChoose || sd.ResumeKind != "arrange_mayshuffle" {
		t.Fatalf("expected the may-shuffle ask, got %+v", sd)
	}
	yesIdx := -1
	for _, o := range sd.Options {
		if o.Kind == "yes" {
			yesIdx = o.Index
		}
	}
	if yesIdx < 0 {
		t.Fatalf("no yes option: %+v", sd.Options)
	}
	submitChoices(t, e, yesIdx)
	passUntilStackEmpty(t, e, 20)

	// Exactly one Shuffle event past the pre-cast log (genesis's own shuffles
	// are behind logBefore), and it sits before the resolution's draw.
	shuffles := 0
	shuffleAt := -1
	for i, ev := range e.L.Events {
		if i < logBefore {
			continue
		}
		if ev.Kind == events.Shuffle {
			shuffles++
			shuffleAt = i
		}
	}
	if shuffles != 1 {
		t.Fatalf("recorded %d Shuffle events, want exactly 1", shuffles)
	}
	draws := 0
	lastDraw := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
			lastDraw = i
		}
	}
	if draws == 0 || lastDraw < shuffleAt {
		t.Fatalf("draw after shuffle missing (draws=%d shuffleAt=%d lastDraw=%d)", draws, shuffleAt, lastDraw)
	}
	// Ponder itself left the hand for the stack/graveyard and the chained
	// draw replaced it: net zero on the hand, one card off the library.
	if len(e.G.Zone(state.ZHand, 0)) != before {
		t.Fatalf("hand = %d, want %d (the chained draw replaced the cast spell)", len(e.G.Zone(state.ZHand, 0)), before)
	}
	if len(e.G.Zone(state.ZLibrary, 0)) != libBefore-1 {
		t.Fatalf("library = %d, want %d", len(e.G.Zone(state.ZLibrary, 0)), libBefore-1)
	}
}

// TestPonderMayShuffleDeclineKeepsOrder answers the may-shuffle ask "no":
// no Shuffle event is recorded, and the chained draw still runs.
func TestPonderMayShuffleDeclineKeepsOrder(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 96, ponderSrc)
	addMana(t, e, 0, "U")
	logBefore := len(e.L.Events)
	castFixture(t, e, id, -1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected the KArrange ask, got %+v", d)
	}
	submitChoices(t, e, 0, 1, 2)
	sd := e.Pending()
	if sd == nil || sd.ResumeKind != "arrange_mayshuffle" {
		t.Fatalf("expected the may-shuffle ask, got %+v", sd)
	}
	noIdx := -1
	for _, o := range sd.Options {
		if o.Kind == "no" {
			noIdx = o.Index
		}
	}
	submitChoices(t, e, noIdx)
	passUntilStackEmpty(t, e, 20)
	for i, ev := range e.L.Events {
		if i < logBefore {
			continue
		}
		if ev.Kind == events.Shuffle {
			t.Fatal("a declined may-shuffle still emitted a Shuffle event")
		}
	}
	if !hasEventKind(e, events.Draw) {
		t.Fatal("the chained draw did not run after the decline")
	}
}
