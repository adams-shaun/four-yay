package botpolicy

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Hand-card fixtures for the land-count mulligan, shaped as the adapters fill
// a hand card (Castable, not on the battlefield). Lands print "no cost", the
// Forge script's cost for every land.
func plainsCard() Card {
	var p cards.ManaProduction
	p.Colour[0] = 1
	return Card{PrintedName: "Plains", Basic: true, ManaCost: "no cost", Castable: true, Produces: p}
}

func swampCard() Card {
	var p cards.ManaProduction
	p.Colour[2] = 1
	return Card{PrintedName: "Swamp", Basic: true, ManaCost: "no cost", Castable: true, Produces: p}
}

// wildsCard is a non-basic land with no recorded production (Evolving Wilds).
func wildsCard() Card {
	return Card{PrintedName: "Evolving Wilds", ManaCost: "no cost", Castable: true}
}

func spellCard(cmc int32, cost string) Card {
	return Card{PrintedName: "spell " + cost, Creature: true, Power: 2, CMC: cmc, ManaCost: cost, Castable: true}
}

// handBoard is a Board whose hand is cs, ObjIDs 1..len(cs) in order, with
// the Mulligan rule set.
func handBoard(rule MulliganRule, cs ...Card) Board {
	m := map[state.ObjID]Card{}
	for i, c := range cs {
		m[state.ObjID(i+1)] = c
	}
	return Board{Mulligan: rule, Cards: TableOf(m), HandSize: int32(len(cs))}
}

// sevenWith is a seven-card hand of `lands` Plains and 7-lands 2-drops.
func sevenWith(lands int) []Card {
	var cs []Card
	for i := 0; i < 7; i++ {
		if i < lands {
			cs = append(cs, plainsCard())
		} else {
			cs = append(cs, spellCard(2, "1 W"))
		}
	}
	return cs
}

// keepPrompt is rules/mulligan.go's keepMulliganPrompt wording for a
// two-player round (no free mulligan), copied verbatim.
func keepPrompt(bottom int, canMulligan bool) string {
	n := fmt.Sprintf("%d cards", bottom)
	if bottom == 1 {
		n = "1 card"
	}
	if canMulligan {
		return "p0 plays first. Keep your hand (put " + n + " on the bottom of your library) or take a mulligan?"
	}
	return "p0 plays first. Keep your hand (put " + n + " on the bottom of your library)"
}

func keepMullDecision(bottom int, canMulligan bool) decision.Decision {
	d := decision.Decision{Seq: 3, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Prompt: keepPrompt(bottom, canMulligan), Options: []decision.Option{{Index: 0, Kind: "keep", Label: "keep"}}}
	if canMulligan {
		d.Options = append(d.Options, decision.Option{Index: 1, Kind: "mulligan", Label: "mulligan"})
	}
	return d
}

func TestKeepBottomCount(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   int
	}{
		{keepPrompt(0, true), 0},
		{keepPrompt(1, true), 1},
		{keepPrompt(2, true), 2},
		{keepPrompt(2, false), 2},
		{keepPrompt(12, false), 12},
		// The multiplayer free mulligan's wording, and anything unreadable.
		{"a plays first. Keep your hand (keep all seven cards) or take a mulligan?", 0},
		{"", 0},
		{"Keep your hand (put some cards on the bottom)", 0},
		// A display name that itself reads like the prompt: the last match wins.
		{"Keep your hand (put 5 cards plays first. Keep your hand (put 1 card on the bottom of your library) or take a mulligan?", 1},
	} {
		if got := keepBottomCount(tc.prompt); got != tc.want {
			t.Errorf("keepBottomCount(%q) = %d, want %d", tc.prompt, got, tc.want)
		}
	}
}

