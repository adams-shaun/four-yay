package rules

// Averna, the Chaos Bloom (R:Event$ Cascade). The cascade instruction's
// replacement boundary: after the whole exile-until batch is known and before
// any card is bottomed or offered for casting, Averna's body puts ONE land
// from that batch onto the battlefield tapped. These tests drive the real
// corpus card end to end through a real cast, with a nonmember land already
// in exile so a whole-zone offer (the failure mode the boundary exists to
// prevent) cannot pass.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// avernaOptions returns the offered object ids of the pending decision, in
// option order.
func avernaOptions(d *decision.Decision) []state.ObjID {
	out := make([]state.ObjID, 0, len(d.Options))
	for _, o := range d.Options {
		out = append(out, o.Obj)
	}
	return out
}

// TestAvernaCascadeOffersOnlyExiledLandBeforeCastElection is the real-card
// end-to-end pin: casting Bloodbraid Elf (mana value 4) over a window of
// Forest + Grizzly Bears exiles both cards, Averna's replacement offers
// exactly the Forest from THAT batch (never the unrelated land already in
// exile), the land enters tapped, and the ordinary free-cast election for
// Grizzly Bears follows. The unrelated land must remain in exile throughout.
func TestAvernaCascadeOffersOnlyExiledLandBeforeCastElection(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9301, "Bloodbraid Elf",
		[]string{"Forest", "Grizzly Bears"},
		[]string{"Averna, the Chaos Bloom"})
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, bearID := lib[0], lib[1]
	// Precondition: the arranged window really is Forest then Grizzly Bears.
	if o := e.G.Obj(forestID); o == nil || o.Face() == nil || o.Face().Name != "Forest" {
		t.Fatalf("window[0] = %v, want Forest", e.G.Obj(forestID))
	}
	if o := e.G.Obj(bearID); o == nil || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
		t.Fatalf("window[1] = %v, want Grizzly Bears", e.G.Obj(bearID))
	}
	// A nonmember land already in exile: the pool walker must never offer it.
	otherLand := searchMoveByName(t, e, "Mountain", state.ZExile)
	if o := e.G.Obj(otherLand); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition failed: unrelated land in %v, want exile", e.G.Obj(otherLand))
	}
	// Precondition: Averna is really on the battlefield (the zone the R:
	// line's ActiveZones$ Battlefield reads).
	aver := avernaOnBattlefield(t, e)
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	addMana(t, e, 0, "GGRR")
	castFixture(t, e, elfID, -1)

	// Averna's land pick is posed BEFORE the cast election, offering only
	// the exiled batch's land.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" {
		t.Fatalf("want Averna's hidden land pick, got %+v", d)
	}
	if got := avernaOptions(d); len(got) != 1 || got[0] != forestID {
		t.Fatalf("Averna offered %v, want exactly the exiled Forest %d (not the unrelated land %d or the found card %d)",
			got, forestID, otherLand, bearID)
	}
	if d.Source != aver {
		t.Fatalf("pick source = %d, want Averna %d", d.Source, aver)
	}
	// Precondition: the found nonland card really is in exile at ask time, so
	// the batch the replacement saw is non-empty and ordered.
	if got := e.G.Obj(bearID).Zone; got != state.ZExile {
		t.Fatalf("found card in %v at Averna ask, want exile", got)
	}
	if !hasNote(e, "cascades, exiling") {
		t.Fatal("the exile reveal Note is missing")
	}

	submitChoices(t, e, d.Options[0].Index)
	// The chosen batch land enters the battlefield TAPPED.
	if got := e.G.Obj(forestID).Zone; got != state.ZBattlefield {
		t.Fatalf("chosen land in %v, want the battlefield", got)
	}
	if !e.G.Obj(forestID).Tapped {
		t.Fatal("Averna's land must enter tapped")
	}
	// The nonmember land was never offered and never moved.
	if got := e.G.Obj(otherLand).Zone; got != state.ZExile {
		t.Fatalf("unrelated land moved to %v, want exile", got)
	}

	// Then the ordinary cast election for the found card.
	idx := cascadeElection(t, e, "Grizzly Bears")
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bearID).Zone; got != state.ZBattlefield {
		t.Fatalf("free-cast Grizzly Bears in %v, want the battlefield", got)
	}
	if got := e.G.Obj(elfID).Zone; got != state.ZBattlefield {
		t.Fatalf("resolved Bloodbraid Elf in %v, want the battlefield", got)
	}
	// No double exiles: each batch card left the library exactly once.
	if n := movedTo(t, e, forestID, state.ZLibrary, state.ZExile); n != 1 {
		t.Fatalf("the batch land was exiled %d times", n)
	}
	if n := movedTo(t, e, bearID, state.ZLibrary, state.ZExile); n != 1 {
		t.Fatalf("the found card was exiled %d times", n)
	}
	// The land was put onto the battlefield by Averna, never bottomed.
	if n := movedTo(t, e, forestID, state.ZExile, state.ZLibrary); n != 0 {
		t.Fatalf("Averna's land was returned to the library %d times", n)
	}
	replayCheck(t, e, cfg)
}

