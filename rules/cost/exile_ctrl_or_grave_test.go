package cost

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestParseCostExileCtrlOrGrave(t *testing.T) {
	c := ParseCost("5 G ExileCtrlOrGrave<1/Cave.Other>")
	if c.Generic != 6 || c.Colored[4] != 1 || c.XMin != 0 || len(c.Exile) != 1 || len(c.Unknown) != 0 {
		t.Fatalf("parsed cost = %+v", c)
	}
	p := c.Exile[0]
	wantZones := uint8((1 << state.ZBattlefield) | (1 << state.ZGraveyard))
	if p.N != 1 || p.Spec != "Cave.Other" || p.ZoneSet != wantZones {
		t.Fatalf("exile part = %+v; want one Cave.Other from battlefield or graveyard (zone set %d)", p, wantZones)
	}
}
