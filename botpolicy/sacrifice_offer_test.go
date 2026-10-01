package botpolicy

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// devilOffer builds the KModes decision effSacrifice's damage-offer gate
// poses: ResumeKind unless_pay, ResumeSA the real Sacrifice API with an
// UnlessCost$ DamageYou<N>, option 0 "take N", option 1 "refuse".
func devilOffer(n int, obj state.ObjID) decision.Decision {
	return decision.Decision{
		Player: 1, Kind: decision.KModes, Min: 1, Max: 1,
		ResumeKind: "unless_pay",
		ResumeSA:   &cards.SA{API: "Sacrifice", Params: map[string]string{"UnlessCost": "DamageYou<" + strconv.Itoa(n) + ">"}},
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Label: "Take " + strconv.Itoa(n) + " damage", Obj: obj, Player: 1, Mode: decision.ModeUnlessPay},
			{Index: 1, Kind: "mode", Label: "Refuse — it stays", Obj: obj, Player: 1, Mode: decision.ModeUnlessDecline},
		},
	}
}

// TestBotAnswersVexingDevilDeliberately pins the botpolicy arm for the new
// Sacrifice damage offer (vexdev). The deciding opponent accepts when the
// offered creature is worth more alive than the life the damage costs and
// the damage is not lethal; declines when it is worthless or lethal; the
// ordinary KModes first-option arm still answers the mana unless-pay.
func TestBotAnswersVexingDevilDeliberately(t *testing.T) {
	devil := state.ObjID(42)
	// A 4/3 Devil offered for 4: worth 46 alive, 4 life is cheap — accept.
	b := Board{
		Cards: TableOf(map[state.ObjID]Card{devil: {Creature: true, Power: 4, CMC: 1}}),
		Life:  TableOf(map[state.PlayerID]int32{1: 20}),
	}
	d := devilOffer(4, devil)
	if got := Decide(b, &d, rng(1)).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("worthwhile creature: %v, want the take-damage option (index 0)", got)
	}
	// Lethal damage: decline even for a worthwhile creature.
	b.Life.Set(1, 3)
	if got := Decide(b, &d, rng(1)).Choices; len(got) != 1 || got[0] != 1 {
		t.Fatalf("lethal damage: %v, want the refuse option (index 1)", got)
	}
	// A worthless permanent for 4: decline.
	b.Life.Set(1, 20)
	chump := state.ObjID(43)
	b.Cards.Set(chump, Card{CMC: 0})
	d2 := devilOffer(4, chump)
	if got := Decide(b, &d2, rng(1)).Choices; len(got) != 1 || got[0] != 1 {
		t.Fatalf("worthless permanent: %v, want the refuse option", got)
	}
	// The mana unless-pay (no DamageYou$ cost) stays on the ordinary
	// first-option arm: the bot offers to pay.
	manaAsk := decision.Decision{
		Player: 0, Kind: decision.KModes, Min: 1, Max: 1,
		ResumeKind: "unless_pay",
		ResumeSA:   &cards.SA{API: "Sacrifice", Params: map[string]string{"UnlessCost": "1"}},
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Label: "Pay 1", Player: 0, Mode: decision.ModeUnlessPay},
			{Index: 1, Kind: "mode", Label: "Sacrifice it", Player: 0, Mode: decision.ModeUnlessDecline},
		},
	}
	if got := Decide(Board{}, &manaAsk, rng(1)).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("mana unless-pay: %v, want the pay option (the ordinary arm)", got)
	}
	// A non-Sacrifice unless-pay ask is untouched by the arm.
	counter := decision.Decision{
		Player: 0, Kind: decision.KModes, Min: 1, Max: 1,
		ResumeKind: "unless_pay",
		ResumeSA:   &cards.SA{API: "Counter", Params: map[string]string{"UnlessCost": "DamageYou<4>"}},
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Label: "Pay", Player: 0, Mode: decision.ModeUnlessPay},
			{Index: 1, Kind: "mode", Label: "Decline", Player: 0, Mode: decision.ModeUnlessDecline},
		},
	}
	if got := Decide(Board{}, &counter, rng(1)).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("counter unless-pay: %v, want the ordinary first-option arm", got)
	}
}
