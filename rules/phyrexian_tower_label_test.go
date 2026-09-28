package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Phyrexian Tower's stage-1 wheel (fb-20260923T033148Z-877b8f8f,
// fb-20260924T180813Z-bbe4fd8f): "{T}: Add {C}" and "{T}, Sacrifice a
// creature: Add {B}{B}" rendered as "Add C" and "Add B", so the player who
// wanted to sacrifice a creature saw no option that said so, and the {B}{B}
// read as one pip. The paid option must name its sacrifice and both pips.
const phyrexianTowerSrc = "Name:Phyrexian Tower\nTypes:Legendary Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n" +
	"A:AB$ Mana | Cost$ T Sac<1/Creature> | Produced$ B | Amount$ 2 | SpellDescription$ Add {B}{B}.\n" +
	"Oracle:{T}: Add {C}.\\n{T}, Sacrifice a creature: Add {B}{B}.\n"

func TestPhyrexianTowerWheelNamesTheSacrificeAndBothPips(t *testing.T) {
	t.Parallel()
	e, _, tower := manaSourceEngine(t, phyrexianTowerSrc)
	bear := onBoard(t, e, 0, "Name:Tower Fodder\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.pending = nil
	e.Advance()
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: the sacrifice candidate must be seat 0's battlefield creature: %+v", o)
	}
	activateMana(t, e, tower)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("stage-1 decision = %+v, want the two-ability Phyrexian Tower wheel", d)
	}
	want := []string{"Add C", "Sacrifice a creature: Add BB"}
	for i, w := range want {
		if o := d.Options[i]; o.Label != w || o.Kind != "mana" || o.Obj != tower || o.Ability != i {
			t.Fatalf("stage-1 option %d = %+v, want %q (ability %d of %d)", i, o, w, i, tower)
		}
	}
	// The label is display only: the sacrifice option still resolves the
	// paid ability -- its follow-up asks which creature, and the answer lands
	// {B}{B}.
	submitChoices(t, e, 1)
	for guard := 0; guard < 4; guard++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "sacrifice" && o.Obj == bear {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("unexpected follow-up after the sacrifice option: %+v", d)
		}
		submitChoices(t, e, idx)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 2 {
		t.Fatalf("pool black = %d after the sacrifice activation, want 2: %+v", got, e.G.Players[0].Pool)
	}
	if got := e.G.Obj(bear).Zone; got != state.ZGraveyard {
		t.Fatalf("sacrificed creature zone = %s, want graveyard", got)
	}
}

// TestManaAbilityLabelCostAndAmount is the formatter table for the two
// riders: a cost beyond the shared {T} is named in front, a literal Amount$
// repeats a single pip, and every plain {T} shape stays byte-identical.
func TestManaAbilityLabelCostAndAmount(t *testing.T) {
	cases := []struct{ cost, produced, amount, want string }{
		{"T", "C", "", "Add C"},
		{"", "G", "", "Add G"},
		{"T", "B", "2", "Add BB"},
		{"T", "C", "3", "Add CCC"},
		{"T Sac<1/Creature>", "B", "2", "Sacrifice a creature: Add BB"},
		{"T PayLife<1>", "Combo B R", "", "Pay 1 life: Add B or R"},
		{"T ExileFromHand<1/Card>", "B", "", "Exile a card from your hand: Add B"},
		{"1 T", "C", "2", "Pay 1: Add CC"},
		{"T", "B", "X", "Add B"},   // non-literal amount keeps one pip
		{"T", "B", "0", "Add B"},   // non-positive literal keeps one pip
		{"T", "RR", "2", "Add RR"}, // a multi-pip token is left as written
		// An Any ability's literal amount is named so two abilities of one
		// source that differ only in amount are distinguishable on the wheel
		// (task mana-wheel-amount-labels). An absent, 1 or non-literal amount
		// keeps the fail-safe "Add any color".
		{"T", "Any", "2", "Add two mana of any one color"},
		{"T", "Combo Any", "3", "Add three mana in any combination of colors"},
		{"T", "Any", "1", "Add any color"},
		{"T", "Any", "X", "Add any color"},
	}
	for _, c := range cases {
		ma := &cards.SA{Kind: "AB", API: "Mana", Params: map[string]string{"Cost": c.cost, "Produced": c.produced}}
		if c.amount != "" {
			ma.Params["Amount"] = c.amount
		}
		if got := manaAbilityLabel(ma, ""); got != c.want {
			t.Errorf("manaAbilityLabel(Cost$ %q Produced$ %q Amount$ %q) = %q, want %q",
				c.cost, c.produced, c.amount, got, c.want)
		}
	}
}
