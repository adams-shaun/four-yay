package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestPumpKnownKeysSorted: the unread lookup binary-searches the table.
func TestPumpKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(pumpKnownKeys[:]) || len(slices.Compact(slices.Clone(pumpKnownKeys[:]))) != len(pumpKnownKeys) {
		t.Fatal("pumpKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompilePump pins the compiled shapes the resolution and rules read:
// the KWChoice$ candidate list the ask offers AND the answer maps against
// (empty entries dropped, capacity clipped), the parsed PumpZone$, the
// registration grant, the unread report and the front cache.
func TestCompilePump(t *testing.T) {
	sa := &cards.SA{API: "Pump", Params: map[string]string{
		"KWChoice": "Flying, ,Vigilance", "PumpZone": "Graveyard,Exile", "NumAtt": "+2",
		"NumDef": "Double", "KW": "Haste & Trample", "Duration": "Permanent",
		"RememberPumped": "True", "Bogus": "1",
	}}
	p := PumpOf(sa)
	if !p.HasKWChoice || !slices.Equal(p.KWChoice, []string{"Flying", "Vigilance"}) || cap(p.KWChoice) != 2 {
		t.Fatalf("KWChoice = %q (cap %d)", p.KWChoice, cap(p.KWChoice))
	}
	if !p.PumpZoneOK || p.PumpZoneAll || !slices.Equal(p.PumpZones, []state.Zone{state.ZGraveyard, state.ZExile}) {
		t.Fatalf("PumpZone = %v %v %v", p.PumpZones, p.PumpZoneAll, p.PumpZoneOK)
	}
	if !slices.Equal(p.Grant.KW, []string{"Haste", "Trample"}) || cap(p.Grant.KW) != len(p.Grant.KW) ||
		p.Grant.Duration != "Permanent" || !p.RememberPumped || p.NumAtt.Text != "+2" {
		t.Fatalf("compiled = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Bogus"}) {
		t.Fatalf("unread = %v", p.Unread)
	}
	if allocs := allocsPerRun(100, func() { _ = PumpOf(sa) }); allocs != 0 {
		t.Fatalf("PumpOf front-cache hit allocates %v", allocs)
	}
	cp := *sa
	cp.Params = map[string]string{"KW": "Flying"}
	if got := PumpOf(&cp); got == p || !slices.Equal(got.Grant.KW, []string{"Flying"}) {
		t.Fatal("a rewritten Params map read the stale record")
	}
}
