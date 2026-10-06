package rules

// The max-speed player property (D8): two DFT cards read "max speed" as a
// property of a PLAYER rather than as a `Condition$ MaxSpeed` static, and
// gorge read neither spelling before this ticket.
//
//   - Hazoret, Godseeker's
//     `S:Mode$ CantAttack,CantBlock | ValidCard$ Card.Self |
//      CheckSVar$ PlayerCountPropertyYou$HasPropertyMaxSpeed |
//      SVarCompare$ NE1` -- the count head had no HasPropertyMaxSpeed arm, so
//     the SVar gate was unreadable, failed closed and the CantBlock static was
//     skipped whole (Hazoret blocked freely).
//   - Outpace Oblivion's
//     `A:AB$ DealDamage | Cost$ 2 Sac<1/CARDNAME/this enchantment> |
//      Defined$ Player.!MaxSpeed | NumDmg$ 2` -- the player filter had no
//     MaxSpeed qualifier, so `Player.!MaxSpeed` matched nobody and the ability
//     damaged nobody.
//
// Both reads resolve the SAME state.Player.Speed latch (an event-folded
// one-way count, CR 702.179), so these pins drive the real corpus cards
// through the real reads: the count head through the combat static gate
// (blockRestricted) and the player qualifier through the activated ability's
// resolution.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMaxSpeedPlayerProperty is the D8 end-to-end pin: at max speed the two
// spells behave as printed; below it they behave the other way. Each leg
// asserts its preconditions (the card on the battlefield, the two seats' speeds
// really straddling max speed, the compared life totals really differing).
func TestMaxSpeedPlayerProperty(t *testing.T) {
	t.Parallel()

	t.Run("Hazoret cannot block below max speed, can at max speed", func(t *testing.T) {
		t.Parallel()
		e := layerEngine(t)
		hazoret := onBoardCard(t, e, 0, corpusCard(t, "Hazoret, Godseeker"))
		attacker := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

		// Preconditions: Hazoret is on seat 0's battlefield and seat 0 starts
		// below max speed, so the two legs below really compare speed 1 and
		// speed 4 (a vacuous "0" read would prove nothing).
		if o := e.G.Obj(hazoret); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Hazoret is not on the battlefield (%v)", o)
		}
		if got := e.G.Players[0].Speed; got != 0 {
			t.Fatalf("precondition: seat 0 speed = %d, want 0", got)
		}

		// Speed 1 (below max): the SVar gate holds, so Hazoret's CantBlock is
		// enforced and it cannot be declared as a blocker.
		e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: 1})
		if got := e.G.Players[0].Speed; got != 1 {
			t.Fatalf("precondition: seat 0 speed = %d, want 1", got)
		}
		if !e.blockRestricted(hazoret, attacker) {
			t.Fatal("Hazoret could block at speed 1: HasPropertyMaxSpeed read as unreadable or nonzero")
		}

		// Max speed (4): the gate denies, the CantBlock is skipped and Hazoret
		// may block.
		e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: 3})
		if got := e.G.Players[0].Speed; got != maxSpeed {
			t.Fatalf("precondition: seat 0 speed = %d, want %d", got, maxSpeed)
		}
		if e.blockRestricted(hazoret, attacker) {
			t.Fatal("Hazoret could not block at max speed: HasPropertyMaxSpeed did not read 1")
		}
	})

	t.Run("Outpace Oblivion damages only players without max speed", func(t *testing.T) {
		t.Parallel()
		reg := testutil.CorpusRegistry(t)
		e, _ := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Outpace Oblivion")}, nil)
		outpace := moveByName(t, e, 0, "Outpace Oblivion", state.ZBattlefield)

		// Preconditions: the enchantment is on seat 0's battlefield, seat 0
		// is at max speed while seat 1 is not (the property's two answers
		// really differ), and both seats start at 20 life so the damage is
		// visible.
		if o := e.G.Obj(outpace); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Outpace Oblivion is not on the battlefield (%v)", o)
		}
		e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: maxSpeed})
		if got := e.G.Players[0].Speed; got != maxSpeed {
			t.Fatalf("precondition: seat 0 speed = %d, want %d", got, maxSpeed)
		}
		if got := e.G.Players[1].Speed; got != 0 {
			t.Fatalf("precondition: seat 1 speed = %d, want 0", got)
		}
		if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
			t.Fatalf("precondition: life = %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
		}

		// Pay the {2} + sacrifice cost and resolve the ability. toMain1 left a
		// priority decision computed before Outpace entered; clear it so the
		// re-ask enumerates the ability the board now offers.
		floatMana(t, e, 0, "CC")
		e.pending = nil
		e.priorityRound()
		submitChoices(t, e, abilityOptionFor(t, e, outpace).Index)
		passUntilStackEmpty(t, e, 30)

		// Only the speed-0 seat takes 2: seat 0 has max speed, seat 1 does not.
		if got := e.G.Players[0].Life; got != 20 {
			t.Fatalf("seat 0 (max speed) life = %d, want 20", got)
		}
		if got := e.G.Players[1].Life; got != 18 {
			t.Fatalf("seat 1 (no max speed) life = %d, want 18", got)
		}
	})
}
