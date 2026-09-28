package rules

// Task greatestpower1: the extreme-reduction property suffixes
// ($GreatestCardPower and siblings) were not recognised by the zone-count
// dispatch, so a card whose amount is "the greatest power among creatures you
// control" sized itself to zero. This leaf drives the REAL Orcish Siegemaster
// script (pasted inline -- Forge scripts are GPL and never committed) through
// the attack-trigger machinery and asserts the pump's NumAtt is the greatest
// power among the controller's creatures, with two different board strengths
// so a hard-coded value cannot pass.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const orcishSiegemasterSrc = "Name:Orcish Siegemaster\nManaCost:2 R\nTypes:Creature Orc Soldier\nPT:0/5\n" +
	"K:Trample\n" +
	"S:Mode$ Continuous | Affected$ Goblin.YouCtrl+Other,Orc.YouCtrl+Other | AddKeyword$ Trample | Description$ Other Orcs and Goblins you control have trample.\n" +
	"T:Mode$ Attacks | ValidCard$ Card.Self | TriggerZones$ Battlefield | Execute$ TrigPump | TriggerDescription$ Whenever CARDNAME attacks, it gets +X/+0 until end of turn, where X is the greatest power among creatures you control.\n" +
	"SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +X\n" +
	"SVar:X:Count$Valid Creature.YouCtrl$GreatestCardPower\n" +
	"SVar:HasAttackEffect:TRUE\n" +
	"Oracle:Whenever Orcish Siegemaster attacks, it gets +X/+0 until end of turn, where X is the greatest power among creatures you control.\n"

const greatBeastSrc = "Name:Great Beast\nManaCost:4 G\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"

const smallBeastSrc = "Name:Small Beast\nManaCost:1 G\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"

// TestOrcishSiegemasterAttackPumpSizesFromGreatestPower is the brief's
// end-to-end leaf: the attacker's pump is exactly the greatest power among
// the controller's creatures, measured twice on different boards (5 then 7,
// when the Beast is pumped by +2/+2 counters), so a value hard-coded to a
// single board cannot pass.
func TestOrcishSiegemasterAttackPumpSizesFromGreatestPower(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		extras   []string
		prePower int32
		want     int32
	}{
		// Siegemaster is 0/5; the only other creature is the 5/5 Beast.
		{name: "five_power_board", extras: []string{greatBeastSrc}, want: 5},
		// Same board, with the Beast's power pushed to 7 by +2/+2 counters
		// before the attack: the DERIVED read must follow it.
		{name: "seven_power_board", extras: []string{greatBeastSrc}, prePower: 7, want: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, siegemaster := newFixtureDeck(t, 311, orcishSiegemasterSrc, tc.extras...)
			siegemaster = putCreature(t, e, 0, orcishSiegemasterSrc)
			beast := putCreature(t, e, 0, greatBeastSrc)
			if tc.prePower > 0 {
				// A logged CounterChange, not a direct mutation, so replayCheck
				// can rebuild it from the log.
				e.emit(events.Event{Kind: events.CounterChange, Obj: beast, Counter: "P1P1", Amount: tc.prePower - 5})
			}
			// Declare the Siegemaster as the sole attacker: the attack
			// trigger queues, then resolves its pump.
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{siegemaster}})
			if len(e.pendingTriggers) == 0 {
				t.Fatalf("no attack trigger queued for Orcish Siegemaster (pending %d)", len(e.pendingTriggers))
			}
			e.putTriggersOnStack()
			for len(e.G.Stack) > 0 {
				e.resolveTop()
			}
			if got := e.Power(siegemaster); got != tc.want {
				t.Fatalf("Orcish Siegemaster power after the attack pump = %d, want %d (the greatest power among the controller's creatures)", got, tc.want)
			}
			replayCheck(t, e, cfg)
		})
	}
}
