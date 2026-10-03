package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
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

// Pin both the full permutation and the re-entry guard on a real chained SA.
// Ponder's optional shuffle is now real (task inbox-paramcensus-final-stragglers):
// the arrange answer is followed by the may-shuffle ask, which this oracle
// declines so the assertions below stay exactly the permutation/draw-once
// contract they always certified (the shuffle arm itself is pinned in
// rules/ponder_test.go).
func TestPonderArrangeResumesDrawExactlyOnce(t *testing.T) {
	tapeLegacyOnly(t)
	e := crResolutionEngine(t, []string{"Ponder"}, nil)
	id := crAbortMove(t, e, 0, "Ponder", state.ZHand)
	sa := e.G.Obj(id).Face().SpellAbility()
	if sa == nil || sa.API != "RearrangeTopOfLibrary" || sa.Sub == nil || sa.Sub.API != "Draw" {
		t.Fatal("CR 608.2c: real Ponder arrange/draw chain missing")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.askPriority(0)
	crAbortAnswer(t, e, "Ponder", crAbortOption(t, e, "Ponder", "cast", id))
	crResolutionRound(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KArrange || d.Min != 3 || d.Max != 3 || len(d.Options) != 3 || !e.Suspended() {
		t.Fatalf("CR 608.2c: expected suspended full permutation, got %+v", d)
	}
	before := slices.Clone(e.G.Zone(state.ZLibrary, 0))
	if len(before) < 5 {
		t.Fatal("CR 608.2c: need untouched remainder")
	}
	for i, o := range d.Options {
		if o.Obj != before[i] {
			t.Fatal("CR 608.2c: top options do not describe live top")
		}
	}
	draws := countDraw(e)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Ponder", d.Options[2].Index, d.Options[0].Index, d.Options[1].Index)
	// The may-shuffle ask the arrange answer re-entry poses: decline, so the
	// oracle below sees exactly the arrangement + draw it always certified.
	sd := e.Pending()
	if sd == nil || sd.ResumeKind != "arrange_mayshuffle" {
		t.Fatalf("CR 608.2c: expected the may-shuffle ask, got %+v", sd)
	}
	noIdx := -1
	for _, o := range sd.Options {
		if o.Kind == "no" {
			noIdx = o.Index
		}
	}
	crAbortAnswer(t, e, "Ponder", noIdx)
	wantOrder := append([]state.ObjID{before[2], before[0], before[1]}, before[3:]...)
	orders := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.LibraryOrder {
			orders++
			if !slices.Equal(ev.IDs, wantOrder) {
				t.Fatalf("CR 608.2c: permutation=%v want %v", ev.IDs, wantOrder)
			}
		}
	}
	if orders != 1 || countDraw(e) != draws+1 || e.G.Obj(id).Zone != state.ZGraveyard || e.Suspended() {
		t.Fatalf("CR 608.2c/608.2n: orders=%d draws=%d zone=%s suspended=%v", orders, countDraw(e)-draws, e.G.Obj(id).Zone, e.Suspended())
	}
	if !slices.Equal(e.G.Zone(state.ZLibrary, 0), wantOrder[1:]) {
		t.Fatal("CR 608.2c: draw did not consume chosen top, or remainder changed")
	}
	t.Log("real Ponder: full permutation applied, chained draw once, no re-ask, spell completed; the may-shuffle ask was declined to keep this oracle")
}
