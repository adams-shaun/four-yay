package oraclegen

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The two answer-shape derivations this ticket adds are driven by raw Forge
// markers, not by a per-card table: DividedAsYouChoose$ makes a DealDamage
// ask a "damage_split" KChoose (one index per damage), and Produced$ Combo Any
// with Amount$ > 1 makes a Mana ask a "mana_color" allocation (one unit per
// option). A FORGE_REF bump that adds or removes a carrier must fail here
// before it silently changes what the generator emits, in the same spirit as
// fixtureCensus and the template oracleCorpusCarriers ratchet.
//
// Measured 2026-10-05 at the corpus pin the brief names. The whole-corpus
// counts match /usr/bin/grep -rlF over .cards/cardsfolder (132 and 45); the
// audit-set arm counts the carriers among censusSets' manifests, so a set
// rotation or a new carrier names itself.
const (
	dividedAsYouChooseTotal = 132
	comboAnyTotal           = 45
	dividedAsYouChooseAudit = 17
	comboAnyAudit           = 7
)

// answerShapeCarriers returns, for one raw marker, the whole-corpus file count
// and the sorted catalogue names of carriers whose face is in censusSets.
func answerShapeCarriers(t *testing.T, marker string) (int, []string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", ".cards", "cardsfolder", "*", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no corpus under .cards/cardsfolder (is the .cards symlink present?)")
	}
	manifests := censusManifestNames(t)
	total := 0
	var audit []string
	for _, fp := range files {
		b, err := os.ReadFile(fp)
		if err != nil || !strings.Contains(string(b), marker) {
			continue
		}
		total++
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "Name:") {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(line[len("Name:"):]))
			for _, names := range manifests {
				if names[name] {
					audit = append(audit, name)
				}
			}
		}
	}
	sort.Strings(audit)
	return total, audit
}

// TestAnswerShapeCensus pins the corpus carriers of the two raw markers whose
// engine decisions the new derivations script. A new carrier (a FORGE_REF
// bump) or a corpus pin change moves the count and fails, naming the marker.
func TestAnswerShapeCensus(t *testing.T) {
	tests := []struct {
		name   string
		marker string
		total  int
		audit  int
	}{
		{"DividedAsYouChoose", "DividedAsYouChoose$", dividedAsYouChooseTotal, dividedAsYouChooseAudit},
		{"Combo Any", "Produced$ Combo Any", comboAnyTotal, comboAnyAudit},
	}
	for _, tc := range tests {
		total, audit := answerShapeCarriers(t, tc.marker)
		if total != tc.total {
			t.Errorf("%s carriers: %d, want %d", tc.name, total, tc.total)
		}
		if len(audit) != tc.audit {
			t.Errorf("%s audit-set carriers: %d %v, want %d", tc.name, len(audit), audit, tc.audit)
		}
	}
}
