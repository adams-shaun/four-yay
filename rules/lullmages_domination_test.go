package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestLullmagesDominationTargetControllerDiscount pins the real-corpus
// reduction (agent-20260928T043626Z-b7e271c1): Lullmage's Domination's
// ReduceCost static carries its discount entirely in an SVar chain
// (`Amount$ XGrave`, `SVar:XGrave:Count$Compare CheckTgt GE8.3.0`,
// `SVar:CheckTgt:TargetedController$CardsInGraveyard`). Before the fix the
// TargetedController$ count head resolved to (0, false), so Count$Compare read
// 0, GE8 failed, and the generic {3} was never reduced. This test prices the
// real card with a chosen target whose controller has 8+ cards in its
// graveyard and asserts the generic drops to 0, with the no-target case as the
// negative control.
func TestLullmagesDominationTargetControllerDiscount(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Lullmage's Domination"))
	spell := e.G.Zone(state.ZHand, 0)[0]

	// Seat 1's creature is the chosen target; seat 1's graveyard holds eight
	// cards. Seat 0's own graveyard is empty, so a wrong-perspective read (the
	// resolving controller) would leave the discount unearned.
	tgt := battlePerm(t, e, 1, "Name:Mark\nManaCost:2 U\nTypes:Creature Merfolk\nPT:2/2\nOracle:x\n")
	for i := 0; i < 8; i++ {
		c := e.G.AddObject(card(t, "Name:Past\nManaCost:U\nTypes:Instant\nOracle:x\n"), 1)
		c.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), c.ID))
	}

	// Preconditions: the spell is in the zone the static reads, the target is
	// seat 1's battlefield creature, seat 1's graveyard really has 8+ cards,
	// and seat 0's does not -- without all of these the assertions are vacuous.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: spell zone = %v, want hand", o.Zone)
	}
	if o := e.G.Obj(tgt); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: target zone=%v controller=%v, want battlefield seat 1", o.Zone, o.Controller)
	}
	if n := len(e.G.Zone(state.ZGraveyard, 1)); n < 8 {
		t.Fatalf("precondition: seat 1 graveyard = %d, want >=8", n)
	}
	if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 graveyard = %d, want 0", n)
	}

	base := e.parseCost("3 U U U")
	if base.Generic != 3 {
		t.Fatalf("precondition: base generic = %d, want 3", base.Generic)
	}
	statics := e.collectCostStatics()

	// Negative control: with no target bound the controller read has no
	// players, so the discount must NOT apply ({3} stays {3}). This row would
	// also pass if the static were never applied at all, which is why the
	// positive row below is the real assertion.
	none := e.costModifiersWithTargetsUsing(statics, 0, spell, spellScope(""), nil, true).apply(base)
	if none.Generic != 3 {
		t.Fatalf("negative control: no-target generic = %d, want 3", none.Generic)
	}

	// The real assertion: one chosen target whose controller has 8+ graveyard
	// cards earns the full {3} reduction.
	got := e.costModifiersWithTargetsUsing(statics, 0, spell, spellScope(""), []state.Target{{Obj: tgt}}, true).apply(base)
	if got.Generic != 0 {
		t.Fatalf("TargetedController discount: generic = %d, want 0 ({3} less)", got.Generic)
	}
}
