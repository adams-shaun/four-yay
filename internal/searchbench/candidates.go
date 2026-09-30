package searchbench

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// SourceCandidate is an auditable pre-reconstruction candidate. It is not a
// benchmark item: Gorge must still rebuild its root and resolve the label.
type SourceCandidate struct {
	GameID, DraftID string
	Turn            int
	Type            DecisionType
	HumanAct        bool
	Evidence        []string
}

type CandidateAudit struct {
	EligibleGames, Candidates int
	ByType                    [4]int
}

// CandidatesCSV extracts the four public 17lands label shapes deterministically.
// Aggregate rows cannot establish engine legality, so consumers must pass each
// result through native reconstruction before it enters a Manifest.
func CandidatesCSV(path string, f SourceFilter, maximumPerGame int) ([]SourceCandidate, CandidateAudit, error) {
	if maximumPerGame < 1 {
		return nil, CandidateAudit{}, fmt.Errorf("searchbench: maximum items per game %d", maximumPerGame)
	}
	in, close, err := sourceReader(path)
	if err != nil {
		return nil, CandidateAudit{}, err
	}
	defer close()
	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return nil, CandidateAudit{}, err
	}
	ix := headerIndex(head)
	required := []string{"expansion", "event_type", "draft_id", "build_index", "match_number", "game_number", "user_game_win_rate_bucket", "user_n_games_bucket"}
	for _, name := range required {
		if _, ok := ix[name]; !ok {
			return nil, CandidateAudit{}, fmt.Errorf("searchbench: source has no %q column", name)
		}
	}
	var all []SourceCandidate
	var audit CandidateAudit
	for rowNo := 2; ; rowNo++ {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, audit, fmt.Errorf("searchbench: reading source row %d: %w", rowNo, err)
		}
		if len(row) != len(head) || row[ix["expansion"]] != "FDN" || row[ix["event_type"]] != "PremierDraft" || !eligible(row, ix, f) {
			continue
		}
		audit.EligibleGames++
		game := strings.Join([]string{row[ix["draft_id"]], row[ix["build_index"]], row[ix["match_number"]], row[ix["game_number"]]}, ":")
		draft := row[ix["draft_id"]]
		var gameCandidates []SourceCandidate
		for turn := 3; turn <= 12; turn++ {
			acts := turnValues(row, ix, turn, "user", []string{"creatures_cast", "non_creatures_cast", "user_instants_sorceries_cast", "user_abilities"})
			if len(acts) > 0 {
				gameCandidates = append(gameCandidates, SourceCandidate{GameID: game, DraftID: draft, Turn: turn, Type: DecisionSpell, HumanAct: true, Evidence: acts})
			} else {
				gameCandidates = append(gameCandidates, SourceCandidate{GameID: game, DraftID: draft, Turn: turn, Type: DecisionHold})
			}
			attacks := turnValues(row, ix, turn, "user", []string{"creatures_attacked"})
			gameCandidates = append(gameCandidates, SourceCandidate{GameID: game, DraftID: draft, Turn: turn, Type: DecisionAttack, HumanAct: len(attacks) > 0, Evidence: attacks})
			blocks := turnValues(row, ix, turn, "oppo", []string{"creatures_blocking"})
			gameCandidates = append(gameCandidates, SourceCandidate{GameID: game, DraftID: draft, Turn: turn, Type: DecisionBlock, HumanAct: len(blocks) > 0, Evidence: blocks})
		}
		sort.SliceStable(gameCandidates, func(i, j int) bool { return candidateKey(gameCandidates[i]) < candidateKey(gameCandidates[j]) })
		if len(gameCandidates) > maximumPerGame {
			gameCandidates = gameCandidates[:maximumPerGame]
		}
		for _, c := range gameCandidates {
			audit.Candidates++
			audit.ByType[typeIndex(c.Type)]++
			all = append(all, c)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return candidateKey(all[i]) < candidateKey(all[j]) })
	return all, audit, nil
}

func headerIndex(head []string) map[string]int {
	out := make(map[string]int, len(head))
	for i := range head {
		out[head[i]] = i
	}
	return out
}

func eligible(row []string, ix map[string]int, f SourceFilter) bool {
	wr, e1 := parseFloat(row[ix["user_game_win_rate_bucket"]])
	games, e2 := parseInt(row[ix["user_n_games_bucket"]])
	return e1 == nil && e2 == nil && wr >= f.MinimumGameWinRate && games >= f.MinimumGames
}

func parseFloat(v string) (float64, error) { return strconv.ParseFloat(v, 64) }
func parseInt(v string) (int, error)       { return strconv.Atoi(v) }

func turnValues(row []string, ix map[string]int, turn int, side string, fields []string) []string {
	var out []string
	for _, field := range fields {
		if i, ok := ix[fmt.Sprintf("%s_turn_%d_%s", side, turn, field)]; ok && row[i] != "" {
			out = append(out, row[i])
		}
	}
	return out
}

func candidateKey(c SourceCandidate) string {
	s := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", c.GameID, c.Turn, c.Type)))
	return string(s[:])
}
