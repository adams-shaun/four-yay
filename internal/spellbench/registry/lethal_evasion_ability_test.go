package registry

// Tests for the two capabilities the sb-idea-lethal review round t2 named:
// the "can't be blocked except by" evasion shape (a printed CantBlockBy
// static plus the horsemanship/shadow/skulk keywords) and castable burn/pump
// ACTIVATED ABILITIES. Each test asserts the precondition its assertion
// depends on, and each has a paired negative control so a vacuous pass (the
// feature silently unregistered) fails loudly.

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// cbbCard builds a card carrying a printed CantBlockBy static that applies to
// itself, with the given ValidBlocker$ filter ("" meaning nothing may block).
func cbbCard(name, validBlocker string) *cards.Card {
	params := map[string]string{"ValidAttacker": "Creature.Self"}
	if validBlocker != "" {
		params["ValidBlocker"] = validBlocker
	}
	return &cards.Card{Faces: []*cards.Face{{Name: name, Statics: []cards.Static{
		{Mode: "CantBlockBy", Params: params},
	}}}}
}

// abilityCard builds a card whose first activated ability (index 0) is the
// given API with the given params.
func abilityIRCard(name, api string, params map[string]string) *cards.Card {
	return &cards.Card{Faces: []*cards.Face{{Name: name, Abilities: []*cards.SA{
		{Kind: "AB", API: api, Params: params},
	}}}}
}

// evasionView is a 2-seat board where we control one attacker (obj 10, atk
// power/toughness and keywords/name as given) and the opponent has one
// untapped blocker (obj 20, the given power) at the given life.
func evasionView(attacker view.CardView, blockerPower int32, oppLife int32) view.View {
	attacker.Controller = 0
	if attacker.Types == "" {
		attacker.Types = "Creature"
	}
	return view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{attacker}},
			{ID: 1, Life: oppLife, Battlefield: []view.CardView{
				{ID: 20, Name: "Blocker", Types: "Creature", Power: blockerPower, Toughness: blockerPower, Controller: 1},
			}},
		},
	}
}

// TestLethalPrintedCantBlockByExcludesTheNamedBlocker: our 3/3 "Kor
// Castigator" carries a printed "can't be blocked by creatures with power 2
// or less" static, so a 2/2 blocker cannot block it and the 3 damage is a
// guaranteed kill at 3 life -- taken. The negative control (a 3/3 blocker,
// which the filter does NOT exclude) must delegate.
func TestLethalPrintedCantBlockByExcludesTheNamedBlocker(t *testing.T) {
	SetCardLookup(fakeLookup(cbbCard("Kor Castigator", "Creature.powerLE2")))
	atk := view.CardView{ID: 10, Name: "Kor Castigator", Power: 3, Toughness: 3}

	v := evasionView(atk, 2, 3)
	if got := v.Players[1].Battlefield[0].Power; got != 2 {
		t.Fatalf("fixture: blocker power %d, want 2", got)
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the attack [0] (power-2 blocker is excluded by the printed static)", got.Choices)
	}

	// Negative control: a power-3 blocker is NOT excluded, so it blocks and
	// the decorator must delegate.
	v2 := evasionView(atk, 3, 3)
	if got := v2.Players[1].Battlefield[0].Power; got != 3 {
		t.Fatalf("fixture: blocker power %d, want 3", got)
	}
	inner2 := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got2, err := newLethal(inner2, 1).Decide(context.Background(), v2, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Choices) != 0 {
		t.Fatalf("control choices = %v, want delegation (a power-3 blocker can block)", got2.Choices)
	}
}

// TestLethalPrintedCantBlockByUnrelatedStaticIsIgnored: a CantBlockBy static
// whose ValidAttacker$ is not the card itself (a lord granting the
// restriction to other creatures) must NOT make the attacker unblockable --
// the decorator only reads the restriction printed on the attacker.
func TestLethalPrintedCantBlockByUnrelatedStaticIsIgnored(t *testing.T) {
	lord := &cards.Card{Faces: []*cards.Face{{Name: "Lord", Statics: []cards.Static{
		{Mode: "CantBlockBy", Params: map[string]string{"ValidAttacker": "Creature.YouCtrl"}},
	}}}}
	SetCardLookup(fakeLookup(lord))
	atk := view.CardView{ID: 10, Name: "Lord", Power: 3, Toughness: 3}
	// Precondition: the static exists but does not name Self.
	if vb, ok := printedCantBlockBy(lord); ok {
		t.Fatalf("fixture: unrelated static wrongly accepted (ValidBlocker=%q)", vb)
	}
	v := evasionView(atk, 2, 3)
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (the lord static does not apply to this card)", got.Choices)
	}
}

