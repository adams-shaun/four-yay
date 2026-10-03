package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestTargetedPermanentGateReadsLastKnownInformation pins CR 608.2h for a
// `ConditionDefined$ Targeted | ConditionPresent$ Permanent...` gate: "that
// permanent" names the target as it last existed on the battlefield, so a
// gate evaluated after the spell's own first effect moved the target away
// (Filigree Fracture's destroy, Vindictive Triumph's exile) still reads it
// as a permanent. Read live, the target is a card in a graveyard or exile,
// the Permanent base fails and the conditional half never happens.
func TestTargetedPermanentGateReadsLastKnownInformation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		sc   oracleScenario
	}{
		{"filigree fracture on a black enchantment draws", oracleScenario{
			Setup: map[string]oracleSeat{
				"p0": {Hand: []string{"Filigree Fracture"}},
				"p1": {Battlefield: []string{"Phyrexian Arena"}},
			},
			Steps: []oracleStep{
				{Op: "cast", Seat: 0, Card: "p0:Filigree Fracture", Mana: "CCG", Targets: []string{"p1:Phyrexian Arena"}},
				{Op: "resolve"},
			},
			Expect: []oracleExpect{
				{Card: "p1:Phyrexian Arena", Zone: "graveyard"},
				{HandSize: map[string]int{"p0": 1}},
			},
		}},
		{"filigree fracture on a white enchantment does not draw", oracleScenario{
			Setup: map[string]oracleSeat{
				"p0": {Hand: []string{"Filigree Fracture"}},
				"p1": {Battlefield: []string{"Glorious Anthem"}},
			},
			Steps: []oracleStep{
				{Op: "cast", Seat: 0, Card: "p0:Filigree Fracture", Mana: "CCG", Targets: []string{"p1:Glorious Anthem"}},
				{Op: "resolve"},
			},
			Expect: []oracleExpect{
				{Card: "p1:Glorious Anthem", Zone: "graveyard"},
				{HandSize: map[string]int{"p0": 0}},
			},
		}},
		{"vindictive triumph returns a mana value two creature", oracleScenario{
			Setup: map[string]oracleSeat{
				"p0": {Hand: []string{"Vindictive Triumph"}},
				"p1": {Battlefield: []string{"Grizzly Bears"}},
			},
			Steps: []oracleStep{
				{Op: "cast", Seat: 0, Card: "p0:Vindictive Triumph", Mana: "WBB", Targets: []string{"p1:Grizzly Bears"}},
				{Op: "resolve"},
			},
			Expect: []oracleExpect{
				{Card: "p1:Grizzly Bears", Zone: "battlefield"},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fails, transcript, _ := runOracleScenario(reg, tc.sc)
			if len(fails) > 0 {
				t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
			}
		})
	}
}

// TestThresholdMayPlayStaticGrantsOpponentsExiledCard pins Null Summoner's
// "Threshold -- As long as there are seven or more cards in your graveyard,
// you may cast the exiled card, and mana of any type can be spent to cast that
// spell." A printed MayPlay$ static gated by an ability-word Condition$ used to
// fail closed on every value but PlayerTurn, so the exiled card was never
// offered; the layer walk already evaluates the Condition$ before the grant is
// registered, and the zone-permission read now goes through the same
// evaluator.
func TestThresholdMayPlayStaticGrantsOpponentsExiledCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	graveyard := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "Wastes"
		}
		return out
	}
	setup := func(n int) map[string]oracleSeat {
		return map[string]oracleSeat{
			"p0": {Hand: []string{"Null Summoner"}, Graveyard: graveyard(n)},
			"p1": {Hand: []string{"Lightning Bolt"}},
		}
	}
	exile := []oracleStep{
		{Op: "cast", Seat: 0, Card: "p0:Null Summoner", Mana: "CCUB"},
		{Op: "resolve", Targets: []string{"p1"}},
	}
	t.Run("seven cards: cast with blue mana", func(t *testing.T) {
		sc := oracleScenario{Setup: setup(7), Steps: append(append([]oracleStep(nil), exile...),
			oracleStep{Op: "cast", Seat: 0, Card: "p1:Lightning Bolt", Mana: "U", Targets: []string{"p1"}},
			oracleStep{Op: "resolve"}),
			Expect: []oracleExpect{{Life: map[string]int32{"p1": 17}}, {Card: "p1:Lightning Bolt", Zone: "graveyard"}}}
		if fails, transcript, _ := runOracleScenario(reg, sc); len(fails) > 0 {
			t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
		}
	})
	t.Run("six cards: not offered", func(t *testing.T) {
		no := false
		sc := oracleScenario{Setup: setup(6), Steps: append(append([]oracleStep(nil), exile...),
			oracleStep{Op: "mana", Seat: 0, Mana: "R"}),
			Expect: []oracleExpect{{Offered: &oracleOffered{Seat: 0, Kind: "cast", Card: "p1:Lightning Bolt"}, Want: &no}}}
		if fails, transcript, _ := runOracleScenario(reg, sc); len(fails) > 0 {
			t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
		}
	})
}

// TestOutsideTheGameRevealExactlyTwoDifferentNames pins Extrapolate the
// Impossible's head, a hidden Sideboard search with DifferentNames$ True,
// Exactly$ True and no Destination$. The revealed cards stay outside the game
// for the opponent's pick (they used to be binned by ParseZone's Graveyard
// default), and a sideboard holding only same-named cards finds nothing
// instead of posing a Min 2 choose whose every answer repeats a name (a
// decision no answer satisfies: the match wedged).
func TestOutsideTheGameRevealExactlyTwoDifferentNames(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	t.Run("opponent picks one of two revealed", func(t *testing.T) {
		sc := oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Hand: []string{"Extrapolate the Impossible"},
				Sideboard: []string{"Lightning Bolt", "Grizzly Bears"}}},
			Steps: []oracleStep{
				{Op: "cast", Seat: 0, Card: "p0:Extrapolate the Impossible", Mana: "CB"},
				{Op: "resolve", Answers: []oracleAnswer{
					{Kind: "choose", Pick: []string{"p0:Lightning Bolt", "p0:Grizzly Bears"}},
					{Kind: "choose", Pick: []string{"p0:Grizzly Bears"}}}},
			},
			Expect: []oracleExpect{{Card: "p0:Grizzly Bears", Zone: "hand"}, {Card: "p0:Lightning Bolt", Zone: "sideboard"}},
		}
		if fails, transcript, _ := runOracleScenario(reg, sc); len(fails) > 0 {
			t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
		}
	})
	t.Run("only same-named cards finds nothing", func(t *testing.T) {
		sc := oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Hand: []string{"Extrapolate the Impossible"},
				Sideboard: []string{"Grizzly Bears", "Grizzly Bears"}}},
			Steps: []oracleStep{
				{Op: "cast", Seat: 0, Card: "p0:Extrapolate the Impossible", Mana: "CB"},
				{Op: "resolve"},
			},
			Expect: []oracleExpect{{Card: "p0:Grizzly Bears", Zone: "sideboard"}, {Card: "p0:Grizzly Bears#2", Zone: "sideboard"},
				{HandSize: map[string]int{"p0": 0}}},
		}
		if fails, transcript, _ := runOracleScenario(reg, sc); len(fails) > 0 {
			t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
		}
	})
}
