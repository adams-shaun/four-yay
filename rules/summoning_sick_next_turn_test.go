package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// advanceToSeat0PriorityOnTurn passes every decision until the game is on the
// given turn with a pending seat-0 priority decision. Non-priority decisions
// take their first option, priorities belonging to another seat are passed.
// It is the real-flow driver for "give me a seat-0 window during the
// opponent's turn".
func advanceToSeat0PriorityOnTurn(t *testing.T, e *Engine, turn int32, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			if e.G.Over {
				t.Fatalf("game ended before reaching seat-0 priority on turn %d", turn)
			}
			t.Fatal("drained with no pending decision")
		}
		if e.G.Turn >= turn && d.Kind == decision.KPriority && d.Player == 0 {
			return
		}
		if d.Kind == decision.KPriority {
			passOnce(t, e)
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty non-priority decision %+v at turn %d", d, e.G.Turn)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	t.Fatalf("no seat-0 priority reached on turn %d", turn)
}

// TestSickCreatureTapsForManaOnTheOpponentsTurn pins the CR 302.6 scope rule:
// a creature's {T} ability needs it to have been under its controller's
// control "since their most recent turn began" -- and a turn begins for every
// player, so the START of turn 2 (the opponent's turn) ends the sickness of an
// elf cast on turn 1. Its {T} mana ability must therefore be offered in seat
// 0's turn-2 priority window. The bug: TurnChange cleared SummonSick only on
// the incoming ACTIVE player's battlefield, so the elf stayed sick through the
// opponent's whole turn and lost a full turn of its mana ability.
func TestSickCreatureTapsForManaOnTheOpponentsTurn(t *testing.T) {
	t.Parallel()
	elfSrc := "Name:Real Elf\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n"
	e, _, elf := newFixtureDeck(t, 9821, elfSrc)
	forest := onBoard(t, e, 0, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	toMain1(t, e)
	// Cast the elf through the real flow (turn 1, main1): tap the Forest, then
	// cast. The battlefield entry event is what stamps the sickness.
	e.pending = nil
	e.askPriority(0)
	forestIdx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == forest {
			forestIdx = o.Index
			break
		}
	}
	if forestIdx < 0 {
		t.Fatalf("Forest mana ability not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, forestIdx)
	e.pending = nil
	e.askPriority(0)
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == elf {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatal("fixture elf not offered for casting")
	}
	submitChoices(t, e, idx)
	resolveStack(t, e)

	// Precondition: the elf really entered via the cast this turn and is sick.
	if o := e.G.Obj(elf); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: elf zone=%s, want battlefield", o.Zone)
	}
	if !e.G.Obj(elf).SummonSick {
		t.Fatal("precondition: elf cast this turn must be summoning sick")
	}

	// Drive into the opponent's turn 2 and find seat 0's own priority window.
	passUntilTurn(t, e, 2)
	if e.G.Active != 1 {
		t.Fatalf("precondition: turn 2 active seat = %d, want opponent 1", e.G.Active)
	}
	advanceToSeat0PriorityOnTurn(t, e, 2, 400)

	// Preconditions for the assertion: the elf is still under seat 0's control,
	// on the battlefield, untapped, and turn 2 has ended its sickness.
	o := e.G.Obj(elf)
	if o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: elf zone=%s controller=%d, want battlefield under seat 0", o.Zone, o.Controller)
	}
	if o.Tapped {
		t.Fatal("precondition: elf must be untapped in the turn-2 window")
	}
	if o.SummonSick {
		t.Fatalf("precondition: elf still sick at the start of turn 2 (SummonSick=%v)", o.SummonSick)
	}
	if e.Pending().Player != 0 || e.Pending().Kind != decision.KPriority {
		t.Fatalf("precondition: pending decision = %+v, want seat-0 priority", e.Pending())
	}
	sawTap := false
	for _, option := range e.Pending().Options {
		if option.Kind == "activate" && option.Obj == elf {
			sawTap = true
		}
	}
	if !sawTap {
		t.Fatalf("elf's {T} mana ability not offered in seat 0's turn-2 window (CR 302.6): %+v", e.Pending().Options)
	}
}

// TestSickCreatureTapAbilityOfferedOnTheOpponentsTurn is the non-mana half of
// the same scope rule: a creature with a plain {T} activated ability (the
// Royal Assassin shape) must be offered it in seat 0's priority window on the
// opponent's turn once that turn has begun, not one turn later. It also
// proves the offer is real by resolving it and destroying the target.
func TestSickCreatureTapAbilityOfferedOnTheOpponentsTurn(t *testing.T) {
	t.Parallel()
	assassinSrc := "Name:Real Assassin\nManaCost:1 B B\nTypes:Creature Human Assassin\nPT:1/1\nA:AB$ Destroy | Cost$ T | TargetType$ Creature | ValidTgts$ Creature.nonBlack | SpellDescription$ Destroy target creature.\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 4242, "Name:Fodder\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// Real board state: the assassin entered this turn (onBoard stamps
	// SummonSick, matching a real entry), plus an untapped land to prove no
	// mana is involved and an opponent creature as the destroy target.
	assassin := onBoard(t, e, 0, assassinSrc)
	opponent := onBoard(t, e, 1, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if !e.G.Obj(assassin).SummonSick {
		t.Fatal("precondition: assassin placed this turn must be summoning sick")
	}

	// Drive into the opponent's turn 2 and find seat 0's own priority window.
	passUntilTurn(t, e, 2)
	if e.G.Active != 1 {
		t.Fatalf("precondition: turn 2 active seat = %d, want opponent 1", e.G.Active)
	}
	advanceToSeat0PriorityOnTurn(t, e, 2, 400)

	o := e.G.Obj(assassin)
	if o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: assassin zone=%s controller=%d, want battlefield under seat 0", o.Zone, o.Controller)
	}
	if o.Tapped {
		t.Fatal("precondition: assassin must be untapped in the turn-2 window")
	}
	if o.SummonSick {
		t.Fatalf("precondition: assassin still sick at the start of turn 2 (SummonSick=%v)", o.SummonSick)
	}
	if e.G.Obj(opponent).Zone != state.ZBattlefield {
		t.Fatal("precondition: destroy target must still be on the battlefield")
	}

	idx := -1
	for _, option := range e.Pending().Options {
		if option.Kind == "ability" && option.Obj == assassin {
			idx = option.Index
		}
	}
	if idx < 0 {
		t.Fatalf("assassin's {T} destroy ability not offered in seat 0's turn-2 window (CR 302.6): %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	// The target ask is the next decision; choose the opponent's creature.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after activate, decision = %+v, want a target ask", d)
	}
	targetIdx := -1
	for _, option := range d.Options {
		if option.Obj == opponent {
			targetIdx = option.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("opponent's creature not a legal destroy target: %+v", d.Options)
	}
	submitChoices(t, e, targetIdx)
	resolveStack(t, e)
	if e.G.Obj(opponent).Zone == state.ZBattlefield {
		t.Fatalf("destroy did not remove the target: zone=%s", e.G.Obj(opponent).Zone)
	}
}
