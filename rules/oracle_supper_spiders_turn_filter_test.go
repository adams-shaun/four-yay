package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSupperForSpidersReturnsCreaturePutIntoGraveyardThisTurn runs the
// Oracle-derived scenario on the real corpus card. The qualifying Bears must
// both still be in p1's graveyard after Bolt resolves and have a current-turn
// battlefield-to-graveyard entry before Supper resolves; the scenario runner
// checks those observable preconditions through its final zone assertions.
func TestSupperForSpidersReturnsCreaturePutIntoGraveyardThisTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, ok := oracleScenarioByName(t, "Supper for Spiders", "reanimates-a-creature-that-died-this-turn")
	if !ok {
		t.Fatal("scenario reanimates-a-creature-that-died-this-turn not found")
	}
	fails, transcript, _ := runOracleScenario(reg, sc)
	if len(fails) > 0 {
		t.Errorf("Supper for Spiders scenario diverged:\n  %s\n  transcript:\n    %s",
			joinLines(fails), joinLines(transcript))
	}
}
