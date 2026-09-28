package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A graveyard replacement leaves mill provenance on the proposed move but
// cannot turn the redirected exile move into a completed mill (CR 701.17a).
func TestMillTriggerRedirectToExileDoesNotCount(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	glowing := searchCorpusCard(t, reg, "Glowing One")
	mothman := searchCorpusCard(t, reg, "The Wise Mothman")
	rip := searchCorpusCard(t, reg, "Rest in Peace")
	e, _ := millTriggerEngine(t, glowing, mothman, rip)
	glowingID := seatOnBattlefield(t, e, glowing, 0)
	mothmanID := seatOnBattlefield(t, e, mothman, 0)
	ripID := seatOnBattlefield(t, e, rip, 0)
	for _, id := range []state.ObjID{glowingID, mothmanID, ripID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("source %d must be on battlefield, got %+v", id, o)
		}
	}
	milled := orderLibraryTop(t, e, 0, "Grizzly Bears", 2)
	for _, id := range milled {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZLibrary || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
			t.Fatalf("precondition: proposed nonland mill %d = %+v", id, o)
		}
	}
	start := len(e.L.Events)
	life := e.G.Players[0].Life
	resolveMill(t, e, 0, 2)
	moves := 0
	for _, id := range milled {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("Rest in Peace did not redirect proposed mill %d to exile: %+v", id, o)
		}
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile {
			moves++
			if events.IsMill(ev) {
				t.Fatalf("redirected exile move counted as completed mill: %+v", ev)
			}
		}
		if ev.Kind == events.TriggerPush && (ev.Obj == glowingID || ev.Obj == mothmanID) {
			t.Fatalf("redirected mill fired trigger: %+v", ev)
		}
	}
	if moves != 2 {
		t.Fatalf("replacement produced %d library-to-exile moves, want 2", moves)
	}
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("Glowing One gained life from redirected mill: %d, want %d", got, life)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("redirected cards queued %d triggers, want none", len(e.pendingTriggers))
	}

	// Some replacements keep the initiating action's Text on their final
	// MoveZone. Exercise that provenance-preserving result as well: the real
	// Rest in Peace body above happens to emit a fresh, unmarked exile move.
	remaining := orderLibraryTop(t, e, 0, "Grizzly Bears", 1)[0]
	redirected := events.Mill(remaining, 0)
	redirected.To = state.ZExile
	e.emit(redirected)
	if o := e.G.Obj(remaining); o == nil || o.Zone != state.ZExile {
		t.Fatalf("marked replacement result did not reach exile: %+v", o)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("provenance-preserving exile move queued %d mill triggers, want none", len(e.pendingTriggers))
	}
}

// The marker by itself is insufficient even if a replacement carries it
// forward; non-library moves and redirected destinations are not mills.
func TestMillTriggerRequiresCompletedLibraryToGraveyardMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		from, to state.Zone
		want     bool
	}{
		{state.ZLibrary, state.ZGraveyard, true},
		{state.ZLibrary, state.ZExile, false},
		{state.ZHand, state.ZGraveyard, false},
	} {
		ev := events.Mill(1, 0)
		ev.From, ev.To = tc.from, tc.to
		if got := events.IsMill(ev); got != tc.want {
			t.Errorf("IsMill(%s -> %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