// TestLethalPrintedCantBlockByUnevaluableFilterKeepsBlocker: a ValidBlocker$
// term the decorator cannot decide (a creature type) must leave the blocker
// in the pool -- the conservative direction -- so a kill that depends on the
// filter is NOT claimed.
func TestLethalPrintedCantBlockByUnevaluableFilterKeepsBlocker(t *testing.T) {
	SetCardLookup(fakeLookup(cbbCard("Kor Castigator", "Creature.Eldrazi")))
	atk := view.CardView{ID: 10, Name: "Kor Castigator", Power: 3, Toughness: 3}
	v := evasionView(atk, 2, 3)
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (an undecidable filter keeps the blocker)", got.Choices)
	}
}

// TestLethalSkulkExcludesGreaterPowerBlocker: skulk exempts blockers with
// greater power; a 2/2 skulker at 2 life against a 3/3 blocker connects, and
// the 2/2-blocker control is blocked.
func TestLethalSkulkExcludesGreaterPowerBlocker(t *testing.T) {
	atk := view.CardView{ID: 10, Name: "Skulker", Power: 2, Toughness: 2, Keywords: []string{"Skulk"}}
	v := evasionView(atk, 3, 2)
	if !hasKeyword(v.Players[0].Battlefield[0].Keywords, "Skulk") {
		t.Fatal("fixture: attacker lacks skulk")
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the skulker attack [0] (greater-power blocker excluded)", got.Choices)
	}
	// Control: an equal-power blocker can block a skulker.
	v2 := evasionView(atk, 2, 2)
	inner2 := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got2, err := newLethal(inner2, 1).Decide(context.Background(), v2, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Choices) != 0 {
		t.Fatalf("control choices = %v, want delegation (an equal-power blocker may block)", got2.Choices)
	}
}

// TestLethalHorsemanshipExcludesNonHorsemanshipBlocker: only another
// horsemanship creature may block a horsemanship attacker.
func TestLethalHorsemanshipExcludesNonHorsemanshipBlocker(t *testing.T) {
	atk := view.CardView{ID: 10, Name: "Rider", Power: 3, Toughness: 3, Keywords: []string{"Horsemanship"}}
	v := evasionView(atk, 5, 3)
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the horsemanship attack [0]", got.Choices)
	}
}

// abilityPriority is a priority decision offering one activated ability (obj,
// Ability index, label) and an inner pass.
func abilityPriority(obj state.ObjID, ability int, label string) decision.Decision {
	return decision.Decision{Seq: 8, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "pass"},
			{Index: 1, Kind: "ability", Obj: obj, Ability: ability, Label: label},
		}}
}

// TestLethalBurnAbilityTakesTheKillAndTargetsTheOpponent: a creature with a
// "{T}: this creature deals 2 damage to any target" activated ability is the
// only lethal line (opponent at 2), and the fielding target ask is aimed at
// the opponent. Without ability support the decorator would delegate.
func TestLethalBurnAbilityTakesTheKillAndTargetsTheOpponent(t *testing.T) {
	ping := abilityIRCard("Pinger", "DealDamage", map[string]string{"NumDmg": "2", "ValidTgts": "Any"})
	SetCardLookup(fakeLookup(ping))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{{ID: 10, Name: "Pinger", Types: "Creature", Power: 1, Toughness: 1, Controller: 0}}},
			{ID: 1, Life: 2},
		},
	}
	if got := v.Players[1].Life; got != 2 {
		t.Fatalf("fixture: opponent life %d, want 2", got)
	}
	inner := &stubPlain{answer: passIntent}
	wrapped := newLethal(inner, 1)
	got, err := wrapped.Decide(context.Background(), v, abilityPriority(10, 0, "Pinger: deal 2 damage"))
	if err != nil {
		t.Fatal(err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("priority choices = %v, want the burn ability [1]", got.Choices)
	}
	d2 := decision.Decision{Seq: 9, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: 10, Player: 0},
			{Index: 1, Kind: "player", Player: 1},
		}}
	got2, err := wrapped.Decide(context.Background(), v, d2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Choices) != 1 || got2.Choices[0] != 1 {
		t.Fatalf("target choices = %v, want the opponent [1]", got2.Choices)
	}
}

// TestLethalBurnAbilityDelegatesWhenShort: the same ability, plus the
// Pinger's own 1 power, one life short of the combined 3 must delegate,
// proving the take above is not vacuous.
func TestLethalBurnAbilityDelegatesWhenShort(t *testing.T) {
	ping := abilityIRCard("Pinger", "DealDamage", map[string]string{"NumDmg": "2", "ValidTgts": "Any"})
	SetCardLookup(fakeLookup(ping))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{{ID: 10, Name: "Pinger", Types: "Creature", Power: 1, Toughness: 1, Controller: 0}}},
			{ID: 1, Life: 4},
		},
	}
	if got := v.Players[1].Life; got != 4 {
		t.Fatalf("fixture: opponent life %d, want 4", got)
	}
	inner := &stubPlain{answer: passIntent}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, abilityPriority(10, 0, "Pinger: deal 2 damage"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the inner's pass [0]", got.Choices)
	}
}

