package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestReplacementEmitWalkVisitsOnlyReplacementSources pins the per-zone
// replacement hot subset on the mass-token battlefield shape.
func TestReplacementEmitWalkVisitsOnlyReplacementSources(t *testing.T) {
	if !replZoneSkipVerify {
		t.Fatal("precondition: replacement-zone skip verifier is disabled")
	}
	const n = 500
	e := layerEngine(t)
	for i := 0; i < n; i++ {
		onBoard(t, e, 0, "Name:Vanilla\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	}
	repl := onBoard(t, e, 0, "Name:Replacement\nTypes:Creature\nPT:1/1\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidToken$ Creature | ReplaceWith$ ShiftCtrl | Description$ creates differently\n"+
		"SVar:ShiftCtrl:DB$ ChangeController | Defined$ ReplacedToken | Controller$ You\nOracle:x\n")
	bf := e.G.Zone(state.ZBattlefield, 0)
	if len(bf) != n+1 {
		t.Fatalf("precondition: battlefield has %d objects, want %d", len(bf), n+1)
	}
	if !objectReplHot(e.G.Obj(repl)) {
		t.Fatal("precondition: printed replacement source is not classified hot")
	}
	if objectReplHot(e.G.Obj(bf[0])) {
		t.Fatal("precondition: vanilla token is not replacement-cold")
	}
	// An R:-bearing token-like permanent is the uncertainty boundary: it must
	// remain in the walk even beside the large cold prefix.
	rplToken := onBoard(t, e, 0, "Name:Replacement token\nTypes:Creature Goblin\nPT:1/1\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ReplaceWith$ None | Description$ replacement token\nOracle:x\n")
	if !objectReplHot(e.G.Obj(rplToken)) {
		t.Fatal("precondition: replacement-bearing token face was not classified hot")
	}

	var visited []state.ObjID
	e.forEachReplacementSource(func(id state.ObjID) { visited = append(visited, id) })
	if len(visited) != 2 || visited[0] != repl || visited[1] != rplToken {
		t.Fatalf("replacement walk visited %v, want hot objects [%d %d] in order (battlefield %d)", visited, repl, rplToken, len(bf)+1)
	}
}

// TestReplacementSkipVerifyCatchesUnreferencedWrite proves the
// replacement-zone-skip verifier is live in the rules test binary: a direct
// in-place write no event names -- the one input the summary argument cannot
// see -- must trip it rather than silently drop the newly-live replacement
// from the walk.
func TestReplacementSkipVerifyCatchesUnreferencedWrite(t *testing.T) {
	if !replZoneSkipVerify {
		t.Fatal("precondition: replacement-zone skip verifier is disabled")
	}
	e := layerEngine(t)
	cold := onBoard(t, e, 0, "Name:Vanilla\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	if objectReplHot(e.G.Obj(cold)) {
		t.Fatal("precondition: vanilla object is not replacement-cold")
	}
	// Classify the battlefield summary with the object cold.
	var seen []state.ObjID
	e.forEachReplacementSource(func(id state.ObjID) { seen = append(seen, id) })
	if len(seen) != 0 {
		t.Fatalf("precondition: replacement walk = %v, want empty", seen)
	}
	// Directly give the already-classified object an R:-bearing copy face
	// without emitting an event: the same bypass the trigger-walk verifier
	// catches.
	e.G.Obj(cold).CopyFace = card(t, "Name:Live\nTypes:Creature\nPT:1/1\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ReplaceWith$ None | Description$ live replacement\nOracle:x\n").Faces[0]
	defer func() {
		r := recover()
		s, ok := r.(string)
		if !ok || !strings.Contains(s, "replacement zone summary") {
			t.Fatalf("verify did not flag the skipped live replacement: %v", r)
		}
	}()
	e.forEachReplacementSource(func(state.ObjID) {})
	t.Fatal("replacement skip served a stale cold summary without a verify panic")
}

// TestReplacementAppendFastPathDuringTokenFlood drives the TokenCreate
// append fast path in replZoneHot (the branch keyed on a log suffix of
// TokenCreates for the same player): a cold token appended onto an already
// classified battlefield must not enter the walk, a replacement-bearing
// token appended in the same flood must, and the pre-existing hot source
// must keep its slot and order throughout.
func TestReplacementAppendFastPathDuringTokenFlood(t *testing.T) {
	if !replZoneSkipVerify {
		t.Fatal("precondition: replacement-zone skip verifier is disabled")
	}
	e := layerEngine(t)
	e.G.Tokens = make(map[string]*cards.Card)
	e.G.Tokens["cold"] = card(t, "Name:Plain token\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	e.G.Tokens["hot"] = card(t, "Name:Replacement token\nTypes:Creature Goblin\nPT:1/1\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ReplaceWith$ None | Description$ replacement token\nOracle:x\n")
	// A pre-existing hot source and a cold one, classified by a first walk.
	hot := onBoard(t, e, 0, "Name:Replacement\nTypes:Creature\nPT:1/1\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidToken$ Creature | ReplaceWith$ None | Description$ existing\nOracle:x\n")
	onBoard(t, e, 0, "Name:Vanilla\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	var visited []state.ObjID
	e.forEachReplacementSource(func(id state.ObjID) { visited = append(visited, id) })
	if len(visited) != 1 || visited[0] != hot {
		t.Fatalf("precondition: first walk = %v, want [%d]", visited, hot)
	}

	// Emit an append-only TokenCreate flood: one cold, one hot, one cold.
	for _, name := range []string{"cold", "hot", "cold"} {
		before := len(e.G.Zone(state.ZBattlefield, 0))
		got := e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: name})
		if got.Kind != events.TokenCreate || len(e.G.Zone(state.ZBattlefield, 0)) != before+1 {
			t.Fatalf("precondition: %q TokenCreate did not append one battlefield object", name)
		}
		created := e.G.Zone(state.ZBattlefield, 0)[before]
		if wantHot := objectReplHot(e.G.Obj(created)); wantHot != (name == "hot") {
			t.Fatalf("precondition: token %q hot = %v", name, wantHot)
		}
		visited = visited[:0]
		e.forEachReplacementSource(func(id state.ObjID) { visited = append(visited, id) })
		want := []state.ObjID{hot}
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if id != hot && objectReplHot(e.G.Obj(id)) {
				want = append(want, id)
			}
		}
		if !slices.Equal(visited, want) {
			t.Fatalf("after %q append, walk = %v, want %v", name, visited, want)
		}
	}
}
