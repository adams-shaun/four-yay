package rules

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// landwalkCorpusCard parses a real Forge script straight from the live corpus
// (never a checked-in copy) so each proof below is tied to an actual carrier
// of the printed `K:Landwalk:<spec>` form, not a lookalike handwritten card.
func landwalkCorpusCard(t *testing.T, path string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", path, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", path, ds)
	}
	return c
}

// landwalkAttacker places a real corpus attacker under seat 1 and declares it
// attacking seat 0, returning its id. Seat 1 is the attacker because seat 0's
// creatures are the prospective blockers, matching the other combat-keyword
// tests in this package.
func landwalkAttacker(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	a := onBoardCard(t, e, 1, c)
	e.G.Obj(a).IsAttacking, e.G.Obj(a).Attacking = true, 0
	return a
}

// TestLandwalkEvadesDefenderLands proves CR 702.14 against the reported
// carrier, Colossal Whale (`K:Landwalk:Island`): the whale is unblockable
// while the DEFENDING player controls an Island, and becomes ordinarily
// blockable once that Island leaves.
func TestLandwalkEvadesDefenderLands(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	whale := landwalkAttacker(t, e, landwalkCorpusCard(t, "c/colossal_whale.txt"))
	// Precondition: the rule reads this exact keyword, and the defending
	// player is seat 0 (the seat the attacker was declared against).
	if !e.HasKeyword(whale, "Landwalk") {
		t.Fatal("Colossal Whale does not read as carrying printed Landwalk")
	}
	if got := e.G.Obj(whale).Attacking; got != 0 {
		t.Fatalf("attacker declared against seat %d, want seat 0", got)
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	// Precondition: the prospective blocker is a real battlefield creature
	// controlled by the defending player.
	if b := e.G.Obj(blocker); b == nil || b.Zone != state.ZBattlefield || b.Controller != 0 {
		t.Fatalf("blocker precondition failed: %+v", e.G.Obj(blocker))
	}

	// No Island yet: the whale is ordinarily blockable.
	if !e.canBlock(blocker, whale) {
		t.Fatal("islandwalk attacker was unblockable with no Island on the defending player's board")
	}

	island := onBoard(t, e, 0, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	if got := e.G.Obj(island); got.Zone != state.ZBattlefield || got.Controller != 0 {
		t.Fatalf("Island precondition failed: %+v", got)
	}
	if e.canBlock(blocker, whale) {
		t.Fatal("islandwalk attacker was blockable while the defending player controlled an Island")
	}

	// The land being gone is what lifts the restriction: move it to the
	// graveyard and the same pair is legal again.
	e.emit(events.Event{Kind: events.MoveZone, Obj: island, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(island); got.Zone != state.ZGraveyard {
		t.Fatalf("Island precondition after move failed: zone %v", got.Zone)
	}
	if !e.canBlock(blocker, whale) {
		t.Fatal("islandwalk attacker stayed unblockable after the defending player's only Island left")
	}

	// A land under the ATTACKER's own control is not the defender's and must
	// not confer evasion: the rule reads the defending player's lands.
	onBoard(t, e, 1, "Name:Mine\nTypes:Basic Land Island\nOracle:x\n")
	if !e.canBlock(blocker, whale) {
		t.Fatal("an Island controlled by the attacker itself conferred islandwalk")
	}
}

// TestLandwalkBasicLandTypeForms covers the printed basic-type forms with a
// positive and a negative land-control case apiece.
func TestLandwalkBasicLandTypeForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		land   string
		absent string
	}{
		{"islandwalk", "c/colossal_whale.txt", "Name:Isle\nTypes:Basic Land Island\nOracle:x\n", "Name:Mt\nTypes:Basic Land Mountain\nOracle:x\n"},
		{"forestwalk", "k/koths_courier.txt", "Name:For\nTypes:Basic Land Forest\nOracle:x\n", "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"},
		{"swampwalk", "k/krosan_constrictor.txt", "Name:Sw\nTypes:Basic Land Swamp\nOracle:x\n", "Name:For\nTypes:Basic Land Forest\nOracle:x\n"},
		{"mountainwalk", "v/vug_lizard.txt", "Name:Mt\nTypes:Basic Land Mountain\nOracle:x\n", "Name:Sw\nTypes:Basic Land Swamp\nOracle:x\n"},
		{"plainswalk", "b/boggart_arsonists.txt", "Name:Pl\nTypes:Basic Land Plains\nOracle:x\n", "Name:Mt\nTypes:Basic Land Mountain\nOracle:x\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := combatEngine(t)
			a := landwalkAttacker(t, e, landwalkCorpusCard(t, tc.path))
			if !e.HasKeyword(a, "Landwalk") {
				t.Fatalf("%s: carrier does not read as carrying printed Landwalk", tc.path)
			}
			blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
			// Negative first: a non-matching basic land does not confer the walk.
			wrong := onBoard(t, e, 0, tc.absent)
			if e.G.Obj(wrong).Zone != state.ZBattlefield {
				t.Fatal("wrong-land precondition failed")
			}
			if !e.canBlock(blocker, a) {
				t.Fatalf("%s: a non-matching land conferred evasion", tc.name)
			}
			// Positive: the named land does.
			right := onBoard(t, e, 0, tc.land)
			if e.G.Obj(right).Zone != state.ZBattlefield {
				t.Fatal("named-land precondition failed")
			}
			if e.canBlock(blocker, a) {
				t.Fatalf("%s: attacker was blockable while the defender controlled the named land", tc.name)
			}
		})
	}
}

// TestLandwalkNonbasicQualifier covers `K:Landwalk:Land.nonBasic` (Dryad
// Sophisticate): any land that is not basic confers the walk, and a board of
// basics does not.
func TestLandwalkNonbasicQualifier(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	a := landwalkAttacker(t, e, landwalkCorpusCard(t, "d/dryad_sophisticate.txt"))
	if !e.HasKeyword(a, "Landwalk") {
		t.Fatal("Dryad Sophisticate does not read as carrying printed Landwalk")
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	basic := onBoard(t, e, 0, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	if e.G.Obj(basic).Zone != state.ZBattlefield {
		t.Fatal("basic-land precondition failed")
	}
	if !e.canBlock(blocker, a) {
		t.Fatal("Land.nonBasic walker was evasive over a board of only basic lands")
	}
	nonbasic := onBoard(t, e, 0, "Name:Ruins\nTypes:Land Desert\nOracle:x\n")
	if e.G.Obj(nonbasic).Zone != state.ZBattlefield {
		t.Fatal("nonbasic-land precondition failed")
	}
	if e.canBlock(blocker, a) {
		t.Fatal("Land.nonBasic walker was blockable while the defender controlled a nonbasic land")
	}
}

// TestLandwalkLegendaryQualifier covers `K:Landwalk:Land.Legendary` (Livonya
// Silone): a legendary land confers the walk, an ordinary land does not.
func TestLandwalkLegendaryQualifier(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	a := landwalkAttacker(t, e, landwalkCorpusCard(t, "l/livonya_silone.txt"))
	if !e.HasKeyword(a, "Landwalk") {
		t.Fatal("Livonya Silone does not read as carrying printed Landwalk")
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	plain := onBoard(t, e, 0, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	if e.G.Obj(plain).Zone != state.ZBattlefield {
		t.Fatal("nonlegendary-land precondition failed")
	}
	if !e.canBlock(blocker, a) {
		t.Fatal("Land.Legendary walker was evasive over a nonlegendary land")
	}
	legendary := onBoard(t, e, 0, "Name:Hall\nTypes:Legendary Land\nOracle:x\n")
	if e.G.Obj(legendary).Zone != state.ZBattlefield {
		t.Fatal("legendary-land precondition failed")
	}
	if e.canBlock(blocker, a) {
		t.Fatal("Land.Legendary walker was blockable while the defender controlled a legendary land")
	}
}

// TestLandwalkSnowQualifierForms covers both snow spellings the corpus carries:
// the bare snow-land form (`Land.Snow`, Zombie Musher) and the snow-basic form
// (`Swamp.Snow`, Legions of Lim-Dûl). Each must need a SNOW land of the right
// ordinary type, not merely any snow land or any Swamp.
func TestLandwalkSnowQualifierForms(t *testing.T) {
	t.Parallel()
	t.Run("land_snow", func(t *testing.T) {
		e := combatEngine(t)
		a := landwalkAttacker(t, e, landwalkCorpusCard(t, "z/zombie_musher.txt"))
		if !e.HasKeyword(a, "Landwalk") {
			t.Fatal("Zombie Musher does not read as carrying printed Landwalk")
		}
		blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
		plain := onBoard(t, e, 0, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
		if e.G.Obj(plain).Zone != state.ZBattlefield {
			t.Fatal("nonsnow-land precondition failed")
		}
		if !e.canBlock(blocker, a) {
			t.Fatal("Land.Snow walker was evasive over a non-snow land")
		}
		snow := onBoard(t, e, 0, "Name:SnowIsle\nTypes:Basic Snow Land Island\nOracle:x\n")
		if e.G.Obj(snow).Zone != state.ZBattlefield {
			t.Fatal("snow-land precondition failed")
		}
		if e.canBlock(blocker, a) {
			t.Fatal("Land.Snow walker was blockable while the defender controlled a snow land")
		}
	})
	t.Run("swamp_snow", func(t *testing.T) {
		e := combatEngine(t)
		a := landwalkAttacker(t, e, landwalkCorpusCard(t, "l/legions_of_lim_dul.txt"))
		if !e.HasKeyword(a, "Landwalk") {
			t.Fatal("Legions of Lim-Dûl does not read as carrying printed Landwalk")
		}
		blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
		// A snow ISLAND is snow but not a Swamp; a plain SWAMP is a Swamp but
		// not snow. Neither alone may confer swampwalk.
		snowIsland := onBoard(t, e, 0, "Name:SnowIsle\nTypes:Basic Snow Land Island\nOracle:x\n")
		plainSwamp := onBoard(t, e, 0, "Name:Sw\nTypes:Basic Land Swamp\nOracle:x\n")
		if e.G.Obj(snowIsland).Zone != state.ZBattlefield || e.G.Obj(plainSwamp).Zone != state.ZBattlefield {
			t.Fatal("snow-Island / plain-Swamp precondition failed")
		}
		if !e.canBlock(blocker, a) {
			t.Fatal("Swamp.Snow walker was evasive without a snow Swamp")
		}
		snowSwamp := onBoard(t, e, 0, "Name:SnowSw\nTypes:Basic Snow Land Swamp\nOracle:x\n")
		if e.G.Obj(snowSwamp).Zone != state.ZBattlefield {
			t.Fatal("snow-Swamp precondition failed")
		}
		if e.canBlock(blocker, a) {
			t.Fatal("Swamp.Snow walker was blockable while the defender controlled a snow Swamp")
		}
	})
}

// TestLandwalkReadsGrantedLandType proves the rule reads the land's DERIVED
// (layer-4) type, not only its printed type: Yavimaya, Cradle of Growth's
// `S:Mode$ Continuous | Affected$ Land | AddType$ Forest` grants the Forest
// type to the defending player's non-Forest land, and that granted type alone
// must confer forestwalk. This exercises the DerivedTypes table landwalkEvades
// binds through withNames; a printed-type-only read passes every other test in
// this file but fails here.
//
// Yavimaya must sit in the engine's DECK (not merely be placed on the board):
// layer4InPool is armed at genesis by a deck scan, exactly as a real match
// does, so a card dropped in without a deck would test the fixture, not the
// rule. The engine here is combatEngine's construction with Yavimaya's real
// parsed card in seat 0's deck.
func TestLandwalkReadsGrantedLandType(t *testing.T) {
	t.Parallel()
	yavimaya := landwalkCorpusCard(t, "y/yavimaya_cradle_of_growth.txt")
	deck := mountainDeck(t, 39)
	deck = append(deck, yavimaya)
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}}))
	if !e.layer4InPool {
		t.Fatal("precondition: Yavimaya in the deck did not arm layer4InPool")
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	a := landwalkAttacker(t, e, landwalkCorpusCard(t, "k/koths_courier.txt"))
	if !e.HasKeyword(a, "Landwalk") {
		t.Fatal("Koth's Courier does not read as carrying printed Landwalk")
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	// A Mountain is a land but NOT a Forest by printed type: with no granted
	// type it must not confer forestwalk.
	mountain := onBoard(t, e, 0, "Name:Mt\nTypes:Basic Land Mountain\nOracle:x\n")
	if e.G.Obj(mountain).Zone != state.ZBattlefield {
		t.Fatal("Mountain precondition failed")
	}
	if !e.canBlock(blocker, a) {
		t.Fatal("forestwalk attacker was evasive over a printed Mountain")
	}
	// Yavimaya on the battlefield grants every land the Forest type (layer 4).
	// The same Mountain now carries the granted Forest type and confers
	// forestwalk.
	onBoardCard(t, e, 0, yavimaya)
	if !slices.Contains(e.Derived(mountain).Types, "Forest") {
		t.Fatalf("granted Forest type precondition failed: derived types %v", e.Derived(mountain).Types)
	}
	if e.canBlock(blocker, a) {
		t.Fatal("a GRANTED Forest type did not confer forestwalk (rule read printed types only)")
	}
}

// TestLandwalkUnknownParameterFailsClosed proves an unread qualifier is not
// silently treated as a basic walk nor as a universal evasion: the creature
// stays ordinarily blockable whatever the defender controls.
func TestLandwalkUnknownParameterFailsClosed(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	// A synthetic carrier with a qualifier no land can satisfy. The keyword is
	// still a Landwalk head, so the test proves the spec path, not the head.
	a := landwalkAttacker(t, e, card(t, "Name:Odd\nManaCost:1\nTypes:Creature\nPT:2/2\nK:Landwalk:NotARealLandType\nOracle:x\n"))
	if !e.HasKeyword(a, "Landwalk") {
		t.Fatal("synthetic carrier does not read as carrying Landwalk")
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	island := onBoard(t, e, 0, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	if e.G.Obj(island).Zone != state.ZBattlefield {
		t.Fatal("Island precondition failed")
	}
	if !e.canBlock(blocker, a) {
		t.Fatal("an unread Landwalk qualifier silently granted evasion")
	}

	// A bare `Landwalk` with no parameter likewise stays blockable.
	b := landwalkAttacker(t, e, card(t, "Name:Bare\nManaCost:1\nTypes:Creature\nPT:2/2\nK:Landwalk\nOracle:x\n"))
	if !e.canBlock(blocker, b) {
		t.Fatal("a parameterless Landwalk silently granted universal evasion")
	}
}

// TestLandwalkDescriptionSuffixIsNotPartOfSpec proves the second colon Forge
// uses for the reminder text (`Landwalk:Land.Snow:snow Land`) is stripped: the
// walk matches a snow land and does not match a land merely named like the
// description.
func TestLandwalkDescriptionSuffixIsNotPartOfSpec(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	a := landwalkAttacker(t, e, card(t, "Name:Desc\nManaCost:1\nTypes:Creature\nPT:2/2\nK:Landwalk:Island:This be islandwalk\nOracle:x\n"))
	spec, ok := landwalkSpec("Landwalk:Island:This be islandwalk")
	if !ok || spec != "Island" {
		t.Fatalf("landwalkSpec kept the description: spec=%q ok=%v", spec, ok)
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if !e.canBlock(blocker, a) {
		t.Fatal("precondition: no Island yet")
	}
	island := onBoard(t, e, 0, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	if e.G.Obj(island).Zone != state.ZBattlefield {
		t.Fatal("Island precondition failed")
	}
	if e.canBlock(blocker, a) {
		t.Fatal("the description suffix was read as part of the land filter")
	}
}

// TestLandwalkReadsLandTypeNotName proves the rule reads the land's actual
// CHARACTERISTICS, not a substring of its name: a land NAMED like a Forest but
// typed as a Desert does not confer forestwalk, and a land with an unrelated
// name but the Forest type does.
func TestLandwalkReadsLandTypeNotName(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	a := landwalkAttacker(t, e, landwalkCorpusCard(t, "k/koths_courier.txt"))
	if !e.HasKeyword(a, "Landwalk") {
		t.Fatal("Koth's Courier does not read as carrying printed Landwalk")
	}
	blocker := onBoard(t, e, 0, "Name:Blocker\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n")
	// Named "Forest" but NOT a Forest by type: must not confer forestwalk.
	misnamed := onBoard(t, e, 0, "Name:Forest\nTypes:Land Desert\nOracle:x\n")
	if e.G.Obj(misnamed).Zone != state.ZBattlefield {
		t.Fatal("misnamed-land precondition failed")
	}
	if !e.canBlock(blocker, a) {
		t.Fatal("a land merely NAMED Forest conferred forestwalk")
	}
	// Unrelated name, real Forest type: confers forestwalk.
	realOne := onBoard(t, e, 0, "Name:Bog\nTypes:Basic Land Forest\nOracle:x\n")
	if e.G.Obj(realOne).Zone != state.ZBattlefield {
		t.Fatal("forest-land precondition failed")
	}
	if e.canBlock(blocker, a) {
		t.Fatal("forestwalk read names instead of the land's Forest type")
	}
}