// TestLandMulliganKeep pins the keep/mulligan table on constructed hands of
// seven: 2-5 lands keep on the opening seven and after one mulligan, two
// mulligans keep unless the seven are all lands or all spells, and three or
// more always keep. No rng is consumed.
func TestLandMulliganKeep(t *testing.T) {
	want := map[int]map[int]bool{ // bottom -> lands -> keep
		0: {0: false, 1: false, 2: true, 3: true, 4: true, 5: true, 6: false, 7: false},
		1: {0: false, 1: false, 2: true, 3: true, 4: true, 5: true, 6: false, 7: false},
		2: {0: false, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: false},
		3: {0: true, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true},
	}
	for bottom := 0; bottom <= 3; bottom++ {
		for lands := 0; lands <= 7; lands++ {
			b := handBoard(MulliganLands, sevenWith(lands)...)
			d := keepMullDecision(bottom, true)
			r := rng(9)
			in := Decide(b, &d, r)
			if err := d.Validate(in); err != nil {
				t.Fatalf("bottom %d lands %d: invalid answer %+v: %v", bottom, lands, in, err)
			}
			gotKeep := d.Options[in.Choices[0]].Kind == "keep"
			if gotKeep != want[bottom][lands] {
				t.Errorf("bottom %d, %d lands: keep = %v, want %v", bottom, lands, gotKeep, want[bottom][lands])
			}
			if r.Uint64() != rng(9).Uint64() {
				t.Errorf("bottom %d, %d lands: the land rule consumed the rng", bottom, lands)
			}
		}
	}
	// A keep-only offer (the allowance is spent) keeps whatever the hand.
	b := handBoard(MulliganLands, sevenWith(0)...)
	d := keepMullDecision(2, false)
	if in := Decide(b, &d, rng(1)); len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Errorf("keep-only offer = %+v, want keep", in)
	}
	// A hand census that disagrees with HandSize (a castable card outside the
	// hand, say) keeps rather than guessing.
	b = handBoard(MulliganLands, sevenWith(0)...)
	b.HandSize = 8
	d = keepMullDecision(0, true)
	if in := Decide(b, &d, rng(1)); in.Choices[0] != 0 {
		t.Errorf("census mismatch = %+v, want keep", in)
	}
	// Non-basic lands count: gain lands and Evolving Wilds print "no cost".
	hand := append(sevenWith(0)[:5], wildsCard(), wildsCard())
	b = handBoard(MulliganLands, hand...)
	if in := Decide(b, &d, rng(1)); in.Choices[0] != 0 {
		t.Errorf("two Evolving Wilds = %+v, want keep (2 lands)", in)
	}
	// A {0} artifact is a spell, not a land.
	zero := Card{PrintedName: "Ornament", CMC: 0, ManaCost: "0", Castable: true}
	hand = append(sevenWith(1)[:6], zero)
	b = handBoard(MulliganLands, hand...)
	if in := Decide(b, &d, rng(1)); in.Choices[0] != 1 {
		t.Errorf("one land and a {0} artifact = %+v, want mulligan", in)
	}
}

// TestNeverMulligan pins MulliganNever: keep, whatever the hand, no rng.
func TestNeverMulligan(t *testing.T) {
	for lands := 0; lands <= 7; lands++ {
		b := handBoard(MulliganNever, sevenWith(lands)...)
		d := keepMullDecision(0, true)
		r := rng(4)
		if in := Decide(b, &d, r); in.Choices[0] != 0 {
			t.Errorf("%d lands: never-mulligan = %+v, want keep", lands, in)
		}
		if r.Uint64() != rng(4).Uint64() {
			t.Errorf("%d lands: never-mulligan consumed the rng", lands)
		}
	}
}

// TestCoinMulliganUnchanged pins that the zero rule is the historical answer
// on both shapes: the coin draws exactly as the default Decide does, and
// bottoming is chooseDiscard's.
func TestCoinMulliganUnchanged(t *testing.T) {
	hand := sevenWith(5)
	for seed := uint64(0); seed < 30; seed++ {
		d := keepMullDecision(0, true)
		withHand := Decide(handBoard(MulliganCoin, hand...), &d, rng(seed))
		bare := Decide(Board{}, &d, rng(seed))
		if !slices.Equal(withHand.Choices, bare.Choices) {
			t.Fatalf("seed %d: coin with a hand = %v, bare board = %v", seed, withHand.Choices, bare.Choices)
		}
	}
	b := handBoard(MulliganCoin, hand...)
	d := bottomDecision(7, 2)
	if got, want := Decide(b, &d, rng(1)).Choices, b.chooseDiscard(&d); !slices.Equal(got, want) {
		t.Errorf("coin bottoming = %v, want chooseDiscard's %v", got, want)
	}
}

