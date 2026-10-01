package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// fakeLookup builds a card-name lookup over hand-built IR.
func fakeLookup(cs ...*cards.Card) func(string) *cards.Card {
	m := map[string]*cards.Card{}
	for _, c := range cs {
		if len(c.Faces) > 0 && c.Faces[0] != nil {
			m[c.Faces[0].Name] = c
		}
	}
	return func(n string) *cards.Card { return m[n] }
}

func burnCard(name string, dmg string) *cards.Card {
	return &cards.Card{Faces: []*cards.Face{{Name: name, Abilities: []*cards.SA{
		{Kind: "SP", API: "DealDamage", Params: map[string]string{"NumDmg": dmg, "ValidTgts": "Any"}},
	}}}}
}

func pumpCard(name, att string) *cards.Card {
	return &cards.Card{Faces: []*cards.Face{{Name: name, Abilities: []*cards.SA{
		{Kind: "SP", API: "Pump", Params: map[string]string{"NumAtt": att, "NumDef": "+0", "ValidTgts": "Creature.YouCtrl"}},
	}}}}
}

func attackerDecision(player state.PlayerID, objs ...state.ObjID) decision.Decision {
	d := decision.Decision{Seq: 5, Player: player, Kind: decision.KAttackers, Min: 0, Max: len(objs)}
	for i, o := range objs {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "attacker", Obj: o, Player: 1})
	}
	return d
}

func priorityDecision(player state.PlayerID) decision.Decision {
	return decision.Decision{Seq: 8, Player: player, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "pass"},
			{Index: 1, Kind: "cast", Obj: 7, Label: "Cast Bolt"},
		}}
}

// TestLethalTakesTheAttackKill: with a 3/3 and an opponent at 3 life behind
// no blockers, the guaranteed attack is lethal, so the decorator attacks with
// it instead of taking the inner's pass.
func TestLethalTakesTheAttackKill(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{10: {Power: 3, Toughness: 3, Controller: 0}}),
		Life:      botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 3}),
		Cards:     botpolicy.TableOf(map[state.ObjID]botpolicy.Card{10: {Sick: false}}),
	}
	// Preconditions the assertion depends on.
	if got := board.Creatures.Get(10).Power; got != 3 {
		t.Fatalf("fixture: attacker power %d, want 3", got)
	}
	if got := board.Life.Get(1); got != 3 {
		t.Fatalf("fixture: opponent life %d, want 3", got)
	}
	d := attackerDecision(0, 10)
	d.Options[0].Index = 0
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	wrapped := newLethal(inner, 1).(seat.BoardSeat)
	got, err := wrapped.DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.boarded)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the attacking option [0]", got.Choices)
	}
}

// TestLethalAttackDelegatesWithoutEnoughDamage: one life above the
// guaranteed damage must delegate, proving the take above is not vacuous.
func TestLethalAttackDelegatesWithoutEnoughDamage(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{10: {Power: 3, Toughness: 3, Controller: 0}}),
		Life:      botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 4}),
		Cards:     botpolicy.TableOf(map[state.ObjID]botpolicy.Card{10: {Sick: false}}),
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want the inner's empty pass", got.Choices)
	}
}

// TestLethalWorstCaseBlockingDeniesTheKill: a ground attacker that an
// untapped blocker can answer deals nothing under the worst-case assignment,
// so the decorator delegates. This pins the conservative blocking model --
// without it the previous two tests could both pass on a naive "sum the
// power" bound.
func TestLethalWorstCaseBlockingDeniesTheKill(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{
			10: {Power: 3, Toughness: 3, Controller: 0},
			20: {Power: 2, Toughness: 2, Controller: 1}, // untapped blocker
		}),
		Life:  botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 3}),
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{10: {Sick: false}}),
	}
	if board.Creatures.Get(20).Tapped {
		t.Fatal("fixture: blocker must be untapped")
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (a blocked ground attacker deals nothing)", got.Choices)
	}
}

