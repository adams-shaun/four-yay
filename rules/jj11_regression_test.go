package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The inactive-source conformance leaf cannot detect an inert replacement body.
// Exercise the real compiled Origin$ All body without a stack-exit backstop.
func TestActiveRestInPeaceReplacesGraveyardMove(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Rest in Peace"}, nil)
	rip := crAbortMove(t, e, 0, "Rest in Peace", state.ZBattlefield)
	target := crAbortMove(t, e, 0, "Delver of Secrets", state.ZBattlefield)
	checked := 0
	for _, r := range e.G.Obj(rip).Face().Repls {
		if r.Event == "Moved" && r.Params["ActiveZones"] == "Battlefield" && r.With != nil && r.With.Params["Origin"] == "All" && r.With.Params["Destination"] == "Exile" {
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("CR 614.1a/614.6: no real Origin All RIP replacement examined")
	}
	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.MoveZone, Obj: target, From: state.ZBattlefield, To: state.ZGraveyard})
	t.Logf("destination=%s events_added=%d", e.G.Obj(target).Zone, len(e.L.Events)-start)
	if e.G.Obj(target).Zone != state.ZExile {
		t.Fatal("CR 614.1a/614.6: active Rest in Peace must exile instead")
	}
	moves := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == target {
			moves++
			if ev.To != state.ZExile {
				t.Fatalf("CR 614.6: original graveyard event occurred: %+v", ev)
			}
		}
	}
	if moves != 1 {
		t.Fatalf("CR 614.6: replacement moves=%d want 1", moves)
	}
}

// Exile reached by a spell is not a cessation tombstone (CR 704.5d).
func TestSwordsTokenCeasesFromExile(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Raise the Alarm", "Swords to Plowshares"}, nil)
	raise := crAbortMove(t, e, 0, "Raise the Alarm", state.ZHand)
	f := e.G.Obj(raise).Face()
	if sa := f.SpellAbility(); sa == nil || sa.API != "Token" || sa.Params["TokenAmount"] != "2" {
		t.Fatal("CR 704.5d: Raise the Alarm fixture changed")
	}
	e.resolveAbility(raise, 0, nil, f.SpellAbility(), f.SVars)
	var tokens []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).IsToken {
			tokens = append(tokens, id)
		}
	}
	if len(tokens) != 2 {
		t.Fatalf("CR 704.5d: tokens=%v want 2", tokens)
	}
	swords := crAbortMove(t, e, 0, "Swords to Plowshares", state.ZHand)
	sf := e.G.Obj(swords).Face()
	e.resolveAbility(swords, 0, []state.Target{{Obj: tokens[0]}}, sf.SpellAbility(), sf.SVars)
	if e.G.Obj(tokens[0]).Zone != state.ZExile {
		t.Fatal("CR 704.5d: fixture never reached exile via Swords")
	}
	start := len(e.L.Events)
	e.checkStateBased()
	t.Logf("Swords token after SBA=%s", e.G.Obj(tokens[0]).Zone)
	for z := state.ZLibrary; z <= state.ZCommand; z++ {
		for p := range e.G.Players {
			if slices.Contains(e.G.Zone(z, state.PlayerID(p)), tokens[0]) {
				t.Fatalf("CR 704.5d: token remains in %s", z)
			}
		}
	}
	ceased := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == tokens[0] && ev.From == state.ZExile && ev.To == state.ZCeased {
			ceased++
		}
	}
	if ceased != 1 {
		t.Fatalf("CR 704.5d: exile cessation events=%d want 1", ceased)
	}
}
