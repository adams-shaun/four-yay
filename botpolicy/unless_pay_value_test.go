package botpolicy

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// manaUnlessDecision builds a payable unless_pay KModes election: option 0 is
// Pay, option 1 Don't pay, with a plain-mana UnlessCost the policy can value.
func manaUnlessDecision(cost string) *decision.Decision {
	return &decision.Decision{Seq: 11, Player: 0, Kind: decision.KModes, Min: 1, Max: 1,
		ResumeKind: "unless_pay",
		ResumeSA:   &cards.SA{Kind: "SP", API: "Counter", Params: map[string]string{"UnlessCost": cost}},
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Label: "Pay 3 — don't counter", Mode: decision.ModeUnlessPay},
			{Index: 1, Kind: "mode", Label: "Don't pay", Mode: decision.ModeUnlessDecline},
		}}
}

// TestUnlessPayPolicyDeclinesWhenPayingIsWorse pins the value-aware half of
// the unless-pay bot policy: a PAYABLE mana vote can be declined when the
// payer faces lethal board damage and the tax would spend its entire floating
// pool — the losing race. Before the fix the generic KModes arm chose option
// 0 (Pay) unconditionally.
func TestUnlessPayPolicyDeclinesWhenPayingIsWorse(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	d := manaUnlessDecision("3")
	b := Board{
		Life:      TableOf(map[state.PlayerID]int32{0: 4}),
		Pool:      state.Mana{state.MG: 1},
		Creatures: TableOf(map[state.ObjID]Creature{1: {Power: 5, Toughness: 5, Controller: 1}}),
	}
	// Real assertion precondition: the decision really is a payable 2-option
	// mana election with decline available.
	if len(d.Options) != 2 || d.Options[0].Kind != "mode" {
		t.Fatalf("ask precondition failed: %+v", d.Options)
	}
	if !b.facingLethal(0) {
		t.Fatal("board precondition failed: expected lethal threat, got none")
	}
	in := Decide(b, d, r)
	if err := d.Validate(in); err != nil {
		t.Fatalf("decline answer is illegal: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("bot should decline a tax while facing lethal: %+v", in)
	}
}

// TestUnlessPayPolicyPaysWhenSafe is the positive control: with no lethal
// threat the same payable election is answered Pay, so the arm is value-aware
// rather than a blanket decline.
func TestUnlessPayPolicyPaysWhenSafe(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	d := manaUnlessDecision("3")
	b := Board{
		Life:      TableOf(map[state.PlayerID]int32{0: 20}),
		Pool:      state.Mana{state.MG: 1},
		Creatures: TableOf(map[state.ObjID]Creature{1: {Power: 5, Toughness: 5, Controller: 1}}),
	}
	if b.facingLethal(0) {
		t.Fatal("board precondition failed: unexpected lethal threat")
	}
	in := Decide(b, d, r)
	if err := d.Validate(in); err != nil {
		t.Fatalf("pay answer is illegal: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("bot should pay a safe unless tax: %+v", in)
	}
}
