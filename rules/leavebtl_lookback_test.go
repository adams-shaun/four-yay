package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 603.10a look-back for granted leaves-the-battlefield triggers on the
// NON-SBA departure routes (the Oracle-text audit's Relic Vial rows,
// oracleKnownDivergent's former "Relic Vial/only-cleric-dies-looks-back" and
// "Relic Vial/sacrifice-only-cleric-as-cost-looks-back"): when the permanent
// whose presence a conditional AddTrigger$ grant names IS the permanent
// departing, the granted trigger must be matched against the board
// immediately BEFORE the event, exactly as the SBA paths
// (rules/sba.go's triggerBefore parking) already do for damage deaths. The
// fixtures are inline-authored (never corpus .txt, per the licensing rule);
// Vial Fixture mirrors Relic Vial's conditional grant shape,
// Cleric Fixture is the named Cleric, and Push Fixture/Zap Fixture are the
// destroy and lethal-damage spells the two routes are driven through.

var lbVial = "Name:Vial Fixture\nManaCost:3\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ 2 T Sac<1/Creature> | NumCards$ 1 | SpellDescription$ Draw a card.\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | AddTrigger$ TrigDrain | IsPresent$ Cleric.YouCtrl | Description$ As long as you control a Cleric, CARDNAME has \"Whenever a creature you control dies, each opponent loses 1 life and you gain 1 life.\"\n" +
	"SVar:TrigDrain:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature.YouCtrl | TriggerZones$ Battlefield | Execute$ TrigLoseLife | TriggerDescription$ Whenever a creature you control dies, each opponent loses 1 life and you gain 1 life.\n" +
	"SVar:TrigLoseLife:DB$ LoseLife | Defined$ Player.Opponent | LifeAmount$ 1 | SubAbility$ DBGainLife\n" +
	"SVar:DBGainLife:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"Oracle:x\n"

var lbCleric = "Name:Cleric Fixture\nManaCost:1 R\nTypes:Creature Human Cleric\nPT:2/2\nOracle:x\n"
var lbPush = "Name:Push Fixture\nManaCost:R\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature\nOracle:x\n"
var lbZap = "Name:Zap Fixture\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3\nOracle:x\n"

// lbGrantLive asserts the precondition every scenario here leans on: the
// Vial is on the battlefield, the Cleric is a Cleric creature seat 0
// controls on the battlefield, and the conditional grant is therefore LIVE
// (an AddTrigger$ ContinuousEffect is active). A scenario whose grant is
// dead proves nothing about look-back.
func lbGrantLive(t *testing.T, e *Engine, vialID, clericID state.ObjID) {
	t.Helper()
	vo, co := e.G.Obj(vialID), e.G.Obj(clericID)
	if vo == nil || vo.Zone != state.ZBattlefield {
		t.Fatalf("vial precondition: zone %v", vo)
	}
	if co == nil || co.Zone != state.ZBattlefield || co.Controller != 0 {
		t.Fatalf("cleric precondition: %+v", co)
	}
	isCleric := false
	if f := co.Face(); f != nil {
		for _, ty := range f.Types {
			if ty == "Cleric" {
				isCleric = true
			}
		}
	}
	if !isCleric {
		t.Fatalf("cleric precondition: not a Cleric: %+v", co.Face())
	}
	live := false
	for _, ce := range e.active() {
		if ce.AddTrigger != nil && ce.Source == vialID {
			live = true
		}
	}
	if !live {
		t.Fatal("grant precondition: the IsPresent$-Cleric AddTrigger$ grant is not live")
	}
}

