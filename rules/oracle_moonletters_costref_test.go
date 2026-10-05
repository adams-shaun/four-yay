package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestUncoverTheMoonLettersCostBindsTriggeredCard runs the real Oracle
// scenario and pins the effect of its Cost$ Draw<X/You> on the actual card:
// after casting Lightning Bolt, the trigger draws one filler card, then its
// body discards two cards in the SAME resolution. The scenario's answer
// script now carries the draw and the two-pick discard decision in one
// resolve step, and this test asserts the scenario runs cleanly (no diverged
// expectation and no unconsumed answer), then checks the observable draw.
func TestUncoverTheMoonLettersCostBindsTriggeredCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, ok := oracleScenarioByName(t, "Uncover the Moon-Letters", "noncreature-spell-draws-mana-spent-then-discards-two")
	if !ok {
		t.Fatal("scenario noncreature-spell-draws-mana-spent-then-discards-two not found")
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) > 0 {
		t.Fatalf("Uncover the Moon-Letters scenario diverged:\n  %s\n  transcript:\n    %s",
			joinLines(fails), joinLines(transcript))
	}
	if run == nil || run.e == nil {
		t.Fatalf("scenario did not initialize its engine:\n  %s", joinLines(transcript))
	}
	if got := len(run.e.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("after the trigger cost draws one and the body discards two, p0 hand size = %d, want 1; transcript:\n  %s", got, joinLines(transcript))
	}
	for _, ev := range run.e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("the scenario reached an unimplemented handler, not the Cost$ draw: %q", ev.Text)
		}
	}
}
