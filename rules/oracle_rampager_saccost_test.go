package rules

// Focused proof for the triggered-ability Cost$ sacrifice LKI binding
// (Rhovanion Rampager, HOB). The card's attack trigger parks an ability whose
// Cost$ Sac<1/Creature.Other> is settled by rules/cumulative.go's triggered
// cost window (settleTriggeredMandatory). Before the fix that settle emitted
// the sacrifice but never captured the sacrificed permanent's LKI onto the
// stack object, so the resumed body's `CounterNum$ X` with
// `SVar:X:Sacrificed$CardPower` read an empty list and placed zero +1/+1
// counters.
//
// The assertion is the Oracle-derived scenario itself: this runs the real
// corpus card through the real scenario runner, so it fails on the same
// observable divergence (P/T 3/2, want 5/4) the known-divergent row recorded,
// never on a hand-built stand-in for it.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// oracleScenarioByName returns one scenario from the per-card Oracle scenario
// files by card and scenario name. The three hand-oracle fix tickets share it
// so each can assert its card's real scenario directly.
func oracleScenarioByName(t *testing.T, card, name string) (oracleScenario, bool) {
	t.Helper()
	for _, f := range loadOracleFiles(t) {
		if f.Card != card {
			continue
		}
		for _, sc := range f.Scenarios {
			if sc.Name == name {
				return sc, true
			}
		}
	}
	return oracleScenario{}, false
}

// joinLines renders a string slice one entry per line for a failure message.
func joinLines(lines []string) string {
	return strings.Join(lines, "\n  ")
}

// TestRhovanionRampagerSacrificeCostBindsLKI runs the attack-sacrifice
// scenario against the real corpus card and asserts every Oracle-derived
// expectation holds. It is the failing-before / passing-after proof for the
// settleTriggeredMandatory LKI capture.
func TestRhovanionRampagerSacrificeCostBindsLKI(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, ok := oracleScenarioByName(t, "Rhovanion Rampager", "attack-sacrifice-pumps-by-sacrificed-power")
	if !ok {
		t.Fatal("scenario attack-sacrifice-pumps-by-sacrificed-power not found")
	}
	fails, transcript, _ := runOracleScenario(reg, sc)
	if len(fails) > 0 {
		t.Errorf("Rhovanion Rampager attack-sacrifice scenario diverged:\n  %s\n  transcript:\n    %s",
			joinLines(fails), joinLines(transcript))
	}
}
