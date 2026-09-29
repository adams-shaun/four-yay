package rules

// Task prepare1 (CR 722, "Preparation Cards"): the prepared designation and
// the exile copy of a permanent's prepare spell. The designated carrier
// family -- every corpus script with an `Attributes$ Prepared` line -- is
// larger than any one set, so this file also pins the corpus census so a
// future FORGE_REF bump that adds or drops a carrier is visible instead of
// silently widening or narrowing the mechanic.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareCensusCountsPreparedAttributeCarriers is the census half of the
// brief: it names the corpus population the Prepared fold serves. `Attributes$
// Prepared` is the raw Forge spelling both the ETBReplacement grant and the
// unprepare effect use; measured over the pinned Card-Forge corpus it is 56
// files (the brief's figure held). Elite Interceptor is and remains one
// ordinary member of this class -- the engine special-cases no card.
func TestPrepareCensusCountsPreparedAttributeCarriers(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", ".cards", "cardsfolder")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("corpus absent (%v): prepared census needs .cards/cardsfolder", err)
	}
	var carriers []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".txt") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "Attributes$ Prepared") {
			carriers = append(carriers, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	// The pinned corpus (FORGE_REF) is the authority; a mismatch means the
	// corpus moved and the figure in the brief needs re-measuring, not that
	// the mechanic is wrong.
	const want = 56
	if len(carriers) != want {
		t.Errorf("prepared census: %d corpus files carry `Attributes$ Prepared`, want %d (FORGE_REF moved?): %v",
			len(carriers), want, carriers)
	}
	// The designated audit carrier must itself be in the population and be a
	// real preparation card (a prepare spell face), or the census above could
	// be counting an unrelated family while the mechanic under test is absent.
	interceptor := filepath.Join(dir, "e", "elite_interceptor_rejoinder.txt")
	found, isPrepare := false, false
	for _, c := range carriers {
		if c == interceptor {
			found = true
		}
	}
	if !found {
		t.Errorf("prepared census: Elite Interceptor (%s) is not among the %d carriers", interceptor, len(carriers))
	}
	if b, err := os.ReadFile(interceptor); err == nil {
		isPrepare = strings.Contains(string(b), "AlternateMode:Prepare")
	}
	if !found || !isPrepare {
		t.Errorf("prepared census precondition: carrier is a preparation card = %v, found = %v", isPrepare, found)
	}
}