// TestAvernaCascadeNoLandOffersNothing pins the no-land arm: with a batch of
// nonland cards, Averna's replacement must offer nothing (the pool is empty,
// Min 0), the found card is exiled exactly once, and the ordinary cast
// election follows directly.
func TestAvernaCascadeNoLandOffersNothing(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9302, "Bloodbraid Elf",
		[]string{"Grizzly Bears"},
		[]string{"Averna, the Chaos Bloom"})
	lib := e.G.Zone(state.ZLibrary, 0)
	bearID := lib[0]
	aver := avernaOnBattlefield(t, e)
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	addMana(t, e, 0, "GGRR")
	castFixture(t, e, elfID, -1)

	// No land in the batch: effHiddenPick completes silently (empty pool)
	// rather than asking, so the free-cast election is already pending.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "hidden_pick" {
		t.Fatalf("Averna asked over an empty land pool: %+v", d)
	}
	idx := cascadeElection(t, e, "Grizzly Bears")
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(bearID).Zone; got != state.ZBattlefield {
		t.Fatalf("free-cast Grizzly Bears in %v, want the battlefield", got)
	}
	if n := movedTo(t, e, bearID, state.ZLibrary, state.ZExile); n != 1 {
		t.Fatalf("found card exiled %d times", n)
	}
	replayCheck(t, e, cfg)
	_ = aver
}

// TestAvernaCascadeDeclinedLandStillProceeds pins the optionality: declining
// the land pick leaves it exiled (then bottomed by the residue), and the
// cascade still reaches its free-cast election.
func TestAvernaCascadeDeclinedLandStillProceeds(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9303, "Bloodbraid Elf",
		[]string{"Forest", "Grizzly Bears"},
		[]string{"Averna, the Chaos Bloom"})
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, bearID := lib[0], lib[1]
	avernaOnBattlefield(t, e)
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	addMana(t, e, 0, "GGRR")
	castFixture(t, e, elfID, -1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" {
		t.Fatalf("want Averna's hidden land pick, got %+v", d)
	}
	// Precondition: the offered land really is the Forest.
	if got := avernaOptions(d); len(got) != 1 || got[0] != forestID {
		t.Fatalf("Averna offered %v, want the Forest %d", got, forestID)
	}
	if got := e.G.Obj(forestID).Zone; got != state.ZExile {
		t.Fatalf("land in %v before the ask, want exile", got)
	}
	submitChoices(t, e) // decline: the empty answer Min 0 makes legal

	// The residue runs as soon as the pick is answered: the declined land is
	// bottomed (the batch's non-found card), and the free-cast election is
	// now pending.
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("declined land in %v, want the library (bottomed by the residue)", got)
	}
	idx := cascadeElection(t, e, "Grizzly Bears")
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 60)
	// The declined land is bottomed by the residue; the found card was cast.
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("declined land in %v, want the library", got)
	}
	if got := e.G.Obj(bearID).Zone; got != state.ZBattlefield {
		t.Fatalf("free-cast Grizzly Bears in %v, want the battlefield", got)
	}
	if n := movedTo(t, e, forestID, state.ZLibrary, state.ZExile); n != 1 {
		t.Fatalf("land exiled %d times", n)
	}
	if n := movedTo(t, e, forestID, state.ZExile, state.ZLibrary); n != 1 {
		t.Fatalf("land returned to library %d times, want exactly 1", n)
	}
	replayCheck(t, e, cfg)
}

// avernaOnBattlefield asserts Averna is on seat 0's battlefield and returns
// its id.
func avernaOnBattlefield(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Averna, the Chaos Bloom" {
			return id
		}
	}
	t.Fatal("Averna, the Chaos Bloom is not on the battlefield: the R: line's ActiveZones$ Battlefield gate cannot be exercised")
	return 0
}
