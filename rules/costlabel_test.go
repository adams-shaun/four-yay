// The player-facing cost-text ticket (costlabel1). Feedback
// fb-20260921T022723Z-96e6db91 reported that a decision prompt showed the
// engine's own cost token ("pay Sac<1/Creature.Other/another creature>?").
// rules/mana.go's costPhrase renders a parsed cost as prose for a prompt or
// option label -- using Forge's embedded "/description" verbatim when the
// corpus carries one and a synthesized fallback otherwise -- while
// formatCost (raw Forge notation) is retained for the machine wire field
// decision.Option.Cost.
//
// The Sephiroth leaf drives the real compiled corpus card end to end; the
// fallback leaf exercises heads the description-less corpus tokens use.

package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSephirothCostPrompt is the reported-case leaf: the real corpus
// Sephiroth, Fabled SOLDIER's enter/attack trigger body carries
// `Cost$ Sac<1/Creature.Other/another creature>`; reaching the trigger-cost
// window must show the sacrifice in prose, not the raw token.
func TestSephirothCostPrompt(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, _ := realCardEngine(t, reg, 47, "Sephiroth, Fabled SOLDIER", "Grizzly Bears")

	// The ETB trigger's pay/decline window is the reported decision. Drain
	// any priority passes until it is pending.
	d := passUntilNonPriority(t, e, 40)
	if d == nil {
		t.Fatal("the Sephiroth enter trigger never produced a decision")
	}
	pay := -1
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_pay" {
			pay = o.Index
		}
	}

	wantPrompt := "Sephiroth, Fabled SOLDIER — sacrifice another creature?"
	if d.Prompt != wantPrompt {
		t.Fatalf("cost window prompt = %q, want %q", d.Prompt, wantPrompt)
	}
	for _, o := range d.Options {
		if strings.ContainsAny(o.Label, "<>") || strings.Contains(o.Label, "Sac<") {
			t.Fatalf("option %q leaks raw cost syntax", o.Label)
		}
	}
	if strings.ContainsAny(d.Prompt, "<>") || strings.Contains(d.Prompt, "Sac<") {
		t.Fatalf("prompt %q leaks raw cost syntax", d.Prompt)
	}
	// The pay option is the capitalized prose phrase, not "Pay Sac<...>".
	if pay < 0 {
		t.Fatalf("the sacrifice cost was not offered as payable: %+v", d.Options)
	}
	if got := d.Options[pay].Label; got != "Sacrifice another creature" {
		t.Fatalf("pay option label = %q, want %q", got, "Sacrifice another creature")
	}
}

// TestCostPhraseFallbacks pins the synthesized (no-description) and
// description-bearing shapes of costPhrase across heads, so the fallback is
// shown to be whole-vocabulary rather than an accident of the reported card.
func TestCostPhraseFallbacks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		// The reported case: the embedded /description wins verbatim.
		{"Sac<1/Creature.Other/another creature>", "sacrifice another creature"},
		{"Sac<2/Creature>", "sacrifice 2 creature"},
		{"Discard<2/Card>", "discard 2 card"},
		{"Discard<1/Card>", "discard 1 card"},
		{"PayLife<2>", "pay 2 life"},
		{"PayEnergy<3>", "pay 3 energy"},
		{"SubCounter<2/P1P1>", "remove 2 P1P1 counters"},
		{"Exile<1/Creature>", "exile 1 creature"},
		{"ExileFromGrave<1/Card>", "exile 1 card"},
		{"Blight<2>", "blight 2"},
		{"Forage", "forage"},
		{"Return<1/CARDNAME>", "return 1 permanent to its owner's hand"},
		// Mana keeps Forge notation; non-mana parts join with "and".
		{"2 B", "pay 2 B"},
		{"2 B Sac<1/Creature>", "pay 2 B and sacrifice 1 creature"},
		{"U", "pay U"},
		{"T Sac<1/Creature>", "tap this permanent and sacrifice 1 creature"},
		// A Mandatory-prefixed token renders its remaining content.
		{"Mandatory Sac<1/Creature>", "sacrifice 1 creature"},
		// No cost renders empty (a caller must not emit "pay ?").
		{"no cost", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := costPhrase(ParseCost(tc.raw))
		if got != tc.want {
			t.Errorf("costPhrase(%q) = %q, want %q", tc.raw, got, tc.want)
		}
		if strings.ContainsAny(got, "<>") {
			t.Errorf("costPhrase(%q) = %q leaks raw syntax", tc.raw, got)
		}
	}

	// The cumulative-upkeep action vocabulary parseCost does not model gets
	// its own prose renderer; every real corpus kind must render without raw
	// syntax. Forms are the measured corpus shapes.
	for _, raw := range []string{
		"AddMana<1/R>", "GainLife<1/Player.Opponent>", "FlipCoin<1>",
		"ExileFromTop<1/Card>", "PutCardToLibFromSameGrave<2/-1/Card>",
		"Sac<1/Creature>", "Discard<1/Card>", "Draw<1/You>",
		"AddCounter<1/P1P1/Creature.OppCtrl>",
		"GainControl<1/Land.YouDontCtrl>",
	} {
		a, ok := parseCumulativeAction(raw)
		if !ok {
			t.Fatalf("parseCumulativeAction(%q) failed", raw)
		}
		got := cumulativeCostLabel(raw, a, ParseCost(raw))
		if got == "" || strings.ContainsAny(got, "<>") {
			t.Errorf("cumulativeCostLabel(%q) = %q, want non-empty prose", raw, got)
		}
	}
}
