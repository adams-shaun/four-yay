package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestBotAnswersSameColorRevealOnSharedClass pins the bot arm of the
// Reveal<2/SameColor> cost (Illuminated Folio, ticket
// agent-20260928T173851Z-05a69598) on the ONE home for the answer rule: the
// decision the engine poses is a Min 2 / Max 2 KChoose with
// SetPropMode SetPropShared over each candidate's DERIVED colour tokens
// (rules' revealCostAsk). If the bot's answer is not repaired onto a shared
// colour class, SetPropShared's running intersection refuses the top-up and
// the deterministic bot re-derives the same rejected answer forever -- the
// livelock the one-home rule exists to prevent. The anchor trap is real here:
// the first offered option is a lone RED card, so a naive append can never
// reach Min over green.
func TestBotAnswersSameColorRevealOnSharedClass(t *testing.T) {
	// Offer order matters: the red card is first, exactly the seed
	// setPropSharedChoices must re-anchor away from.
	d := &decision.Decision{
		Kind:        decision.KChoose,
		Min:         2,
		Max:         2,
		SetPropMode: decision.SetPropShared,
		Options: []decision.Option{
			{Index: 0, Kind: "revealcost", Obj: 11, SetProps: []string{"R"}},
			{Index: 1, Kind: "revealcost", Obj: 12, SetProps: []string{"G"}},
			{Index: 2, Kind: "revealcost", Obj: 13, SetProps: []string{"G"}},
		},
	}
	if decision.SetPropCapacity(decision.SetPropShared, [][]string{{"R"}, {"G"}, {"G"}}) < 2 {
		t.Fatal("precondition: a green pair must exist so the constraint has a legal answer")
	}
	if decision.SetPropAdmits(decision.SetPropShared, []string{"R"}, []string{"G"}) {
		t.Fatal("precondition: red and green must NOT share a token")
	}
	in := Decide(Board{}, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %+v rejected by Decision.Validate (bot would livelock): %v", in.Choices, err)
	}
	if len(in.Choices) != 2 {
		t.Fatalf("bot picked %v, want exactly 2 cards", in.Choices)
	}
	// The answer must be the two GREEN cards: a SetPropShared answer is legal
	// only if every pick shares one colour, and only green has two offers.
	for _, c := range in.Choices {
		if got := d.Options[c].SetProps; len(got) != 1 || got[0] != "G" {
			t.Fatalf("bot picked option %d with props %v, want a G card", c, got)
		}
	}
}