// lbDestroyedDrains drives one destroy of the only Cleric and asserts the
// drain: life 20/20 at the start (so the drain is measurable), the Cleric in
// seat 0's graveyard, and 21/19 after.
func lbDestroyedDrains(t *testing.T, e *Engine, cfg Config, weaponSrc string, vialID, clericID state.ObjID) {
	t.Helper()
	weaponID := moveSeeded(t, e, 0, weaponSrc, state.ZHand)
	lbGrantLive(t, e, vialID, clericID)
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: life %d/%d, want 20/20 so the drain is measurable",
			e.G.Players[0].Life, e.G.Players[1].Life)
	}
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, weaponID).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending %+v, want the weapon's target decision", d)
	}
	tgtIdx := -1
	for _, o := range d.Options {
		if o.Obj == clericID {
			tgtIdx = o.Index
		}
	}
	if tgtIdx < 0 {
		t.Fatal("the only Cleric was not offered as the weapon's target")
	}
	submitChoices(t, e, tgtIdx)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(clericID); o.Zone != state.ZGraveyard {
		t.Fatalf("the Cleric did not die: zone %s", o.Zone)
	}
	if e.G.Players[0].Life != 21 || e.G.Players[1].Life != 19 {
		t.Fatalf("life %d/%d, want 21/19: the granted dies trigger did not look back (CR 603.10a)",
			e.G.Players[0].Life, e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}

func TestLeavesBattlefieldGrantLooksBackOnDestroy(t *testing.T) {
	e, cfg, vialID := newFixtureDeck(t, 61, lbVial, lbCleric, lbPush)
	clericID := moveSeeded(t, e, 0, lbCleric, state.ZBattlefield)
	moveSeeded(t, e, 0, lbVial, state.ZBattlefield)
	lbDestroyedDrains(t, e, cfg, lbPush, vialID, clericID)
}

func TestLeavesBattlefieldGrantLooksBackOnDamageDeath(t *testing.T) {
	// The SBA-route control: the same grant, the Cleric dying to lethal
	// damage. This already worked before the fix (the SBA paths park
	// triggerBefore themselves) and must keep working -- the emit-level
	// parking must not have changed the SBA route.
	e, cfg, vialID := newFixtureDeck(t, 62, lbVial, lbCleric, lbZap)
	clericID := moveSeeded(t, e, 0, lbCleric, state.ZBattlefield)
	moveSeeded(t, e, 0, lbVial, state.ZBattlefield)
	lbDestroyedDrains(t, e, cfg, lbZap, vialID, clericID)
}

func TestLeavesBattlefieldGrantLooksBackOnCostSacrifice(t *testing.T) {
	e, cfg, vialID := newFixtureDeck(t, 63, lbVial, lbCleric)
	clericID := moveSeeded(t, e, 0, lbCleric, state.ZBattlefield)
	moveSeeded(t, e, 0, lbVial, state.ZBattlefield)
	lbGrantLive(t, e, vialID, clericID)
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: life %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, vialID, 0)
	// The ability's cost is {2}, {T} and Sac<1/Creature>; the only creature
	// is the Cleric, so the sacrifice cost's singleton pose is answered
	// without a choice (the Scalding Tarn precedent).
	submitChoices(t, e, opt.Index)
	// The Sac<1/Creature> cost poses its own choose even for a single
	// candidate: answer it with the Cleric.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		sacIdx := -1
		for _, o := range d.Options {
			if o.Obj == clericID {
				sacIdx = o.Index
			}
		}
		if sacIdx < 0 {
			t.Fatalf("sacrifice cost decision %+v does not offer the Cleric", d)
		}
		submitChoices(t, e, sacIdx)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(clericID); o.Zone != state.ZGraveyard {
		t.Fatalf("the Cleric was not sacrificed: zone %s", o.Zone)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand %d, want %d: the Vial ability did not draw", got, handBefore+1)
	}
	if e.G.Players[0].Life != 21 || e.G.Players[1].Life != 19 {
		t.Fatalf("life %d/%d, want 21/19: the granted dies trigger did not look back at the cost sacrifice (CR 603.10a)",
			e.G.Players[0].Life, e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}

func TestLeavesBattlefieldGrantWithoutClericDrainsNobody(t *testing.T) {
	// The control that keeps the three scenarios above honest: with no
	// Cleric the grant is DEAD (asserted, so this is not a vacuous setup)
	// and destroying a creature drains nobody.
	e, _, vialID := newFixtureDeck(t, 64, lbVial, lbCleric, lbPush,
		"Name:Lions Fixture\nManaCost:W\nTypes:Creature Cat\nPT:2/1\nOracle:x\n")
	moveSeeded(t, e, 0, lbVial, state.ZBattlefield)
	// The Cleric stays in the hand: no Cleric you control.
	lionsID := moveSeeded(t, e, 0, "Name:Lions Fixture\nManaCost:W\nTypes:Creature Cat\nPT:2/1\nOracle:x\n",
		state.ZBattlefield)
	for _, ce := range e.active() {
		if ce.AddTrigger != nil && ce.Source == vialID {
			t.Fatal("control precondition failed: the grant is live without a Cleric")
		}
	}
	weaponID := moveSeeded(t, e, 0, lbPush, state.ZHand)
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, weaponID).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending %+v, want the weapon's target decision", d)
	}
	tgtIdx := -1
	for _, o := range d.Options {
		if o.Obj == lionsID {
			tgtIdx = o.Index
		}
	}
	if tgtIdx < 0 {
		t.Fatal("the Cat was not offered as the weapon's target")
	}
	submitChoices(t, e, tgtIdx)
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("life %d/%d, want 20/20: a dead grant must not drain",
			e.G.Players[0].Life, e.G.Players[1].Life)
	}
}

// TestLeavesBattlefieldGrantDrainsOnceOnLookBack: the split gate the fix
// added to the AddTrigger granted walk must queue the battlefield-origin
// grant in the look-back pass ALONE. Without it every departure queued the
// grant once per pass (observer + live) and each opponent lost 2 life.
func TestLeavesBattlefieldGrantDrainsOnceOnLookBack(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 65, lbVial, lbCleric, lbPush)
	clericID := moveSeeded(t, e, 0, lbCleric, state.ZBattlefield)
	moveSeeded(t, e, 0, lbVial, state.ZBattlefield)
	weaponID := moveSeeded(t, e, 0, lbPush, state.ZHand)
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, weaponID).Index)
	d := e.Pending()
	tgtIdx := -1
	for _, o := range d.Options {
		if o.Obj == clericID {
			tgtIdx = o.Index
		}
	}
	if d == nil || d.Kind != decision.KTarget || tgtIdx < 0 {
		t.Fatalf("pending %+v, want a target decision offering the Cleric", d)
	}
	submitChoices(t, e, tgtIdx)
	passUntilStackEmpty(t, e, 20)
	// The Cleric has died to the resolving Push: exactly ONE granted drain
	// may have run for the departure. The log is the record of what ran --
	// one LoseLife on the opponent and one GainLife on seat 0. The old
	// double-queue (observer pass + live pass, before the split gate) left
	// two of each.
	lose, gain := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.LifeChange {
			continue
		}
		switch {
		case ev.Player == 1 && ev.Amount == -1:
			lose++
		case ev.Player == 0 && ev.Amount == 1:
			gain++
		}
	}
	if lose != 1 || gain != 1 {
		t.Fatalf("drain LifeChanges: opponent-lose=%d you-gain=%d, want 1/1 (a double queue drains twice)",
			lose, gain)
	}
	if e.G.Players[0].Life != 21 || e.G.Players[1].Life != 19 {
		t.Fatalf("life %d/%d, want 21/19", e.G.Players[0].Life, e.G.Players[1].Life)
	}
}
