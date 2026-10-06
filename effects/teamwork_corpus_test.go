package effects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Pins every corpus script mentioning Teamwork and its keyword/count spellings.
func TestTeamworkCorpusCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var files, keywordFiles, countFiles []string
	for _, card := range reg.AllCards() {
		if card == nil || card.Path == "" {
			continue
		}
		data, err := os.ReadFile(card.Path)
		if err != nil {
			t.Fatalf("read corpus script %q: %v", card.Path, err)
		}
		text := string(data)
		if !strings.Contains(text, "Teamwork") {
			continue
		}
		name := filepath.Base(card.Path)
		files = append(files, name)
		if strings.Contains(text, "K:Teamwork:") {
			keywordFiles = append(keywordFiles, name)
		}
		if strings.Contains(text, "Count$Teamwork") {
			countFiles = append(countFiles, name)
		}
	}
	for _, names := range [][]string{files, keywordFiles, countFiles} {
		sort.Strings(names)
	}
	wantFiles := []string{"agent_maria_hill.txt", "allied_teamwork.txt", "atlantis_attacks.txt", "beast_mode.txt", "crossover_collaboration.txt", "cruel_alliance.txt", "earths_mightiest_heroes.txt", "go_nuts.txt", "helicarrier_strike.txt", "heroic_teamwork.txt", "hulk_smash.txt", "murdocks_crusade.txt", "quantum_reduction.txt", "repulsor_blast.txt", "team_tactics.txt", "timeline_inquiry.txt", "too_evil_to_stay_dead.txt", "virtual_assistant.txt", "we_say_thee_nay.txt", "widows_bite.txt"}
	wantKeywords := []string{"atlantis_attacks.txt", "beast_mode.txt", "crossover_collaboration.txt", "cruel_alliance.txt", "earths_mightiest_heroes.txt", "go_nuts.txt", "helicarrier_strike.txt", "heroic_teamwork.txt", "hulk_smash.txt", "murdocks_crusade.txt", "quantum_reduction.txt", "repulsor_blast.txt", "team_tactics.txt", "timeline_inquiry.txt", "too_evil_to_stay_dead.txt", "we_say_thee_nay.txt", "widows_bite.txt"}
	wantCounts := []string{"atlantis_attacks.txt", "cruel_alliance.txt", "earths_mightiest_heroes.txt", "go_nuts.txt", "helicarrier_strike.txt", "hulk_smash.txt", "murdocks_crusade.txt", "repulsor_blast.txt", "too_evil_to_stay_dead.txt", "we_say_thee_nay.txt", "widows_bite.txt"}
	for _, check := range []struct {
		name      string
		got, want []string
	}{{"Teamwork corpus", files, wantFiles}, {"K:Teamwork", keywordFiles, wantKeywords}, {"Count$Teamwork", countFiles, wantCounts}} {
		if strings.Join(check.got, "\n") != strings.Join(check.want, "\n") {
			t.Errorf("%s files (%d) = %v, want (%d) %v", check.name, len(check.got), check.got, len(check.want), check.want)
		}
	}
}
