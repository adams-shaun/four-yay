package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaxTotalTargetCMCMixedPlayerCreatureMinimum verifies that a player is a
// legal zero-mana-value member of a mandatory target set. The feasibility
// census must not reject a mixed Player,Creature declaration just because no
// object target is present.
func TestMaxTotalTargetCMCMixedPlayerCreatureMinimum(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	sa := setPropSA("Player,Creature", map[string]string{
		"TargetMin":         "1",
		"MaxTotalTargetCMC": "3",
	})
	candidates := e.legalTargetCandidates(0, 0, 0, sa)
	players := 0
	for _, candidate := range candidates {
		if candidate.kind == "player" {
			players++
		}
	}
	if players == 0 {
		t.Fatalf("precondition: mixed Player,Creature spec yielded no eligible player candidates: %+v", candidates)
	}
	if !e.targetSAAvailable(0, 0, 0, sa, 0, false) {
		t.Fatal("mandatory mixed Player,Creature target set was judged infeasible under MaxTotalTargetCMC$ 3")
	}
}

// TestMaxTotalTargetCMCCorpusCensus keeps the mechanism class visible and
// names every current Forge script carrying the parameter.
func TestMaxTotalTargetCMCCorpusCensus(t *testing.T) {
	t.Parallel()
	var files []string
	err := filepath.WalkDir("../.cards/cardsfolder", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "MaxTotalTargetCMC$") {
			files = append(files, filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 14 {
		t.Fatalf("MaxTotalTargetCMC$ corpus carriers = %d, want 14: %v", len(files), files)
	}
	t.Logf("MaxTotalTargetCMC$ carriers (%d): %s", len(files), strings.Join(files, ", "))
}
