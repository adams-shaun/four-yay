package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const metalcraftVictim = "Name:Metalcraft Victim\nTypes:Creature\nPT:2/2\nOracle:x\n"
const metalcraftAttacker = "Name:Metalcraft Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n"

// metalcraftSpell sets up a real corpus spell, its creature target and exactly
// the requested number of battlefield artifacts using logged moves.
func metalcraftSpell(t *testing.T, seed uint64, name string, artifacts int) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	e, cfg, spell := gateFixture(t, seed, name, metalcraftVictim, metalcraftAttacker,
		"Name:Metalcraft Bauble 1\nTypes:Artifact\nOracle:x\n",
		"Name:Metalcraft Bauble 2\nTypes:Artifact\nOracle:x\n",
		"Name:Metalcraft Bauble 3\nTypes:Artifact\nOracle:x\n")
	victim := gateMoveFromLibrary(t, e, "Metalcraft Victim", state.ZBattlefield)
	for i, n := range []string{"Metalcraft Bauble 1", "Metalcraft Bauble 2", "Metalcraft Bauble 3"} {
		if i < artifacts {
			gateMoveFromLibrary(t, e, n, state.ZBattlefield)
		}
	}
	if z := e.G.Obj(victim).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: victim zone %s", z)
	}
	if got := e.metalcraftHolds(0); got != (artifacts >= 3) {
		t.Fatalf("precondition: artifact threshold with %d artifacts = %v", artifacts, got)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZHand {
		t.Fatalf("precondition: spell zone %s", z)
	}
	return e, cfg, spell, victim
}

func TestDispatchBareMetalcraft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		count int
		want  state.Zone
	}{
		{"below threshold", 2, state.ZBattlefield}, {"at threshold", 3, state.ZExile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, spell, victim := metalcraftSpell(t, uint64(995+tc.count), "Dispatch", tc.count)
			addMana(t, e, 0, "W")
			submitChoices(t, e, castOptMode(t, castOptions(t, e), spell, "").Index)
			submitChoices(t, e, targetOptionFor(t, e, victim))
			passUntilStackEmpty(t, e, 20)
			o := e.G.Obj(victim)
			if o.Zone != tc.want {
				t.Fatalf("Dispatch target zone = %s, want %s", o.Zone, tc.want)
			}
			if tc.want == state.ZBattlefield && !o.Tapped {
				t.Fatal("Dispatch did not tap its unexiled target")
			}
			if z := e.G.Obj(spell).Zone; z != state.ZGraveyard {
				t.Fatalf("Dispatch zone = %s, want graveyard", z)
			}
			replayCheck(t, e, cfg)
		})
	}
}

func TestConcussiveBoltBareMetalcraft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		count   int
		blocked bool
	}{
		{"below threshold", 2, false}, {"at threshold", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, spell, victim := metalcraftSpell(t, uint64(1005+tc.count), "Concussive Bolt", tc.count)
			// The player targeted by Bolt controls the victim creature. Give the
			// opponent a creature attacking that player for the blocking oracle.
			attacker := gateMoveFromLibrary(t, e, "Metalcraft Attacker", state.ZBattlefield)
			e.emit(events.Event{Kind: events.ControlChange, Obj: attacker, Player: 1})
			if o := e.G.Obj(attacker); o.Zone != state.ZBattlefield || o.Controller != 1 {
				t.Fatalf("precondition: attacker = %+v", o)
			}
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
			if !e.canBlock(victim, attacker) {
				t.Fatal("precondition: ordinary victim must be able to block attacker")
			}
			before := e.G.Players[0].Life
			addMana(t, e, 0, "RRRRR")
			submitChoices(t, e, castOptMode(t, castOptions(t, e), spell, "").Index)
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("expected player target ask, got %+v", d)
			}
			idx := -1
			for _, opt := range d.Options {
				if opt.Kind == "player" && opt.Player == 0 {
					idx = opt.Index
				}
			}
			if idx < 0 {
				t.Fatalf("player 0 not offered: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			passUntilStackEmpty(t, e, 20)
			if got := e.G.Players[0].Life; got != before-4 {
				t.Fatalf("Bolt damage: life %d, want %d", got, before-4)
			}
			if z := e.G.Obj(victim).Zone; z != state.ZBattlefield {
				t.Fatalf("victim zone = %s", z)
			}
			// The rider must reach the derived creature as a PumpAll grant,
			// then prohibit a real block through the shared combat oracle.
			if got := e.HasKeyword(victim, "HIDDEN CARDNAME can't block."); got != tc.blocked {
				t.Fatalf("can't-block grant = %v, want %v", got, tc.blocked)
			}
			if got := e.blockRestricted(victim, attacker); got != tc.blocked {
				t.Fatalf("block restriction = %v, want %v", got, tc.blocked)
			}
			if got := e.canBlock(victim, attacker); got == tc.blocked {
				t.Fatalf("canBlock = %v, want %v", got, !tc.blocked)
			}
			if z := e.G.Obj(spell).Zone; z != state.ZGraveyard {
				t.Fatalf("Bolt zone = %s, want graveyard", z)
			}
			replayCheck(t, e, cfg)
		})
	}
}
