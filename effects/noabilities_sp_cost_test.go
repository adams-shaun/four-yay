package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNoAbilitiesNonManaCastCost is the effects leaf for the SP-with-an-
// additional-cost class: a printed-vanilla creature whose `SP$ PermanentCreature`
// line carries a non-mana cost part is NOT "no abilities" (Forge's
// Card.hasNoAbilities skips an SP only when isBasicSpell && isOnlyManaCost).
// Before the SP branch read the cost, every one of these matched
// `Creature.NoAbilities` and was granted Muraganda Petroglyphs' +2/+2, found
// by Fang-Druid Summoner, reanimated by Ruxa, and excluded by Jasmine Boreal's
// blocker clause -- all contrary to CR 113.12.
func TestNoAbilitiesNonManaCastCost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})

	// Every one of these is printed vanilla (no keyword/static/trigger/AB):
	// the ONLY thing that makes it ability-bearing is its cast cost.
	cases := []struct {
		name string
		card string
	}{
		{"exile-from-grave", "Makeshift Mauler"}, // Cost$ 3 U ExileFromGrave<1/Creature>
		{"discard", "Mardu Outrider"},            // Cost$ 1 B B Discard<1/Card>
		{"tap-two", "Warlord's Elite"},           // Cost$ 2 W tapXType<2/...>
	}
	for _, tc := range cases {
		o := corpusObject(t, reg, g, tc.card)
		f := o.Face()
		if f == nil {
			t.Fatalf("precondition: %s has no printed face", tc.card)
		}
		// Precondition: the card really is printed vanilla apart from its SP,
		// so only the cost read can exclude it -- the test cannot pass
		// vacuously by some keyword/static the card already carried.
		if len(f.Keywords) != 0 || len(f.Statics) != 0 || len(f.Repls) != 0 || len(f.Triggers) != 0 {
			t.Fatalf("precondition: %s should be printed vanilla apart from its SP, face = %+v", tc.card, f)
		}
		found := false
		for _, a := range f.Abilities {
			if a.CompiledKind() != cards.SAKindSpell || a.APIKind() != cards.APIPermanentCreature {
				continue
			}
			found = true
			// Precondition: the cast really does carry a non-mana cost, so
			// the exclusion below is caused by the cost read.
			if costOnlyMana(a.ParamStr(cards.PKCost)) {
				t.Fatalf("precondition: %s cast cost %q should not be mana-only", tc.card, a.ParamStr(cards.PKCost))
			}
		}
		if !found {
			t.Fatalf("precondition: %s should carry an SP$ PermanentCreature entry, face = %+v", tc.card, f)
		}
		if MatchesObjectCtx(g, "Creature.NoAbilities", o, SpecContext{You: 0}) {
			t.Errorf("Creature.NoAbilities must NOT match %s (its cast demands a non-mana cost)", tc.card)
		}
		// And the negation the blocker clause uses must include it.
		if !MatchesObjectCtx(g, "Creature.!NoAbilities", o, SpecContext{You: 0}) {
			t.Errorf("Creature.!NoAbilities must match %s", tc.card)
		}
	}
}

// TestCostOnlyManaClass pins costOnlyMana against every shape the SP read can
// see: pure mana (including empty = the printed ManaCost), a non-mana part,
// an unknown head the parser prices as generic (ChooseCreatureType), and the
// two non-mana heads HasNonMana itself does not list (CollectEvidence,
// RollDice). All non-mana answers are false; the unknown head must fail
// closed so a token this build does not model can never make an ability read
// as absent.
func TestCostOnlyManaClass(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", true},             // no Cost$ line: paid with the printed ManaCost
		{"1 G", true},          // Grizzly Bears
		{"3 U", true},          // plain mana
		{"X B", true},          // {X} is mana
		{"1 B B", true},        // Mardu Outrider's mana half
		{"Waterbend<4>", true}, // a mana help, not an ability
		{"3 U ExileFromGrave<1/Creature>", false},
		{"1 B B Discard<1/Card>", false},
		{"2 W tapXType<2/Artifact;Creature;Land/x>", false},
		{"4 W W Exile<1/Creature>", false},
		{"2 B B Sac<1/Creature>", false},
		{"2 B G PayLife<X>", false},
		{"2 G ChooseCreatureType<1>", false}, // Unknown head, priced as generic
		{"CollectEvidence<X> 2 B G", false},  // non-mana, absent from HasNonMana
		{"RollDice<1/6/X>", false},           // non-mana, absent from HasNonMana
	}
	for _, tc := range cases {
		if got := costOnlyMana(tc.raw); got != tc.want {
			t.Errorf("costOnlyMana(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}
