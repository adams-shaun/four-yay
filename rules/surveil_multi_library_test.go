package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const multiLibrarySurveilSrc = "Name:Surveil Every Library\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Surveil | Defined$ Player | Amount$ 1\nOracle:x\n"

// TestSurveilMultiLibraryResumesEveryArrangement is the end-to-end regression
// for the shared library cursor. Each player's KArrange answer must apply to
// that player's own library, then resume at the next Defined$ player instead
// of re-starting (or ending) the walk.
func TestSurveilMultiLibraryResumesEveryArrangement(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 731, multiLibrarySurveilSrc)
	addMana(t, e, 0, "U")
	before0 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	before1 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...)
	if len(before0) < 2 || len(before1) < 2 || before0[0] == before1[0] {
		t.Fatalf("precondition: distinct libraries need two cards each, got %v / %v", before0, before1)
	}

	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KArrange || d.Player != 0 || d.ResumeTarget != 0 || len(d.Options) != 1 || d.Options[0].Obj != before0[0] {
		t.Fatalf("first arrange = %+v, want player 0 choosing its top card %d", d, before0[0])
	}
	// Player 0 chooses no cards to remain on top, so their only looked-at card
	// must go to their graveyard. This answer is intentionally different from
	// player 1's below, proving each continuation applies independently.
	submitChoices(t, e)

	d = e.Pending()
	if d == nil || d.Kind != decision.KArrange || d.Player != 1 || d.ResumeTarget != 1 || len(d.Options) != 1 || d.Options[0].Obj != before1[0] {
		t.Fatalf("second arrange = %+v, want player 1 choosing its top card %d", d, before1[0])
	}
	submitChoices(t, e, 0) // Player 1 keeps its looked-at top card.

	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after both arrangements, pending = %+v, want priority (resolution complete)", d)
	}
	if got := countSurveilMarkers(e); got != 2 {
		t.Fatalf("Surveil markers = %d, want one for each processed library", got)
	}
	after0 := e.G.Zone(state.ZLibrary, 0)
	if len(after0) != len(before0)-1 || after0[0] != before0[1] {
		t.Fatalf("player 0 library = %v, want first card %d surveilled to graveyard", after0, before0[1])
	}
	found0 := false
	for _, oid := range e.G.Zone(state.ZGraveyard, 0) {
		found0 = found0 || oid == before0[0]
	}
	if !found0 {
		t.Fatalf("player 0's surveilled card %d not in its graveyard %v", before0[0], e.G.Zone(state.ZGraveyard, 0))
	}
	after1 := e.G.Zone(state.ZLibrary, 1)
	if len(after1) != len(before1) || after1[0] != before1[0] {
		t.Fatalf("player 1 library = %v, want its kept top card %d unchanged", after1, before1[0])
	}
	for _, oid := range e.G.Zone(state.ZGraveyard, 1) {
		if oid == before1[0] {
			t.Fatalf("player 1's kept card %d reached its graveyard", before1[0])
		}
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Surveil && ev.Player != 0 && ev.Player != 1 {
			t.Fatalf("Surveil marker named non-library player: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}
