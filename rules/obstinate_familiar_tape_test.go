package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

const obstinateTapeDrawSrc = "Name:Obstinate Tape Draw\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Draw | NumCards$ 1\nOracle:Draw a card.\n"

// The optional replacement interrupts an actual resolving spell here, so its
// election must be answered inside the active resolution tape, not parked as
// a legacy decision after the spell continues.
func TestObstinateFamiliarOptionalDrawDuringResolutionUsesTape(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := kr8Fixture(t, 2, 20261005, obstinateTapeDrawSrc)
	familiar := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Obstinate Familiar"))
	if o := e.G.Obj(familiar); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Obstinate Familiar is not on the battlefield")
	}
	if len(e.G.Zone(state.ZLibrary, 0)) == 0 {
		t.Fatal("precondition: seat 0 library is empty")
	}

	before := resolve.ReadStats()
	tapeCastAndResolve(t, e, "Obstinate Tape Draw", "B")
	stats := resolve.ReadStats().Sub(before)
	if stats.Served == 0 {
		t.Fatalf("draw replacement was not served from the resolution tape: %+v", stats)
	}
	if d := e.Pending(); d != nil && d.Kind != "priority" {
		t.Fatalf("resolution left unexpected pending decision: %+v", d)
	}
}