// TestLethalFlierIgnoresAGroundBlocker: the same board, but the attacker has
// flying and the blocker does not, so the kill is guaranteed and taken --
// pinning that the worst-case model is about reach, not blanket denial.
func TestLethalFlierIgnoresAGroundBlocker(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{
			10: {Power: 3, Toughness: 3, Controller: 0, Keywords: []string{"Flying"}},
			20: {Power: 2, Toughness: 2, Controller: 1},
		}),
		Life:  botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 3}),
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{10: {Sick: false}}),
	}
	d := attackerDecision(0, 10)
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the flying attacker [0]", got.Choices)
	}
}

// TestLethalBurnTakesTheKillAndTargetsTheOpponent: a hand burn that reaches
// lethal on its own is cast, and the following target ask is aimed at the
// opponent (View path, so the card name resolves from the projected hand).
func TestLethalBurnTakesTheKillAndTargetsTheOpponent(t *testing.T) {
	SetCardLookup(fakeLookup(burnCard("Bolt", "3")))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Hand: []view.CardView{{ID: 7, Name: "Bolt"}}},
			{ID: 1, Life: 3},
		},
	}
	if len(v.Players[0].Hand) != 1 || v.Players[1].Life != 3 {
		t.Fatal("fixture: hand and opponent life not as intended")
	}
	d := priorityDecision(0)
	inner := &stubPlain{answer: passIntent}
	wrapped := newLethal(inner, 1)
	got, err := wrapped.Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("priority choices = %v, want the burn cast [1]", got.Choices)
	}
	// The following target ask must name the opponent, not a creature.
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
		t.Fatalf("target choices = %v, want the opponent player [1]", got2.Choices)
	}
}

// TestLethalBurnDelegatesWhenItFallsShort: one life above the burn delegates.
func TestLethalBurnDelegatesWhenItFallsShort(t *testing.T) {
	SetCardLookup(fakeLookup(burnCard("Bolt", "3")))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Hand: []view.CardView{{ID: 7, Name: "Bolt"}}},
			{ID: 1, Life: 4},
		},
	}
	inner := &stubPlain{answer: passIntent}
	got, err := newLethal(inner, 1).Decide(context.Background(), v, priorityDecision(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the inner's pass [0]", got.Choices)
	}
}

// TestLethalPumpEnablesTheAttack: a +3 pump on a 3/3 attacker lifts a
// guaranteed 3 to 6 against a 6-life opponent, so the pump is cast and aimed
// at the attacker.
func TestLethalPumpEnablesTheAttack(t *testing.T) {
	SetCardLookup(fakeLookup(pumpCard("Grow", "+3")))
	v := view.View{
		Viewer: 0, Active: 0, Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20,
				Battlefield: []view.CardView{{ID: 10, Name: "Bear", Types: "Creature Bear", Power: 3, Toughness: 3, Controller: 0}},
				Hand:        []view.CardView{{ID: 7, Name: "Grow"}}},
			{ID: 1, Life: 6},
		},
	}
	d := decision.Decision{Seq: 8, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass"}, {Index: 1, Kind: "cast", Obj: 7, Label: "Cast Grow"}}}
	inner := &stubPlain{answer: passIntent}
	wrapped := newLethal(inner, 1)
	got, err := wrapped.Decide(context.Background(), v, d)
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

// TestLethalDelegatesEverythingElse pins the contract's other half: a
// decision the idea does not name goes through untouched and the inner is
// consulted exactly once.
func TestLethalDelegatesEverythingElse(t *testing.T) {
	SetCardLookup(fakeLookup(burnCard("Bolt", "3")))
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{10: {Power: 3, Toughness: 3, Controller: 0}}),
		Life:      botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 20}),
		Cards:     botpolicy.TableOf(map[state.ObjID]botpolicy.Card{10: {Sick: false}}),
	}
	cases := []struct {
		name string
		d    decision.Decision
		in   decision.Intent
	}{
		{"blockers decision", decision.Decision{Seq: 5, Player: 0, Kind: decision.KBlockers,
			Options: []decision.Option{{Index: 0, Kind: "block", Obj: 10, Attacker: 20}}},
			decision.Intent{Seq: 5, Player: 0, Choices: []int{0}}},
		{"target with no pending line", decision.Decision{Seq: 6, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
			Options: []decision.Option{{Index: 0, Kind: "player", Player: 1}}},
			decision.Intent{Seq: 6, Player: 0, Choices: []int{0}}},
		{"attack with non-lethal damage", attackerDecision(0, 10),
			decision.Intent{Seq: 5, Player: 0, Choices: []int{}}},
		{"priority with no lethal", priorityDecision(0),
			decision.Intent{Seq: 8, Player: 0, Choices: []int{0}}},
	}
	for _, tc := range cases {
		inner := &stubSeat{answer: tc.in}
		wrapped := newLethal(inner, 1).(seat.BoardSeat)
		got, err := wrapped.DecideBoard(context.Background(), board, tc.d)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if inner.boarded != 1 {
			t.Fatalf("%s: inner consulted %d times, want 1", tc.name, inner.boarded)
		}
		if len(got.Choices) != len(tc.in.Choices) || (len(tc.in.Choices) > 0 && got.Choices[0] != tc.in.Choices[0]) {
			t.Fatalf("%s: choices = %v, want the inner's %v (unchanged)", tc.name, got.Choices, tc.in.Choices)
		}
	}
}

