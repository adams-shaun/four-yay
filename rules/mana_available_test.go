package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestAvailableManaBasic is the heart of the feature: an untapped Plains
// (its intrinsic tap-for-mana) is one white available; a tapped source
// contributes nothing; and a player with no battlefield permanents has
// nothing available.
func TestAvailableManaBasic(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
	island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	// A tapped Island is not available until it untaps.
	e.G.Obj(island).Tapped = true

	got := e.AvailableMana(0)
	if got[state.MW] != 1 {
		t.Errorf("available white = %d, want 1 (untapped Plains)", got[state.MW])
	}
	if got[state.MU] != 0 {
		t.Errorf("available blue = %d, want 0 (Island is tapped)", got[state.MU])
	}
	// Player 1 controls nothing.
	if oth := e.AvailableMana(1); oth.Total() != 0 {
		t.Errorf("player 1 available = %v, want 0 (no permanents)", oth)
	}
}

// TestAvailableManaSumsAcrossSources verifies the aggregate is the sum over
// every qualifying untapped source, across colours, in the WUBRGC slots.
// TestAvailableManaOmitsMultiAbilitySource pins AvailableMana's conservative
// aggregate rule. A Volcanic Island can produce U or R, but state.Mana cannot
// express that alternative as one fixed vector; counting both would claim the
// land can pay both pips with one tap.
func TestAvailableManaOmitsMultiAbilitySource(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Volcanic Island\nTypes:Land Island Mountain\nOracle:x\n")
	if got := e.AvailableMana(0); got.Total() != 0 {
		t.Fatalf("Volcanic Island available = %v, want no fixed mana from U-or-R choice", got)
	}
}

