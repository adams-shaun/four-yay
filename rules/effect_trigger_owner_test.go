package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// An Effect's owner can differ from the controller of the card that created
// it (the opening-hand EffectOwner$ Opponent shape). The matcher must use the
// registration's owner for ValidTarget$ You, not the source card's controller.
func TestEffectTriggerMatchesRegistrationOwner(t *testing.T) {
	t.Parallel()
	watcher := card(t, "Name:OpponentsPromise\nTypes:Creature\nPT:1/1\n"+
		"SVar:Grant:DB$ Effect | Triggers$ Hook | EffectOwner$ Opponent | Duration$ UntilEndOfTurn\n"+
		"SVar:Hook:Mode$ DamageDone | ValidTarget$ You | Execute$ Pain | TriggerZones$ Command\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, watcher)
	id := e.G.Zone(state.ZHand, 0)[0]
	src := e.G.Obj(id)
	if src == nil || src.Zone != state.ZHand || src.Controller != 0 {
		t.Fatalf("precondition: source must be seat 0's hand card: %+v", src)
	}
	sa := cards.ResolveSVar(src.Face().SVars, "Grant")
	owners, ok := effects.EffectOwnerPlayers(e, &effects.Ctx{Controller: src.Controller}, sa.Params["EffectOwner"])
	if !ok || len(owners) != 1 || owners[0] != 1 || owners[0] == src.Controller {
		t.Fatalf("precondition: EffectOwner$ Opponent must differ from source controller: %v", owners)
	}
	// effEffect resolves EffectOwner$ from the resolving Ctx's controller
	// itself (the one home), so the resolution is entered as the creating
	// card's controller -- not as the already-resolved owner -- and the
	// registration lands on the opponent.
	ctx := &effects.Ctx{Source: id, Controller: src.Controller}
	effects.SetSVars(ctx, src.Face().SVars)
	effects.Resolve(e, ctx, sa)
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat || e.G.Delayed[0].Controller != 1 || e.G.Delayed[0].Source != id {
		t.Fatalf("precondition: expected a recurring Effect registration for seat 1: %+v", e.G.Delayed)
	}
	// The source's controller was damaged, not the Effect owner. A positive
	// match here is a false firing (and cannot be masked by a later correct hit).
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("source controller's damage fired effect owned by seat 1: %+v", e.pendingTriggers)
	}
	before := e.G.Players[1].Life
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Controller != 1 {
		t.Fatalf("Effect owner's damage must fire their trigger: %+v", e.pendingTriggers)
	}
	e.putTriggersOnStack()
	e.askPriority(1)
	passUntilStackEmpty(t, e, 8)
	if got := e.G.Players[1].Life; got != before-3 { // damage 1 + triggered LoseLife 2
		t.Fatalf("Effect owner's life = %d, want %d (damage and triggered loss)", got, before-3)
	}
}

func TestEffectTriggerMatchesCapturedMemory(t *testing.T) {
	t.Parallel()
	watcher := card(t, "Name:MemoryPromise\nTypes:Creature\nPT:1/1\n"+
		"SVar:Grant:DB$ Effect | Triggers$ Hook | Duration$ UntilEndOfTurn\n"+
		"SVar:Hook:Mode$ DamageDone | ValidTarget$ Creature.IsRemembered | Execute$ Pain | TriggerZones$ Command\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, watcher)
	id := e.G.Zone(state.ZHand, 0)[0]
	remembered := onBoard(t, e, 1, "Name:Remembered\nTypes:Creature\nPT:5/5\nOracle:x\n")
	other := onBoard(t, e, 1, "Name:Other\nTypes:Creature\nPT:5/5\nOracle:x\n")
	if remembered == other || e.G.Obj(remembered).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct creatures must be on battlefield")
	}
	face := e.G.Obj(id).Face()
	ctx := &effects.Ctx{Source: id, Controller: 0, Remembered: []state.Target{{Obj: remembered}}}
	effects.SetSVars(ctx, face.SVars)
	effects.Resolve(e, ctx, cards.ResolveSVar(face.SVars, "Grant"))
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != remembered {
		t.Fatalf("precondition: Effect did not capture the chosen creature: %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.Damage, Obj: other, Amount: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("uncaptured creature's damage fired Effect: %d", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.Damage, Obj: remembered, Amount: 1})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Controller != 0 {
		t.Fatalf("captured creature's damage should fire Effect: %+v", e.pendingTriggers)
	}
}
