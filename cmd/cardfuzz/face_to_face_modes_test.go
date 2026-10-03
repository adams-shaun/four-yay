package main

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// faceToFaceDeck is a mono-red deck dense with Face to Face, so every game
// casts the rock-paper-scissors match many times.
func faceToFaceDeck() genDeck {
	d := genDeck{Colour: "R"}
	for i := 0; i < 24; i++ {
		d.Cards = append(d.Cards, "Mountain")
	}
	for i := 0; i < 36; i++ {
		d.Cards = append(d.Cards, "Face to Face")
	}
	return d
}

// TestFaceToFaceEndsUnderEveryAutoPayMode plays Face to Face mirrors under
// every -autopay mode, with and without the explore seat: every seat kind
// cardfuzz builds must throw at random (AILogic$ Random), so a tied match
// re-throws with fresh draws and no game livelocks.
func TestFaceToFaceEndsUnderEveryAutoPayMode(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	decks := []genDeck{faceToFaceDeck(), faceToFaceDeck()}
	for _, mode := range autoPayModes {
		for _, explore := range []bool{false, true} {
			apc, err := parseAutoPay(mode, false)
			if err != nil {
				t.Fatal(err)
			}
			for seed := uint64(1); seed <= 4; seed++ {
				fl, gc := playGame(reg, decks, seed*7919, 100, 20000, 20000, true, explore, apc)
				if fl != nil {
					t.Fatalf("autopay %s explore %v seed %d: %s: %.400s", mode, explore, seed*7919, fl.Kind, fl.Diag)
				}
				if gc == nil {
					t.Fatalf("autopay %s explore %v seed %d: no game coverage", mode, explore, seed*7919)
				}
				if !gc.cast["Face to Face"] {
					t.Fatalf("autopay %s explore %v seed %d: Face to Face was never cast", mode, explore, seed*7919)
				}
			}
		}
	}
}
