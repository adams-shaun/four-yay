package seat

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// forestView is one basic Forest as the view projects it: a basic land with a
// single fixed {G} production summary. summoning sickness is supplied by the
// caller exactly as the engine's events.Apply sets it on every permanent that
// enters the battlefield.
func forestView(id state.ObjID, sick bool) view.CardView {
	p := cards.ManaProduction{}
	p.Colour[state.MG] = 1
	return view.CardView{
		ID:         id,
		Name:       "Forest",
		Types:      "Basic Land Forest",
		Produces:   &p,
		SummonSick: sick,
		Controller: 0,
		Owner:      0,
	}
}

// TestBotReserveCountsJustPlayedLand is the CR 302.6 regression over the view
// adapter: the engine marks EVERY permanent summoning-sick on entry
// (events/apply.go sets o.SummonSick = true), but the tap-legality rule only
// refuses the tap cost when the derived types contain Creature and the
// permanent has no Haste (rules/legal.go). A basic land is never a creature,
// so a Forest played this turn is fully tappable for mana and MUST count
// toward the bot's coloured-instant reserve.
//
// The board is the claim-A shape with the reserve preserved: four live basic
// Forests (one of them just played, so the View carries SummonSick true), a
// {G}{G} instant the seat is holding, and a one-mana {G} burn pending on the
// stack. Four live sources survive both the pending payment and the
// single-unit probe, so the value kill (the 2/2 the three-damage burn can
// actually kill) must be promoted over the unkillable but board-first 5/5.
//
// This is the end-to-end guard for the wrong model a prior revision of the
// pending-payment fix shipped: treating a sick BASIC source as dead mana made
// the adapter hand the policy three live Forests instead of four and silently
// declined the value kill on the extremely common "play a land, then burn"
// turn. The precondition assertions below pin that the fresh source reaches
// the policy as a live basic Forest and that the two creature values differ,
// so a vacuous setup fails loudly rather than passing.
func TestBotReserveCountsJustPlayedLand(t *testing.T) {
	green := cards.ManaProduction{}
	green.Colour[state.MG] = 1
	reserve := "G G"
	burn := "G"

	v := view.View{
		Viewer: 0,
		Players: []view.PlayerView{
			{ID: 0, Life: 20, Battlefield: []view.CardView{
				forestView(1, false),
				forestView(2, false),
				forestView(3, false),
				forestView(4, true), // played this turn: the engine's SummonSick flag
			}, Hand: []view.CardView{
				{ID: 9, Name: "Reserve", Types: "Instant", ManaCost: reserve, Controller: 0, Owner: 0},
			}},
			{ID: 1, Life: 20, Battlefield: []view.CardView{
				{ID: 201, Name: "Bear", Types: "Creature", Power: 2, Toughness: 2, Controller: 1, Owner: 1},
				{ID: 202, Name: "Wurm", Types: "Creature", Power: 5, Toughness: 5, Controller: 1, Owner: 1},
			}},
		},
		Stack: []view.StackView{
			{ID: 50, Kind: "spell", Controller: 0, Card: &view.CardView{ID: 50, ManaCost: burn, Types: "Sorcery"}},
		},
	}

	// Precondition 1: the just-played source really carries SummonSick in the
	// View (the fact the engine sets on entry), and the three older Forests do
	// not -- otherwise the test would not exercise the distinction at all.
	if !v.Players[0].Battlefield[3].SummonSick {
		t.Fatal("precondition: the fourth Forest must carry SummonSick in the View")
	}
	for i := 0; i < 3; i++ {
		if v.Players[0].Battlefield[i].SummonSick {
			t.Fatalf("precondition: Forest %d must not be sick", v.Players[0].Battlefield[i].ID)
		}
	}

	// Precondition 2: the adapter hands the policy four live basic green
	// sources and a {G}{G} instant reserve in hand, with the pending burn on
	// the stack whose cost the policy will price.
	b := BoardFromView(v)
	live := 0
	for id := state.ObjID(1); id <= 4; id++ {
		c := b.Cards.Get(id)
		if c.OnBattlefield && c.Basic && !c.Tapped && c.Produces.Colour[state.MG] == 1 {
			live++
		}
	}
	if live != 4 {
		t.Fatalf("adapter produced %d live basic Forests, want 4 (a just-played land is tappable)", live)
	}
	if r := b.Cards.Get(9); !r.Castable || !r.InstantSpeed || r.ManaCost != reserve {
		t.Fatalf("hand reserve 9 is not a castable {G}{G} instant: %+v", r)
	}
	if len(b.Stack) != 1 || b.Stack[0].ID != 50 || !b.Stack[0].IsSpell || b.Stack[0].ManaCost != burn {
		t.Fatalf("pending stack spell is not a {G} spell at id 50: %+v", b.Stack)
	}
	if b.Creatures.Get(201).Toughness == b.Creatures.Get(202).Toughness {
		t.Fatal("precondition: the two creature values under comparison must differ")
	}

	// The value kill: with four live sources the reserve survives the pending
	// {G} payment and the probe, so tier 3 fires and the burn is spent on the
	// killable 2/2 (201), not the unkillable 5/5 (202).
	d := decision.Decision{
		Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1, Source: 50,
		TargetEffect: &decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{Amount: reserveIntp(3)}},
		Options: []decision.Option{
			{Index: 0, Kind: "player", Player: 1},
			{Index: 1, Kind: "permanent", Obj: 201, Player: 1},
			{Index: 2, Kind: "permanent", Obj: 202, Player: 1},
		},
	}
	in, err := NewBot(1).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("chose option %v (obj %v), want option 1 (the killable 2/2, obj 201): "+
			"a just-played basic land is tappable and must not be dropped from the reserve",
			in.Choices, decidedObjs(d, in.Choices))
	}
}

// decidedObjs renders chosen option indices as object ids for failure output.
func decidedObjs(d decision.Decision, ch []int) []state.ObjID {
	out := make([]state.ObjID, 0, len(ch))
	for _, i := range ch {
		if i >= 0 && i < len(d.Options) {
			out = append(out, d.Options[i].Obj)
		}
	}
	return out
}

// reserveIntp returns a pointer to n, for the DamageEffect.Amount field.
func reserveIntp(n int) *int { return &n }
