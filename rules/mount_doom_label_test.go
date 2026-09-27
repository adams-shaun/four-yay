package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Mount Doom's single mana ability ("{T}, Pay 1 life: Add {B} or {R}")
// produces a Combo colour choice, which the stage-1 wheel flattens into one
// option per colour. Before this fix the flatten path wrote only the
// production ("Add B" / "Add R"), so the player picking a colour never saw
// the 1 life the ability charges -- the cost prefix was applied to the
// single-option label but not to the flattened per-colour options
// (fb-20260924T180813Z-bbe4fd8f). Each flattened option must name the life.
const mountDoomSrc = "Name:Mount Doom\nManaCost:no cost\nTypes:Legendary Land\n" +
	"A:AB$ Mana | Cost$ T PayLife<1> | Produced$ Combo B R | SpellDescription$ Add {B} or {R}.\n" +
	"Oracle:{T}, Pay 1 life: Add {B} or {R}.\n"

func TestMountDoomColorOptionsNameTheLifePayment(t *testing.T) {
	e, _, doom := manaSourceEngine(t, mountDoomSrc)
	life := lifeOf(t, e, 0)
	activateMana(t, e, doom)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Mount Doom wheel = %+v, want the two flattened colour options", d)
	}
	want := []string{"Pay 1 life: Add B", "Pay 1 life: Add R"}
	for i, w := range want {
		if o := d.Options[i]; o.Label != w || o.Kind != "mana" || o.Obj != doom || o.ManaSymbol != []string{"B", "R"}[i] {
			t.Fatalf("Mount Doom option %d = %+v, want %q and its symbol on the source", i, o, w)
		}
	}
	// Rewriting display prose must not change the selected production.
	d.Options[0].Label = "Unrelated presentation text"
	// The label is display only: picking B still pays the 1 life and lands
	// exactly one black mana.
	submitChoices(t, e, 0)
	if got := lifeOf(t, e, 0); got != life-1 {
		t.Fatalf("life after the Mount Doom activation = %d, want %d", got, life-1)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("pool black after the Mount Doom activation = %d, want 1", got)
	}
}

// TestManaAbilityCostPhraseArticleForms locks the natural-reading rule for
// the mana wheel's cost prefix across object types and counts: a single
// object reads with an article, N > 1 keeps its number, and an exile names
// its origin zone. Every case is a shape the corpus actually carries.
func TestManaAbilityCostPhraseArticleForms(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"T Sac<1/Creature>", "sacrifice a creature"},
		{"T Sac<1/Artifact>", "sacrifice an artifact"},
		{"T Sac<2/Creature>", "sacrifice 2 creature"},
		{"T Discard<1/Card>", "discard a card"},
		{"T Discard<0/Hand>", "discard your hand"},
		{"T Discard<1/Hand>", "discard your hand"},
		{"T ExileFromHand<1/Card>", "exile a card from your hand"},
		{"T ExileFromGrave<1/Card>", "exile a card from your graveyard"},
		{"T ExileFromHand<2/Card>", "exile 2 card from your hand"},
		{"T PayLife<1>", "pay 1 life"},
	}
	for _, c := range cases {
		cost := ParseCost(c.raw)
		cost.Tap = false // the prefix path clears the shared tap
		got := manaAbilityCostPhrase(cost)
		if got != c.want {
			t.Errorf("manaAbilityCostPhrase(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