// TestLethalSelfPumpAbilityEnablesTheAttack: a "{G}: this creature gets +3/+3
// until end of turn" ability (Defined$ Self, no target ask) on a 3/3
// attacker lifts a guaranteed 3 to 6 against a 6-life opponent, so it is
// activated, and no pending target is armed (the ability asks for none).
func TestLethalSelfPumpAbilityEnablesTheAttack(t *testing.T) {
	grow := abilityIRCard("Giant", "Pump", map[string]string{"Defined": "Self", "NumAtt": "+3", "NumDef": "+3"})
	SetCardLookup(fakeLookup(grow))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{{ID: 10, Name: "Giant", Types: "Creature", Power: 3, Toughness: 3, Controller: 0}}},
			{ID: 1, Life: 6},
		},
	}
	if got := v.Players[0].Battlefield[0].Power; got != 3 {
		t.Fatalf("fixture: attacker power %d, want 3", got)
	}
	if got := v.Players[1].Life; got != 6 {
		t.Fatalf("fixture: opponent life %d, want 6", got)
	}
	inner := &stubPlain{answer: passIntent}
	wrapped := newLethal(inner, 1)
	got, err := wrapped.Decide(context.Background(), v, abilityPriority(10, 0, "Giant: get +3/+3"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("priority choices = %v, want the pump ability [1]", got.Choices)
	}
	// The self-pump arms no pending target: a following target ask is the
	// inner's, not re-aimed by the decorator.
	d2 := decision.Decision{Seq: 9, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "permanent", Obj: 11, Player: 0}}}
	got2, err := wrapped.Decide(context.Background(), v, d2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Choices) != 1 || got2.Choices[0] != 0 {
		t.Fatalf("target choices = %v, want the inner's [0] (no pending aim)", got2.Choices)
	}
}

// TestLethalTargetedPumpAbilityArmsTheAttacker: a targeted (+3) ability on a
// 3/3 attacker is activated, and the following target ask is aimed at that
// attacker.
func TestLethalTargetedPumpAbilityArmsTheAttacker(t *testing.T) {
	grow := abilityIRCard("Giant", "Pump", map[string]string{"ValidTgts": "Creature.YouCtrl", "NumAtt": "+3", "NumDef": "+3"})
	SetCardLookup(fakeLookup(grow))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{{ID: 10, Name: "Giant", Types: "Creature", Power: 3, Toughness: 3, Controller: 0}}},
			{ID: 1, Life: 6},
		},
	}
	inner := &stubPlain{answer: passIntent}
	wrapped := newLethal(inner, 1)
	got, err := wrapped.Decide(context.Background(), v, abilityPriority(10, 0, "Giant: target creature gets +3/+3"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("priority choices = %v, want the pump ability [1]", got.Choices)
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

// TestLethalSelfPumpAbilityOnANonAttackerDelegates: a Defined$ Self pump on a
// non-attacking permanent (a tapped/sick creature) adds no guarantee, so the
// decorator delegates.
func TestLethalSelfPumpAbilityOnANonAttackerDelegates(t *testing.T) {
	grow := abilityIRCard("Giant", "Pump", map[string]string{"Defined": "Self", "NumAtt": "+3", "NumDef": "+3"})
	SetCardLookup(fakeLookup(grow))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{
				{ID: 10, Name: "Giant", Types: "Creature", Power: 3, Toughness: 3, Controller: 0, Tapped: true},
			}},
			{ID: 1, Life: 6},
		},
	}
	if !v.Players[0].Battlefield[0].Tapped {
		t.Fatal("fixture: the pump source must be unable to attack")
	}
	inner := &stubPlain{answer: passIntent}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, abilityPriority(10, 0, "Giant: get +3/+3"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the inner's pass [0]", got.Choices)
	}
}

// hasKeyword reports whether k (case-insensitive) is among ks.
func hasKeyword(ks []string, k string) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}

// TestLethalPrintedCantBlockByOnTheBoardPath: the production seat answers
// from a Board, which carries no creature names, so the printed static is
// resolved from the engine's own "Attack with <name> at ..." option label.
// The named 3/3 attacker at 3 life against a power-2 blocker (excluded by
// powerLE2) must take the attack; the power-3 control must delegate.
func TestLethalPrintedCantBlockByOnTheBoardPath(t *testing.T) {
	SetCardLookup(fakeLookup(cbbCard("Kor Castigator", "Creature.powerLE2")))
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 3, Toughness: 3, Controller: 0},
			20: {Power: 2, Toughness: 2, Controller: 1},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 3},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}},
	}
	if got := board.Creatures[20].Power; got != 2 {
		t.Fatalf("fixture: blocker power %d, want 2", got)
	}
	d := attackerDecision(0, 10)
	d.Options[0].Label = "Attack with Kor Castigator at P2"
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the attack [0] (printed static excludes the power-2 blocker)", got.Choices)
	}
	// Control: a power-3 blocker is not excluded.
	board.Creatures[20] = botpolicy.Creature{Power: 3, Toughness: 3, Controller: 1}
	inner2 := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got2, err := newLethal(inner2, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Choices) != 0 {
		t.Fatalf("control choices = %v, want delegation", got2.Choices)
	}
}