// TestLethalPassesErrorsThrough: the wrapper never answers for a failing
// inner.
func TestLethalPassesErrorsThrough(t *testing.T) {
	inner := &stubSeat{err: errors.New("refused")}
	wrapped := newLethal(inner, 1).(seat.BoardSeat)
	_, err := wrapped.DecideBoard(context.Background(), botpolicy.Board{}, attackerDecision(0, 10))
	if err == nil || err.Error() != "refused" {
		t.Fatalf("err = %v, want the inner's refusal", err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.boarded)
	}
}

// TestLethalKeepsThePlainSurface: a non-BoardSeat inner is not wrapped into
// a BoardSeat.
func TestLethalKeepsThePlainSurface(t *testing.T) {
	plain := &stubPlain{answer: passIntent}
	wrapped := newLethal(plain, 1)
	if _, isBoard := wrapped.(seat.BoardSeat); isBoard {
		t.Fatal("wrapper over a non-BoardSeat inner implements seat.BoardSeat")
	}
	if _, ok := wrapped.(Unwrapper); !ok {
		t.Fatal("lethal does not implement Unwrapper")
	}
}

// TestLethalWantsPaymentActions: the payment opt-in is the inner's.
func TestLethalWantsPaymentActions(t *testing.T) {
	with := &stubSeat{wantsPayment: true}
	without := &stubSeat{}
	if !newLethal(with, 1).(seat.PaymentPlanConsumer).WantsPaymentActions() {
		t.Fatal("wrapper did not pass the inner's payment opt-in through")
	}
	if newLethal(without, 1).(seat.PaymentPlanConsumer).WantsPaymentActions() {
		t.Fatal("wrapper opted into payment plans for a non-consumer inner")
	}
}

// TestLethalResolvesThroughTheRegistry: `sb-first+lethal` builds a decorated
// seat through Build, and the undecorated `sb-first` stays the bare builtin.
func TestLethalResolvesThroughTheRegistry(t *testing.T) {
	bare, err := Build("sb-first", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.(*builtins.Seat); !ok {
		t.Fatalf("sb-first built %T, want *builtins.Seat (precondition)", bare)
	}
	wrapped, err := Build("sb-first+lethal", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wrapped.(*builtins.Seat); ok {
		t.Fatal("sb-first+lethal built the bare base seat")
	}
	if _, ok := wrapped.(Unwrapper); !ok {
		t.Fatal("sb-first+lethal is not a decorated seat")
	}
}
