package rules

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAcceptanceGameMaterializesOnlyItsCards is the imaged registry's
// ratchet (pointer-free corpus spec, S4): one acceptance game, on a
// registry no other test has touched, materializes its decks' cards and
// at most a handful more (a named-card mechanic that fired), never the
// corpus. A runtime path that reaches AllCards() or walks the universe's
// cards fails here loudly.
func TestAcceptanceGameMaterializesOnlyItsCards(t *testing.T) {
	testutil.CorpusRegistry(t) // Skips when there is no corpus
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := cards.OpenCorpus(filepath.Join(strings.TrimSpace(string(out)), ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	if reg.MaterializedCount() != 0 {
		t.Fatalf("a fresh imaged registry has %d cards materialized", reg.MaterializedCount())
	}
	names, decks, err := testutil.AcceptanceDecks(reg, 4)
	if err != nil {
		t.Fatal(err)
	}
	distinct := map[*cards.Card]bool{}
	for _, d := range decks {
		for _, c := range d {
			distinct[c] = true
		}
	}
	seated := reg.MaterializedCount()
	if seated > len(distinct)+8 {
		t.Fatalf("resolving %d distinct deck cards materialized %d", len(distinct), seated)
	}
	if _, _, err := PlayAcceptance(AcceptanceConfig(reg, names, decks), acceptanceTestBot, nil); err != nil {
		t.Fatal(err)
	}
	// The allowance covers named-card mechanics (a chosen-name copy, a
	// NamedCard mint) materializing the one card they name.
	const namedAllowance = 16
	got := reg.MaterializedCount()
	t.Logf("materialized: %d after seating %d distinct deck cards, %d after the game (corpus %d)", seated, len(distinct), got, reg.Len())
	if got > seated+namedAllowance {
		t.Fatalf("the game materialized %d cards beyond its decks (allowance %d): a runtime path is walking the corpus", got-seated, namedAllowance)
	}
}
