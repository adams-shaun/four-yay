package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestEffectContinuousExpiresAtEndOfTurn pins the other half of the registry
// feature: a registered continuous effect does not live forever. The Vines
// restriction is UntilEOT (an instant-sourced effect), and the end-of-turn
// cleanup (CR 514.2, rules.Engine.EndOfTurnCleanup) must drop it so the
// creature is once again targetable by the opponent. This deliberately lives
// in its own file: it reads new engine internals (restrictionBlocksTarget /
// AddContinuous) that do not exist on the base tree, so it is not part of the
// fail-on-base per-card proof.
func TestEffectContinuousExpiresAtEndOfTurn(t *testing.T) {
	t.Parallel()
	vines := card(t, "Name:Vines of Vastwood\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +4 | NumDef$ +4 | SubAbility$ DBEffect\n"+
		"SVar:DBEffect:DB$ Effect | Defined$ Targeted | StaticAbilities$ STCantTarget | RememberObjects$ Targeted\n"+
		"SVar:STCantTarget:Mode$ CantTarget | ValidTarget$ Card.IsRemembered | Activator$ Player.Opponent\nOracle:x\n")
	e := handEngine(t, vines)
	e.G.Players[0].Pool[state.MG] = 1
	creature := onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == creature {
			idx = o.Index
		}
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)

	// While on the current turn the restriction is live.
	if !e.restrictionBlocksTarget(creature, 1) {
		t.Fatal("restriction should bite the opponent during this turn")
	}
	// Clearing the effect the way the cleanup step does (CR 514.2) must drop it.
	e.EndOfTurnCleanup()
	if e.restrictionBlocksTarget(creature, 1) {
		t.Fatal("restriction survived end of turn; an UntilEOT effect must expire")
	}
	// A second cleanup is idempotent, and a permanent-sourced (non-EOT) effect
	// must not be dropped by it: add one and confirm EndOfTurnCleanup keeps it.
	e.AddContinuous(ContinuousEffect{
		Source: creature, Controller: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature", AddPower: 2, UntilEOT: false,
	})
	e.EndOfTurnCleanup()
	if e.Power(creature) != 4 {
		t.Fatalf("non-EOT effect dropped by cleanup: power %d, want 4", e.Power(creature))
	}
}
