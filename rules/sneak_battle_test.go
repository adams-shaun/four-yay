package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSneakPreservesBattleDefenderThroughEntry pins CR 702.190b's non-player
// defender branch. The returned attacker is declared at a real battlefield
// Battle, the payment-time Choose event carries both protector and Battle id,
// and the entry event must preserve AttackingBattle rather than degrading to
// an attack at the protector's seat.
func TestSneakPreservesBattleDefenderThroughEntry(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Fatal("precondition: corpus registry missing (.cards not linked?)")
	}
	e, _ := ninjutsuDeck(t, 702190, searchCorpusCard(t, reg, "Oroku Saki, Shredder Rising"))
	battleCard := searchCorpusCard(t, reg, "Invasion of Tolvada")
	if battleCard.Faces[0] == nil || !battleCard.Faces[0].IsBattle() {
		t.Fatalf("precondition: Invasion of Tolvada is not a Battle: %+v", battleCard.Faces[0])
	}
	battle := e.G.AddObject(battleCard, 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: battle.ID, From: state.ZLibrary, To: state.ZBattlefield})
	if b := e.G.Obj(battle.ID); b == nil || b.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Battle is not on battlefield: %+v", b)
	}
	attacker := e.G.AddObject(card(t, ninjutsuBearSrc), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: attacker.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, Obj: battle.ID, IDs: []state.ObjID{attacker.ID}})
	if a := e.G.Obj(attacker.ID); a == nil || !a.IsAttacking || a.AttackingBattle != battle.ID || a.Attacking != 1 {
		t.Fatalf("precondition: unblocked attacker does not target the Battle: %+v", a)
	}

	stackPermanent := e.G.AddObject(card(t, "Name:Sneak entrant\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: stackPermanent.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Choose, Obj: stackPermanent.ID, Player: 0, Counter: "sneak-defender",
		IDs: []state.ObjID{state.PlayerRef(1), battle.ID}})
	// AttackingBattle is the required distinct target, not merely the
	// Battle's protector recorded in Attacking.
	e.sneakEnter(stackPermanent.ID, 0)
	got := e.G.Obj(stackPermanent.ID)
	if got == nil || got.Zone != state.ZBattlefield || !got.Tapped || !got.IsAttacking || got.Attacking != 1 || got.AttackingBattle != battle.ID {
		t.Fatalf("Sneak permanent = %+v, want tapped and attacking Battle %d (protector seat 1)", got, battle.ID)
	}
}
