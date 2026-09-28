package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cardOwnerScopeFixture puts Xantcha on the battlefield owned by player 0 and
// controlled by player 1 — the shape a source-aware CardOwner resolution would
// match if it leaked past CantAttack's Target$ walk — and asserts every
// precondition the assertions below depend on.
func cardOwnerScopeFixture(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	x := corpusCard(t, "Xantcha, Sleeper Agent")
	id := onBoardCard(t, e, 0, x)
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 1})
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Xantcha must be on the battlefield: %+v", o)
	}
	if o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: want owner 0 / controller 1, got owner=%v controller=%v", o.Owner, o.Controller)
	}
	return e, id
}

// TestCantPutCounterValidPlayerCardOwnerStaysSourceLess pins the SCOPE of the
// Player.CardOwner resolution: only CantAttack's Target$ walk passes the
// restriction source (the brief's scope; the review finding that reverted the
// PutCounterBlocked expansion). A CantPutCounter ValidPlayer$ Player.CardOwner
// selector with the source object's OWN owner as the affected player must
// behave exactly as it did before the CardOwner resolution existed — fail
// closed, block nobody.
func TestCantPutCounterValidPlayerCardOwnerStaysSourceLess(t *testing.T) {
	t.Parallel()
	e, id := cardOwnerScopeFixture(t)
	e.AddContinuous(ContinuousEffect{Source: id, Controller: 1, Restriction: "CantPutCounter",
		RestrictParams: map[string]string{"CounterType": "P1P1", "ValidPlayer": "Player.CardOwner"}})

	if e.PutCounterBlocked("P1P1", id, 0, true) {
		t.Fatal("CantPutCounter ValidPlayer$ Player.CardOwner blocked the source's owner: the source-aware CardOwner resolution leaked into PutCounterBlocked")
	}

	// Positive control, same selector grammar: ValidPlayer$ You (the effect's
	// controller 1) MUST block player 1, proving the ValidPlayer$ walk itself
	// is live and the CardOwner miss above is the source-less fail-closed rule,
	// not a dead path.
	e.AddContinuous(ContinuousEffect{Source: id, Controller: 1, Restriction: "CantPutCounter",
		RestrictParams: map[string]string{"CounterType": "P1P1", "ValidPlayer": "You"}})
	if !e.PutCounterBlocked("P1P1", id, 1, true) {
		t.Fatal("positive control failed: CantPutCounter ValidPlayer$ You did not block its controller")
	}
}

// TestCanAttackDefenderValidAttackedCardOwnerStaysSourceLess pins the same
// scope rule for CanAttackDefender's ValidAttacked$ selector: with a
// Player.CardOwner spec and the source's owner as the would-be defender, the
// permission must NOT hold (fail closed, the pre-CardOwner behavior), so a
// Defender wall still keeps the creature home.
func TestCanAttackDefenderValidAttackedCardOwnerStaysSourceLess(t *testing.T) {
	t.Parallel()
	e, id := cardOwnerScopeFixture(t)
	e.AddContinuous(ContinuousEffect{Source: id, Controller: 1, Restriction: "CanAttackDefender",
		// ValidCard$ Card.Self is required for restrictionApplies to reach the
		// ValidAttacked$ spec at all (an empty ValidCard means the effect's
		// remembered set gates it instead).
		RestrictParams: map[string]string{"ValidCard": "Card.Self", "ValidAttacked": "Player.CardOwner"}})

	if e.attackAllowedThroughDefender(id, 0) {
		t.Fatal("CanAttackDefender ValidAttacked$ Player.CardOwner admitted an attack on the source's owner: the source-aware CardOwner resolution leaked into attackAllowedThroughDefender")
	}

	// Positive control, same selector grammar: ValidAttacked$ You (the
	// effect's controller 1) MUST admit an attack on player 1, proving the
	// ValidAttacked$ walk itself is live and the CardOwner miss above is the
	// source-less fail-closed rule, not a dead path.
	e.AddContinuous(ContinuousEffect{Source: id, Controller: 1, Restriction: "CanAttackDefender",
		RestrictParams: map[string]string{"ValidCard": "Card.Self", "ValidAttacked": "You"}})
	if !e.attackAllowedThroughDefender(id, 1) {
		t.Fatal("positive control failed: CanAttackDefender ValidAttacked$ You did not admit an attack on its controller")
	}
}
