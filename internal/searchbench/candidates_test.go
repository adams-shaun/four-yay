package searchbench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidatesCSVLimitsPerGameAndClassifiesActs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.csv")
	data := "expansion,event_type,draft_id,build_index,match_number,game_number,user_game_win_rate_bucket,user_n_games_bucket,user_turn_3_creatures_cast,user_turn_3_non_creatures_cast,user_turn_3_user_instants_sorceries_cast,user_turn_3_user_abilities,user_turn_3_creatures_attacked,oppo_turn_3_creatures_blocking\n" +
		"FDN,PremierDraft,d,0,1,1,0.7,100,,,,,a,\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	rows, a, err := CandidatesCSV(path, DefaultSourceFilter(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || a.EligibleGames != 1 || a.Candidates != 2 {
		t.Fatalf("rows=%+v audit=%+v", rows, a)
	}
	for _, row := range rows {
		if row.Type != DecisionAttack && row.Type != DecisionBlock && row.Type != DecisionSpell && row.Type != DecisionHold {
			t.Fatalf("bad type %+v", row)
		}
	}
}
