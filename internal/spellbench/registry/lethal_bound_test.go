package registry

// Tests for the damage-bound fixes found in the sb-idea-lethal review: the
// pump gain must be a COMBINED delta (blockers are shared between
// attackers), first strike on blockers must deny a non-striking trampler,
// and a blocked double-striker must be priced as a single step. Each test
// asserts the fixture precondition it depends on, so a vacuous setup fails
// loudly.

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// sharedBlockersView is the probe board that broke the isolated pump delta:
// our 3/3 trampler (obj 10) and 5/5 vanilla (obj 30) against untapped 2/2
// and 1/1 blockers, a +2 pump in hand. Unpumped the attack guarantees 2 (the
// vanilla eats the 2/2 for nothing, the trampler tramples the 1/1 for 2).
// Pumping the trampler to 5 moves it ahead in power order, it now eats the
// 2/2 for 3, and the vanilla falls to the 1/1 for nothing: the combined line
// delivers 3, not 4.
func sharedBlockersView(life int32) view.View {
	return view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20,
				Battlefield: []view.CardView{
					{ID: 10, Name: "Bear", Types: "Creature Bear", Power: 3, Toughness: 3, Controller: 0, Keywords: []string{"Trample"}},
					{ID: 30, Name: "Ox", Types: "Creature Ox", Power: 5, Toughness: 5, Controller: 0},
				},
				Hand: []view.CardView{{ID: 7, Name: "Grow"}}},
			{ID: 1, Life: life,
				Battlefield: []view.CardView{
					{ID: 20, Name: "Wall", Types: "Creature Wall", Power: 2, Toughness: 2, Controller: 1},
					{ID: 21, Name: "Peasant", Types: "Creature Peasant", Power: 1, Toughness: 1, Controller: 1},
				}},
		},
	}
}

func pumpPriority(player state.PlayerID, seq uint64) decision.Decision {
	return decision.Decision{Seq: seq, Player: player, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "pass"},
			{Index: 1, Kind: "cast", Obj: 7, Label: "Cast Grow"},
		}}
}

// TestLethalPumpGainIsCombinedNotIsolated: the +2 pump on the trampler shows
// an isolated gain of 2 (projected 2 + 2 = 4 reaches a 4-life opponent) but
// the combined line delivers only 3, so the decorator must delegate -- a
// pump cast here would spend a spell on a kill the opponent prevents.
func TestLethalPumpGainIsCombinedNotIsolated(t *testing.T) {
	SetCardLookup(fakeLookup(pumpCard("Grow", "+2")))
	v := sharedBlockersView(4)
	if v.Players[1].Life != 4 {
		t.Fatal("fixture: opponent life not 4")
	}
	if len(v.Players[1].Battlefield) != 2 || v.Players[1].Battlefield[0].Tapped {
		t.Fatal("fixture: two untapped blockers required")
	}
	inner := &stubPlain{answer: passIntent}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, pumpPriority(0, 8))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the inner's pass [0] (combined gain 1, projected+pump = 3 < 4)", got.Choices)
	}
}

// TestLethalCombinedPumpStillTakesTheKill: the same board against a 3-life
// opponent, where the combined line (2 + 1 = 3) does reach lethal, is taken.
// This is the positive control for the test above.
func TestLethalCombinedPumpStillTakesTheKill(t *testing.T) {
	SetCardLookup(fakeLookup(pumpCard("Grow", "+2")))
	v := sharedBlockersView(3)
	if v.Players[1].Life != 3 {
		t.Fatal("fixture: opponent life not 3")
	}
	inner := &stubPlain{answer: passIntent}
	wrapped := newLethal(inner, 1)
	got, err := wrapped.Decide(context.Background(), v, pumpPriority(0, 8))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("priority choices = %v, want the pump cast [1]", got.Choices)
	}
	d2 := decision.Decision{Seq: 9, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: 11, Player: 0},
			{Index: 1, Kind: "permanent", Obj: 10, Player: 0},
		}}
	got2, err := wrapped.Decide(context.Background(), v, d2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Choices) != 1 || got2.Choices[0] != 1 {
		t.Fatalf("target choices = %v, want the pumped attacker [1]", got2.Choices)
	}
}

// fsTramplerBoard is a 4/3 attacker (obj 10) against an untapped 3/3
// first-strike blocker (obj 20), opponent at 1 life. The attacker's
// keywords are the caller's.
func fsTramplerBoard(atkKeywords []string) botpolicy.Board {
	return botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 4, Toughness: 3, Controller: 0, Keywords: atkKeywords},
			20: {Power: 3, Toughness: 3, Controller: 1, Keywords: []string{"First Strike"}},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 1},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}},
	}
}

