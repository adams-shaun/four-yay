package searchbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/adams-shaun/gorge/decision"
)

// SameRecordedAction compares benchmark actions rather than payment
// witnesses. Every plan under one PaymentAction casts the same card; treating
// a different legal mana-tapping plan as a different player decision would
// score resource routing, not the cast-or-hold decision the source records.
func SameRecordedAction(got, want decision.Intent) bool {
	if got.Payment != nil || want.Payment != nil {
		return got.Payment != nil && want.Payment != nil && got.Payment.ActionID == want.Payment.ActionID
	}
	return reflect.DeepEqual(got, want)
}

// NativeRunResult is one replay-root outcome from root-run. Unlike Result it
// is intentionally not a sealed benchmark score: source root selection and
// payment-aware label semantics are still explicit inputs to the next layer.
type NativeRunResult struct {
	GameID        string          `json:"game_id"`
	SourceKind    string          `json:"source_kind"`
	SourceCard    string          `json:"source_card"`
	Ordinal       int             `json:"ordinal"`
	Seat          int             `json:"seat"`
	Arm           SearchArm       `json:"arm"`
	Choice        int             `json:"choice"`
	Intent        decision.Intent `json:"intent"`
	Recorded      decision.Intent `json:"recorded"`
	MatchRecorded bool            `json:"match_recorded"`
	Sims          int             `json:"sims"`
	Completed     int             `json:"completed"`
	Skipped       int             `json:"skipped"`
}

type NativeRunSummary struct {
	Rows, Matched, Searched, Skipped, Simulations, Completed int
}

func (s NativeRunSummary) Agreement() (float64, bool) {
	if s.Searched == 0 {
		return 0, false
	}
	return float64(s.Matched) / float64(s.Searched), true
}

func SummarizeNativeRuns(rows []NativeRunResult) (NativeRunSummary, SearchArm, error) {
	var out NativeRunSummary
	var arm SearchArm
	for i := range rows {
		r := rows[i]
		if r.GameID == "" || r.Ordinal < 0 || r.Seat < 0 || r.Seat > 1 || r.SourceKind != "land" && r.SourceKind != "spell" || r.SourceCard == "" || r.Arm == "" || r.Sims < 0 || r.Completed < 0 || r.Skipped < 0 || r.Completed > r.Sims {
			return NativeRunSummary{}, "", fmt.Errorf("searchbench: native result %d is invalid", i)
		}
		if arm == "" {
			arm = r.Arm
		} else if arm != r.Arm {
			return NativeRunSummary{}, "", errors.New("searchbench: native result file mixes arms")
		}
		out.Rows++
		out.Simulations += r.Sims
		out.Completed += r.Completed
		out.Skipped += r.Skipped
		if r.Skipped == 0 {
			out.Searched++
			if r.MatchRecorded {
				out.Matched++
			}
		}
	}
	if arm == "" {
		return NativeRunSummary{}, "", errors.New("searchbench: native result file is empty")
	}
	return out, arm, nil
}

func ReadNativeRunResults(path string) ([]NativeRunResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var out []NativeRunResult
	for line := 1; s.Scan(); line++ {
		if len(s.Bytes()) == 0 {
			return nil, fmt.Errorf("searchbench: blank native result line %d", line)
		}
		var row NativeRunResult
		dec := json.NewDecoder(bytes.NewReader(s.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&row); err != nil {
			return nil, fmt.Errorf("searchbench: native result line %d: %w", line, err)
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("searchbench: native result line %d has trailing JSON", line)
		}
		out = append(out, row)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