func bottomDecision(n, m int) decision.Decision {
	d := decision.Decision{Seq: 9, Player: 0, Kind: decision.KMulligan, Min: m, Max: m}
	for i := 0; i < n; i++ {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "bottom", Obj: state.ObjID(i + 1)})
	}
	return d
}

// TestLandBottoming pins the bottoming rule on constructed sevens.
func TestLandBottoming(t *testing.T) {
	for _, tc := range []struct {
		name string
		hand []Card
		m    int
		want []int // option indices bottomed, ascending
	}{
		{
			// 4 lands + 3 spells keeping 6: at most 3 lands, so a land goes.
			// The spells need W twice and B once; Plains and Swamp cover two
			// each, so a Swamp (B: 1/2) is needed less than a Plains (W: 2/2).
			name: "four lands keep six: the less needed colour's land",
			hand: []Card{plainsCard(), swampCard(), plainsCard(), swampCard(),
				spellCard(2, "1 W"), spellCard(3, "2 W"), spellCard(2, "1 B")},
			m: 1, want: []int{1},
		},
		{
			// The same colours but three Plains: a Plains is now redundant
			// (W 2/3) beside the lone Swamp (B 1/1).
			name: "redundant colour goes first",
			hand: []Card{plainsCard(), plainsCard(), swampCard(), plainsCard(),
				spellCard(2, "1 W"), spellCard(3, "2 W"), spellCard(2, "1 B")},
			m: 1, want: []int{0},
		},
		{
			name: "three lands keep six: the highest mana value spell",
			hand: []Card{plainsCard(), plainsCard(), plainsCard(),
				spellCard(2, "1 W"), spellCard(5, "4 W"), spellCard(3, "2 W"), spellCard(1, "W")},
			m: 1, want: []int{4},
		},
		{
			name: "six lands keep five: two lands",
			hand: []Card{plainsCard(), plainsCard(), plainsCard(), plainsCard(), plainsCard(), plainsCard(),
				spellCard(4, "3 W")},
			m: 2, want: []int{0, 1},
		},
		{
			// 4 lands keeping 5: one land (cap 3), then the costliest spell.
			name: "four lands keep five: a land then a spell",
			hand: []Card{plainsCard(), plainsCard(), plainsCard(), plainsCard(),
				spellCard(2, "1 W"), spellCard(6, "5 W"), spellCard(3, "2 W")},
			m: 2, want: []int{0, 5},
		},
		{
			name: "one land keep five: two spells, ties on the lower index",
			hand: []Card{spellCard(3, "2 W"), plainsCard(), spellCard(4, "3 W"), spellCard(2, "1 W"),
				spellCard(3, "2 W"), spellCard(1, "W"), spellCard(4, "3 W")},
			m: 2, want: []int{2, 6},
		},
		{
			// Keeping two of seven: the cap is max(ceil(2/2), 2) = 2, so both
			// lands stay -- the rule never bottoms below two lands.
			name: "never below two lands",
			hand: []Card{spellCard(1, "W"), plainsCard(), spellCard(2, "1 W"), spellCard(3, "2 W"),
				swampCard(), spellCard(1, "B"), spellCard(2, "1 B")},
			m: 5, want: []int{0, 2, 3, 5, 6},
		},
		{
			// Evolving Wilds (no recorded production) counts for every
			// colour, so the redundant Plains goes before it.
			name: "a fetch land is a fixer",
			hand: []Card{plainsCard(), wildsCard(), plainsCard(), plainsCard(),
				spellCard(2, "1 W"), spellCard(2, "1 B"), spellCard(3, "2 W")},
			m: 1, want: []int{0},
		},
	} {
		b := handBoard(MulliganLands, tc.hand...)
		d := bottomDecision(len(tc.hand), tc.m)
		r := rng(2)
		in := Decide(b, &d, r)
		if err := d.Validate(in); err != nil {
			t.Fatalf("%s: invalid answer %+v: %v", tc.name, in, err)
		}
		if !slices.Equal(in.Choices, tc.want) {
			t.Errorf("%s: bottomed %v, want %v", tc.name, in.Choices, tc.want)
		}
		if r.Uint64() != rng(2).Uint64() {
			t.Errorf("%s: bottoming consumed the rng", tc.name)
		}
	}
}
