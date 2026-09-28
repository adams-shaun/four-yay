package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// onBoardCard places an already-compiled *cards.Card (typically from the
// corpus registry) straight onto the battlefield, the same direct setup
// onBoard uses for a script string.
func onBoardCard(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZBattlefield
	o.SummonSick = true
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
	// Same eventless placement as onBoard: stale the derived memos the way
	// an emitted event would (see onBoard's note).
	e.staticEpoch = -1
	e.activeEpoch = -1
	return o.ID
}

// resolveAttackPump drives the common harness: place src and others on seat
// 0's battlefield, declare them all attacking, queue triggers and resolve the
// top of the stack.
func resolveAttackPump(t *testing.T, e *Engine, src state.ObjID, others ...state.ObjID) {
	t.Helper()
	ids := append([]state.ObjID{src}, others...)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: ids})
	e.putTriggersOnStack()
	e.resolveTop()
}

// TestGoblinPiledriverPumpsForEachOtherAttackingGoblin pins the user-reported
// defect end to end with the REAL corpus card: Goblin Piledriver's trigger
// (`SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +X`,
// `SVar:X:Count$Valid Goblin.attacking+Other`) resolved as +0/+0
// because Num saw the signed non-literal "+X", failed Atoi, missed the SVar
// table under the sign-prefixed key and degraded to zero. With the sign
// stripped the pump is +2/+0 per other attacking Goblin.
func TestGoblinPiledriverPumpsForEachOtherAttackingGoblin(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	pileCard, ok := reg.Lookup("Goblin Piledriver")
	if !ok {
		t.Fatal("corpus has no Goblin Piledriver")
	}

	t.Run("with one other attacking goblin", func(t *testing.T) {
		e := layerEngine(t)
		pile := onBoardCard(t, e, 0, pileCard)
		other := onBoard(t, e, 0, "Name:Goblin Skirmisher\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
		resolveAttackPump(t, e, pile, other)
		if got := e.Power(pile); got != 3 {
			t.Fatalf("power = %d, want 3 (1 base + 2 for the one other attacking Goblin)", got)
		}
		if got := e.Toughness(pile); got != 2 {
			t.Fatalf("toughness = %d, want 2 (NumDef is untouched)", got)
		}
	})

	t.Run("attacking alone stays 1/2", func(t *testing.T) {
		e := layerEngine(t)
		pile := onBoardCard(t, e, 0, pileCard)
		resolveAttackPump(t, e, pile)
		if got := e.Power(pile); got != 1 {
			t.Fatalf("power = %d, want 1 (no other attacking Goblin, no pump)", got)
		}
	})
}

// TestSignedPumpNegates pins the -X direction: Kagemaro, First to Suffer's
// shape (`NumAtt$ -X`) must FLIP the resolved value, not resolve as 0.
func TestSignedPumpNegates(t *testing.T) {
	t.Parallel()
	src := `Name:Dread Spirit
ManaCost:2 B
Types:Creature Spirit
PT:2/2
T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigShrink | TriggerDescription$ x
SVar:TrigShrink:DB$ Pump | Defined$ Self | NumAtt$ -X
SVar:X:Count$Valid Goblin.attacking+Other
Oracle:x
`
	e := layerEngine(t)
	spirit := onBoard(t, e, 0, src)
	other := onBoard(t, e, 0, "Name:Goblin Skirmisher\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	resolveAttackPump(t, e, spirit, other)
	if got := e.Power(spirit); got != 1 {
		t.Fatalf("power = %d, want 1 (2 base, -1 for the one other attacking Goblin)", got)
	}
}

// TestSignedInlineCountPump pins the signed inline form (`NumAtt$
// +Count$...`), where the sign sits directly on the expression rather than
// on an SVar name.
func TestSignedInlineCountPump(t *testing.T) {
	t.Parallel()
	src := `Name:Rally Goblin
ManaCost:R
Types:Creature Goblin
PT:1/1
T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +Count$Valid Goblin.attacking+Other
Oracle:x
`
	e := layerEngine(t)
	src1 := onBoard(t, e, 0, src)
	other := onBoard(t, e, 0, "Name:Goblin Skirmisher\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	resolveAttackPump(t, e, src1, other)
	if got := e.Power(src1); got != 2 {
		t.Fatalf("power = %d, want 2 (1 base + inline +Count$ of 1)", got)
	}
}

// TestSignedLiteralPumpEndToEnd pins that the Atoi path still carries a
// signed literal through the same trigger harness (-2 must not be read as 0
// or as +2).
func TestSignedLiteralPumpEndToEnd(t *testing.T) {
	t.Parallel()
	src := `Name:Heavy Spirit
ManaCost:2 B
Types:Creature Spirit
PT:2/2
T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ -2 | NumDef$ -2
Oracle:x
`
	e := layerEngine(t)
	spirit := onBoard(t, e, 0, src)
	resolveAttackPump(t, e, spirit)
	if got, want := e.Power(spirit), e.Toughness(spirit); got != 0 || want != 0 {
		t.Fatalf("P/T = %d/%d, want 0/0 (2/2 base with a -2/-2 signed literal pump)", got, want)
	}
}
