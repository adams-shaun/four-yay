package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestLittjaraMirrorlakeCopyEntersWithACounterKernel: Littjara Mirrorlake's
// `AB$ CopyPermanent ... | WithCountersType$ P1P1` names no
// WithCountersAmount$, so the copy enters with ONE +1/+1 counter. The real
// compiled ability resolves (as a kernel probe) with the Bear as its chosen
// target.
func TestLittjaraMirrorlakeCopyEntersWithACounterKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := tokenWithCountersEngine(t, reg, "Littjara Mirrorlake", "Grizzly Bears")
	lake := searchMoveByName(t, e, "Littjara Mirrorlake", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear not on the battlefield (%+v)", o)
	}
	c := searchCorpusCard(t, reg, "Littjara Mirrorlake")
	var sa *cards.SA
	for _, ab := range c.Faces[0].Abilities {
		if ab.API == "CopyPermanent" {
			sa = ab
		}
	}
	if sa == nil {
		t.Fatal("Littjara Mirrorlake has no CopyPermanent ability")
	}
	if sa.Params["WithCountersType"] != "P1P1" {
		t.Fatalf("compiled WithCountersType$ = %q, want P1P1", sa.Params["WithCountersType"])
	}
	kr9Probe(e, func() {
		effects.Resolve(e, &effects.Ctx{Source: lake, Controller: 0,
			Targets: []state.Target{{Obj: bear}}, TargetsOffered: true}, sa)
	})
	tok := newestTokenOnBattlefield(t, e)
	der := e.Derived(tok.ID)
	if der.Power != 3 || der.Toughness != 3 {
		t.Fatalf("copy derived P/T = %d/%d, want 3/3 (2/2 bear + one counter)", der.Power, der.Toughness)
	}
	if got := e.G.Obj(tok.ID).Counter("P1P1"); got != 1 {
		t.Fatalf("copy P1P1 counters = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
