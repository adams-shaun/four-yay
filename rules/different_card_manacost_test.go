package rules

// Task diffcount1 end-to-end pin on the real corpus carrier: Eris, Roar of
// the Storm carries
//   S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ X | EffectZone$ All ...
//   SVar:X:Count$ValidGraveyard Instant.YouOwn,Sorcery.YouOwn$DifferentCardManaCost/Times.2
// -- "This spell costs {2} less to cast for each different mana value among
// instant and sorcery cards in your graveyard." Before the DifferentCardManaCost
// head existed the SVar evaluated to 0, so Eris always cost full price.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	erisGyInstant1Src = "Name:Grave Bolt\nManaCost:U\nTypes:Instant\nOracle:x\n"
	erisGyInstant2Src = "Name:Grave Trick\nManaCost:1 U\nTypes:Instant\nOracle:x\n"
	erisGySorcery3Src = "Name:Grave Rite\nManaCost:2 R\nTypes:Sorcery\nOracle:x\n"
	// A creature in the same graveyard must NOT count: Eris reads instants
	// and sorceries only.
	erisGyCreature4Src = "Name:Grave Hound\nManaCost:3 B\nTypes:Creature Hound\nPT:2/2\nOracle:x\n"
)

// erisGame builds a 2-seat game whose protagonist seat 0 holds the real
// corpus Eris plus three graveyard instants/sorceries (mana values 1, 2 and 3)
// and a graveyard creature, driven to turn 3's Main1 so seat 0 can cast.
func erisGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	eris, ok := reg.Lookup("Eris, Roar of the Storm")
	if !ok {
		t.Fatal("Eris, Roar of the Storm not found in the compiled corpus registry")
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{eris, card(t, erisGyInstant1Src), card(t, erisGyInstant2Src),
				card(t, erisGySorcery3Src), card(t, erisGyCreature4Src)}, mountainDeck(t, 35)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()
	erisID := findAndMoveToHand(t, e, 0, "Eris, Roar of the Storm")
	addToGraveyard(t, e, 0, erisGyInstant1Src)
	addToGraveyard(t, e, 0, erisGyInstant2Src)
	addToGraveyard(t, e, 0, erisGySorcery3Src)
	addToGraveyard(t, e, 0, erisGyCreature4Src)
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	return e, cfg, erisID
}

// TestErisRoarOfTheStormReducesTwoPerDistinctManaValue pins the discount
// amount: three DISTINCT mana values (1, 2, 3) among the graveyard
// instants/sorceries is {6} off, and the graveyard creature does not add a
// fourth.
func TestErisRoarOfTheStormReducesTwoPerDistinctManaValue(t *testing.T) {
	t.Parallel()
	e, _, erisID := erisGame(t, 811)
	// PRECONDITION: the four cards really are in seat 0's graveyard; without
	// them a zero reduction would be read for the wrong reason.
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 4 {
		t.Fatalf("seat 0 graveyard holds %d cards, want 4", got)
	}
	if got := reduceOf(t, e, 0, erisID); got != 6 {
		t.Fatalf("Eris reduction with three distinct MVs = %d, want 6 ({2} each)", got)
	}
}

// TestErisRoarOfTheStormCastsForSixLess pins the reduction as real money: with
// {2}{U}{R} floated, the discounted {8}{U}{R} is payable and the pool empties.
func TestErisRoarOfTheStormCastsForSixLess(t *testing.T) {
	t.Parallel()
	e, cfg, _ := erisGame(t, 812)
	addMana(t, e, 0, "CCUR")
	opt := castByName(t, e, 0, "Eris, Roar of the Storm")
	if opt == nil {
		t.Fatal("Eris must be castable with {2}{U}{R} when three distinct graveyard MVs are out")
	}
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after casting Eris = %d, want 0 ({2}{U}{R} paid)", got)
	}
	replayCheck(t, e, cfg)
}

// TestErisRoarOfTheStormNoGraveyardCostsFull pins the non-discounted side: an
// empty graveyard leaves the reduction at 0 and the full {8}{U}{R} real.
func TestErisRoarOfTheStormNoGraveyardCostsFull(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	eris, ok := reg.Lookup("Eris, Roar of the Storm")
	if !ok {
		t.Fatal("Eris, Roar of the Storm not found in the compiled corpus registry")
	}
	cfg := seatZeroStart(Config{Seed: 813, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{eris}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()
	erisID := findAndMoveToHand(t, e, 0, "Eris, Roar of the Storm")
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	if got := reduceOf(t, e, 0, erisID); got != 0 {
		t.Fatalf("Eris reduction with an empty graveyard = %d, want 0", got)
	}
}
