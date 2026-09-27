package rules

import (
	"testing"

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
