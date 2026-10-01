package searchbench

import (
	"bufio"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
)

// SourceFilter is the public-data eligibility gate used before native state
// reconstruction. It intentionally does not guess at missing player metadata.
type SourceFilter struct {
	MinimumGameWinRate float64
	MinimumGames       int
}

func DefaultSourceFilter() SourceFilter {
	return SourceFilter{MinimumGameWinRate: .60, MinimumGames: 100}
}

type SourceAudit struct {
	Rows, FDNPremierRows, EligibleRows, MalformedEligibilityRows int
}

// AuditCSV streams a 17lands replay CSV (plain or gzip by magic bytes) and
// counts the rows eligible for the fixed FDN human-decision benchmark. It is
// deliberately a source audit only: an eligible aggregate row is not yet a
// reconstructible Gorge decision.
func AuditCSV(path string, f SourceFilter) (SourceAudit, error) {
	if f.MinimumGameWinRate < 0 || f.MinimumGameWinRate > 1 || f.MinimumGames < 1 {
		return SourceAudit{}, fmt.Errorf("searchbench: invalid source filter")
	}
	in, close, err := sourceReader(path)
	if err != nil {
		return SourceAudit{}, err
	}
	defer close()
	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return SourceAudit{}, fmt.Errorf("searchbench: reading source header: %w", err)
	}
	ix := make(map[string]int, len(head))
	for i := range head {
		ix[head[i]] = i
	}
	required := []string{"expansion", "event_type", "user_game_win_rate_bucket", "user_n_games_bucket"}
	for _, name := range required {
		if _, ok := ix[name]; !ok {
			return SourceAudit{}, fmt.Errorf("searchbench: source has no %q column", name)
		}
	}
	var out SourceAudit
	for {
		row, err := r.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return SourceAudit{}, fmt.Errorf("searchbench: reading source row %d: %w", out.Rows+2, err)
		}
		out.Rows++
		if len(row) != len(head) || row[ix["expansion"]] != "FDN" || row[ix["event_type"]] != "PremierDraft" {
			continue
		}
		out.FDNPremierRows++
		wr, wrErr := strconv.ParseFloat(row[ix["user_game_win_rate_bucket"]], 64)
		games, gamesErr := strconv.Atoi(row[ix["user_n_games_bucket"]])
		if wrErr != nil || gamesErr != nil {
			out.MalformedEligibilityRows++
			continue
		}
		if wr >= f.MinimumGameWinRate && games >= f.MinimumGames {
			out.EligibleRows++
		}
	}
}

func sourceReader(path string) (io.Reader, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	magic, err := r.Peek(2)
	if err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(r)
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		return zr, func() error {
			e1 := zr.Close()
			e2 := f.Close()
			if e1 != nil {
				return e1
			}
			return e2
		}, nil
	}
	return r, f.Close, nil
}
