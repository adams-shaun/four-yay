package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestConditionalDestinationBotTakesAlternate pins the bot's answer to
// effects' non-MANDATORY DestAltSVar$ ask (Green Sun's Twilight at X>=5): it
// takes the alternate destination, the direction of the R-9 no-host stand-in,
// selected by Mode rather than index, and the answer validates.
func TestConditionalDestinationBotTakesAlternate(t *testing.T) {
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
		ResumeKind: "changezone_dest_alt", Prompt: "Choose where to put the selected cards",
		Options: []decision.Option{
			{Index: 0, Kind: "destination", Label: "hand", Mode: "primary"},
			{Index: 1, Kind: "destination", Label: "battlefield", Mode: "alternate"},
		}}
	in := Decide(Board{}, d, rng(1))
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Mode != "alternate" {
		t.Fatalf("bot choices = %v, want the alternate destination", in.Choices)
	}
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %+v rejected by Decision.Validate: %v", in, err)
	}
}
