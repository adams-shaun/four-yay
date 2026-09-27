package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestStaticEmitWalkVisitsOnlyStaticSources pins the battlefield hot subset
// used by staticEffects during token creation: a cold-token flood is classified
// once, and later appends do not put the cold prefix back in the scan.
func TestStaticEmitWalkVisitsOnlyStaticSources(t *testing.T) {
	if !staticZoneSkipVerify {
		t.Fatal("precondition: static-zone skip verifier is disabled")
	}
	const n = 500
	e := layerEngine(t)
	for i := 0; i < n; i++ {
		onBoard(t, e, 0, "Name:Vanilla\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	}
	static := onBoard(t, e, 0, "Name:Static\nTypes:Creature\nPT:1/1\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 1 | Description$ gets +1/+0\nOracle:x\n")
	bf := e.G.Zone(state.ZBattlefield, 0)
	if len(bf) != n+1 {
		t.Fatalf("precondition: battlefield has %d objects, want %d", len(bf), n+1)
	}
	if !objectStaticHotOn(e.G.Obj(static)) {
		t.Fatal("precondition: static source is not classified hot")
	}
	if objectStaticHotOn(e.G.Obj(bf[0])) {
		t.Fatal("precondition: vanilla token is not cold")
	}

	ids := e.staticSourceIDs(0, state.ZBattlefield)
	if len(ids) != 1 || ids[0] != static {
		t.Fatalf("static source walk = %v, want only static source %d (board %d)", ids, static, len(bf))
	}
	if got := len(e.staticEffects(nil)); got == 0 {
		t.Fatal("staticEffects lost the live battlefield static")
	}

	// A new static source appended after classification must be noticed while
	// retaining the cold prefix; this is the TokenCreate append boundary.
	added := onBoard(t, e, 0, "Name:Later Static\nTypes:Creature\nPT:1/1\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 2 | Description$ gets +2/+0\nOracle:x\n")
	ids = e.staticSourceIDs(0, state.ZBattlefield)
	if len(ids) != 2 || ids[0] != static || ids[1] != added {
		t.Fatalf("appended static source walk = %v, want [%d %d]", ids, static, added)
	}
}