func TestAvailableManaSumsAcrossSources(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
	onBoard(t, e, 0, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
	onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	got := e.AvailableMana(0)
	if got[state.MW] != 2 {
		t.Errorf("available white = %d, want 2 (two Plains)", got[state.MW])
	}
	if got[state.MB] != 1 {
		t.Errorf("available black = %d, want 1 (one Swamp)", got[state.MB])
	}
}

// TestAvailableManaExcludesPaidCosts is the honesty rule for an ability with
// a non-mana cost: a mana ability whose activation cost also sacrifices the
// source, pays life or costs mana is NOT free mana by tapping and must not
// be counted as available. A permanent whose only mana ability is paid
// contributes nothing; a permanent mixing one free tap with one paid ability
// contributes only the free one.
func TestAvailableManaExcludesPaidCosts(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// A land whose only mana ability sacrifices itself for two black.
	onBoard(t, e, 0, "Name:SacLand\nTypes:Land\nA:AB$ Mana | Cost$ T Sac<1/CARDNAME> | Produced$ B | Amount$ 2 | Oracle:x\n")
	// A land mixing a free tap-for-white with a paid tap-for-black.
	onBoard(t, e, 0, "Name:MixedLand\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ W\nA:AB$ Mana | Cost$ T Sac<1/CARDNAME> | Produced$ B | Amount$ 2\nOracle:x\n")

	got := e.AvailableMana(0)
	if got[state.MB] != 0 {
		t.Errorf("available black = %d, want 0 (the only black source is an expensive sac cost)", got[state.MB])
	}
	if got[state.MW] != 1 {
		t.Errorf("available white = %d, want 1 (the free tap half of the mixed land)", got[state.MW])
	}
}

// TestAvailableManaAnyReportsItsAlternatives keeps the available-mana
// projection honest about what an untapped Any source can produce: it offers
// each WUBRG colour and no colourless unit, while one activation still adds
// only the colour the player chooses.
func TestAvailableManaAnyReportsItsAlternatives(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Cavern\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | Oracle:x\n")
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Tapped {
		t.Fatalf("Any source precondition failed: %+v", e.G.Obj(id))
	}
	got := e.AvailableMana(0)
	for i := 0; i < 5; i++ {
		if got[i] != 1 {
			t.Errorf("available colour %d = %d, want one possible unit", i, got[i])
		}
	}
	if got[state.MC] != 0 {
		t.Errorf("available colourless = %d, want 0: Any is not a colourless producer", got[state.MC])
	}
}

// TestAvailableManaIndeterminateAmountContributesNothing is the honesty rule
// for an Amount$ the projection cannot statically price ("X", a Count$): such
// an ability yields no amount the pool is guaranteed to receive, so it
// contributes zero.
func TestAvailableManaIndeterminateAmountContributesNothing(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Indet\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ W | Amount$ X | Oracle:x\n")
	got := e.AvailableMana(0)
	if got.Total() != 0 {
		t.Errorf("available = %v, want 0 (X amount is indeterminate)", got)
	}
}

// TestAvailableManaIsBattlefieldOnly verifies available mana comes only from
// the battlefield: a card in a hand or graveyard contributes nothing, and no
// hidden zone is consulted.
func TestAvailableManaIsBattlefieldOnly(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// A bear (a creature, no mana ability) and an untapped source.
	onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	got := e.AvailableMana(0)
	if got.Total() != 0 {
		t.Errorf("available = %v, want 0 (creatures without a mana ability contribute nothing)", got)
	}
}

// TestAvailableManaMatchesCardProjection pins the claim in AvailableMana's
// doc that a pure-tap face's available mana equals the aggregate the cards
// package folds into ManaProduction for that same face: the per-ability
// folding here must agree with cards.Face.ManaProduction for the free-tap
// ability, so the engine and the projected CardView.Produces never drift.
func TestAvailableManaMatchesCardProjection(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"
	onBoard(t, e, 0, src)
	got := e.AvailableMana(0)
	// The face's own production folds the same ability; AvailableMana must
	// equal it translated into state.Mana slots.
	f := card(t, src).Faces[0]
	mp := f.ManaProduction()
	for i := 0; i < 6; i++ {
		if int32(mp.Colour[i]) != got[i] {
			t.Errorf("slot %d: available %d, card production %d (drift)", i, got[i], mp.Colour[i])
		}
	}
}

// TestAvailableManaComboDualNamesItsColoursOnly is the fb-windgrace fix on
// the aggregate side: a Blackcleave-Cliffs-shaped source ("Produced$ Combo
// B R") contributes its two real colours and NO colourless -- the old rune
// walk counted the letters of the word "Combo" as five phantom colourless,
// which the mana rail then advertised as tappable.
func TestAvailableManaComboDualNamesItsColoursOnly(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Blackcleave Cliffs\nManaCost:no cost\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo B R | Oracle:x\n")
	got := e.AvailableMana(0)
	want := state.Mana{state.MB: 1, state.MR: 1}
	if got != want {
		t.Errorf("available = %v, want %v (one B and one R, no colourless)", got, want)
	}
}

// TestAvailableManaColorIdentityClaimsNothing pins the other half: a
// Command-Tower-shaped source ("Produced$ Combo ColorIdentity") contributes
// nothing at all until commander-identity derivation exists -- the honest
// conservative value (the same convention as an Indeterminate amount), not
// the 18 phantom colourless the rune walk counted.
func TestAvailableManaColorIdentityClaimsNothing(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Command Tower\nManaCost:no cost\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo ColorIdentity | Oracle:x\n")
	if got := e.AvailableMana(0); got.Total() != 0 {
		t.Errorf("available = %v, want 0 (ColorIdentity is not derivable here)", got)
	}
}

// TestAvailableManaSameSymbolTokenCountsTwice pins that the shared parse
// still counts a plain multi-symbol token per rune ("RR" two red), so the
// fix narrowed only the unrecognised-word case.
func TestAvailableManaSameSymbolTokenCountsTwice(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:RRland\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ RR | Oracle:x\n")
	if got := e.AvailableMana(0); got[state.MR] != 2 {
		t.Errorf("available = %v, want two red", got)
	}
}