// TestLethalFirstStrikeBlockerDeniesTheTrampler: the blocker strikes first
// with power 3 at a toughness-3 attacker, killing it before the regular
// damage step, so the trampler deals 0 -- not the 1 the trample excess
// without first strike claims. The decorator must delegate, not commit the
// suicide attack.
func TestLethalFirstStrikeBlockerDeniesTheTrampler(t *testing.T) {
	board := fsTramplerBoard([]string{"Trample"})
	if board.Creatures[20].Keywords[0] != "First Strike" {
		t.Fatal("fixture: blocker must have first strike")
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (the first-striking blocker kills the attacker before damage)", got.Choices)
	}
}

// TestLethalFirstStrikeAttackerPunchesThrough: the same board, but the
// attacker strikes first too, so it deals its damage in the first-strike
// step and the kill is taken.
func TestLethalFirstStrikeAttackerPunchesThrough(t *testing.T) {
	board := fsTramplerBoard([]string{"Trample", "First Strike"})
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the attacking option [0]", got.Choices)
	}
}

// TestLethalBlockedDoubleStrikerIsPricedSingleStep: a 5/5 double-striking
// trampler blocked by a 5/5 first-striking blocker. The old model claimed
// 2*5 - 5 = 5 (a kill at 5 life); in reality the blocker's first strike
// kills the attacker after the first damage step, so the line delivers 0.
// The decorator must delegate.
func TestLethalBlockedDoubleStrikerIsPricedSingleStep(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 5, Toughness: 5, Controller: 0, Keywords: []string{"Double Strike", "Trample"}},
			20: {Power: 5, Toughness: 5, Controller: 1, Keywords: []string{"First Strike"}},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 5},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}},
	}
	if board.Life[1] != 5 {
		t.Fatal("fixture: opponent life not 5")
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (the first-striking blocker stops the second damage step)", got.Choices)
	}
}

// TestLethalUnblockedDoubleStrikerStillCountsBothSteps: the same attacker
// with no blocker that can reach it deals 2*p, so a 6-life kill is taken --
// pinning that the single-step pricing above applies only when blocked.
func TestLethalUnblockedDoubleStrikerStillCountsBothSteps(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 3, Toughness: 3, Controller: 0, Keywords: []string{"Double Strike", "Flying"}},
			20: {Power: 2, Toughness: 2, Controller: 1},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 6},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}},
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the attacking option [0] (unblocked double strike = 6)", got.Choices)
	}
}

// TestLethalPendingTargetIgnoresAStaleSeq: a target decision whose Seq is
// not after the priority that armed the line must not be hijacked -- the
// inner's answer goes through, and only a later-posed target ask is aimed.
func TestLethalPendingTargetIgnoresAStaleSeq(t *testing.T) {
	SetCardLookup(fakeLookup(burnCard("Bolt", "3")))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Hand: []view.CardView{{ID: 7, Name: "Bolt"}}},
			{ID: 1, Life: 3},
		},
	}
	if v.Players[1].Life != 3 {
		t.Fatal("fixture: opponent life not 3")
	}
	// The inner always answers a target ask with the permanent [0], so the
	// decorator's aim at the opponent [1] is distinguishable from delegation.
	inner := &stubPlain{answer: decision.Intent{Seq: 10, Player: 0, Choices: []int{0}}}
	wrapped := newLethal(inner, 1)
	// Arm the line at Seq 10.
	got, err := wrapped.Decide(context.Background(), v, pumpPriority(0, 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("priority choices = %v, want the burn cast [1] (precondition: line armed)", got.Choices)
	}
	// A STALE target ask (Seq equal to the arming priority) is delegated: the
	// inner's own choice [0] goes through.
	stale := decision.Decision{Seq: 10, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: 11, Player: 0},
			{Index: 1, Kind: "player", Player: 1},
		}}
	got, err = wrapped.Decide(context.Background(), v, stale)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("stale target choices = %v, want the inner's [0] (delegated, not hijacked)", got.Choices)
	}
	// Re-arm at Seq 11 (the stale ask consumed the old pending), then a
	// LATER-posed target ask is aimed at the opponent as before.
	got, err = wrapped.Decide(context.Background(), v, pumpPriority(0, 11))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("re-arm priority choices = %v, want the burn cast [1]", got.Choices)
	}
	fresh := decision.Decision{Seq: 12, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: 11, Player: 0},
			{Index: 1, Kind: "player", Player: 1},
		}}
	got, err = wrapped.Decide(context.Background(), v, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("fresh target choices = %v, want the opponent [1]", got.Choices)
	}
}
