package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestHexproofRecheckDropsTargetChosenBeforeTheGrant pins the RECHECK leg of
// kw:Hexproof (CR 702.11 + CR 608.2b): a creature is a legal target of an
// opponent's Shock at announcement — it has no hexproof then — and gains
// hexproof before resolution. legalTargets must drop the now-illegal target
// exactly as candidatesFor withheld it at cast time, so the Shock fizzles and
// deals no damage. The offer leg is pinned by TestPrintedHexproofWithholds…
// and TestCorpusLotusFieldHexproofWithholdsOpponent in rules/hexproof_test.go;
// this is the one leg the brief names ("offer and recheck both") that had no
// test. The grant rides an instant-speed KW$ Hexproof pump — the same real
// engine grant path (effects/combatfx.go registerPumpEffects) shroud's own
// recheck test uses — because equip is sorcery-speed and cannot be activated
// while the Shock is on the stack.
func TestHexproofRecheckDropsTargetChosenBeforeTheGrant(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	giant := findOnBoard(t, e, 1, "Hill Giant")

	// Precondition: the bear does NOT carry hexproof yet, or the target would
	// never be offered and the recheck would go untested.
	if e.hexproofBlocksTarget(bear, 1, 0) {
		t.Fatalf("Grizzly Bears unexpectedly carries hexproof before the grant: %v", e.Keywords(bear))
	}

	sh := e.G.AddObject(mustCorpusCard(t, reg, "Shock"), 1)
	sh.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), sh.ID))
	addMana(t, e, 1, "R")
	e.askPriority(1)
	castFirst(t, e, "cast")
	targetObject(t, e, bear) // bear is legal at announcement — Shock on the stack

	// In response, seat 0 casts an instant pump granting KW$ Hexproof.
	pump := e.G.AddObject(card(t, "Name:Blessing of the Wilds\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | KW$ Hexproof\nOracle:x\n"), 0)
	pump.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), pump.ID))
	addMana(t, e, 0, "G")
	e.askPriority(0)
	castFirst(t, e, "cast")
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)

	// The handler ran and the grant took: the bear now has hexproof.
	if !e.hexproofBlocksTarget(bear, 1, 0) {
		t.Fatalf("pump's KW$ Hexproof grant did not reach the bearer's derived keywords: %v", e.Keywords(bear))
	}
	// The recheck dropped the target: the Shock fizzled with no damage.
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(bear).Damage != 0 {
		t.Fatalf("Shock dealt damage to a target that gained hexproof before resolution: zone=%s damage=%d",
			e.G.Obj(bear).Zone, e.G.Obj(bear).Damage)
	}
	if e.G.Obj(giant).Zone != state.ZBattlefield {
		t.Fatal("Hill Giant unexpectedly affected")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after the fizzle: %v", e.G.Stack)
	}
}
