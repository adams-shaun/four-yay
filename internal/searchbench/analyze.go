package searchbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Result is the per-item output of one search arm. It intentionally contains
// only the selected candidate and action bit here; depth, timing and search
// counters will be added to the run schema with the runner, rather than
// inventing unmeasured zero values now.
type Result struct {
	ManifestDigest string
	Arm            string
	ItemID         string
	AgentChoices   []int
	AgentAct       bool
}

// Analyze binds results to their sealed input manifest. It rejects duplicate,
// missing and cross-manifest rows; an incomplete run must never turn into a
// flattering score.
func Analyze(m Manifest, rows []Result) (Summary, string, error) {
	if err := m.Validate(); err != nil {
		return Summary{}, "", err
	}
	items := make(map[string]Item, len(m.Items))
	for _, it := range m.Items {
		items[it.ID] = it
	}
	if len(rows) != len(m.Items) {
		return Summary{}, "", fmt.Errorf("searchbench: results have %d rows, manifest has %d items", len(rows), len(m.Items))
	}
	seen := make(map[string]struct{}, len(rows))
	decisions := make([]Decision, 0, len(rows))
	arm := ""
	for i := range rows {
		r := &rows[i]
		if r.ManifestDigest != m.Digest || r.Arm == "" || r.ItemID == "" {
			return Summary{}, "", fmt.Errorf("searchbench: result %d has a wrong manifest digest or missing identity", i)
		}
		if arm == "" {
			arm = r.Arm
		} else if arm != r.Arm {
			return Summary{}, "", errors.New("searchbench: one result file mixes arms")
		}
		it, ok := items[r.ItemID]
		if !ok {
			return Summary{}, "", fmt.Errorf("searchbench: result names unknown item %q", r.ItemID)
		}
		if _, ok := seen[r.ItemID]; ok {
			return Summary{}, "", fmt.Errorf("searchbench: duplicate result for item %q", r.ItemID)
		}
		seen[r.ItemID] = struct{}{}
		decisions = append(decisions, Decision{ID: it.ID, Type: it.Type, Human: it.Label, AgentChoices: r.AgentChoices, AgentAct: r.AgentAct})
	}
	s, err := Score(decisions)
	return s, arm, err
}

// ReadResults reads one JSON object per line and rejects unknown fields and
// blank records. It leaves output ownership to the runner; analysis is read
// only and can be repeated safely.
func ReadResults(path string) ([]Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	buf := make([]byte, 0, 64<<10)
	s.Buffer(buf, 4<<20)
	var out []Result
	line := 0
	for s.Scan() {
		line++
		if len(s.Bytes()) == 0 {
			return nil, fmt.Errorf("searchbench: blank result line %d", line)
		}
		dec := json.NewDecoder(bytes.NewReader(s.Bytes()))
		dec.DisallowUnknownFields()
		var r Result
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("searchbench: result line %d: %w", line, err)
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("searchbench: result line %d has trailing JSON", line)
		}
		out = append(out, r)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
