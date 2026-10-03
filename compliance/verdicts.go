package compliance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Verdict statuses. A card counts toward a declared level only through an
// Agree or XMageWrong row (or a hand-authored oracle scenario).
const (
	StatusAgree      = "agree"       // both engines produced the same snapshots
	StatusGorgeWrong = "gorge_wrong" // triaged: gorge is wrong; feeds a fix ticket
	StatusXMageWrong = "xmage_wrong" // triaged: XMage is wrong; gorge's result frozen, Ruling cites the CR
	StatusDiverge    = "diverge"     // untriaged disagreement
	StatusHarness    = "harness"     // a driver could not express the scenario
)

// VerdictRow is one card x template result (spec section 5,
// compliance/verdicts/<a-z>.jsonl).
type VerdictRow struct {
	Card        string `json:"card"`
	Template    string `json:"template"`
	ID          string `json:"id"`
	ScenarioSHA string `json:"scenario_sha"` // sha256 of the generated scenario line
	XMageRef    string `json:"xmage_ref"`
	Status      string `json:"status"`
	CanonSHA    string `json:"canon_sha,omitempty"` // gorge's canonical snapshots at agree/xmage_wrong
	Detail      string `json:"detail,omitempty"`    // first difference or harness message
	Ruling      string `json:"ruling,omitempty"`    // triage note: who is wrong and why (CR cite)
}

// VerdictDir is the committed verdict directory, relative to the repo root.
const VerdictDir = "compliance/verdicts"

func shard(card string) string {
	for _, r := range strings.ToLower(card) {
		if r >= 'a' && r <= 'z' {
			return string(r)
		}
		if r >= '0' && r <= '9' {
			return "0"
		}
	}
	return "0"
}

// LoadVerdicts reads every shard under dir, keyed by card then template.
func LoadVerdicts(dir string) (map[string]map[string]VerdictRow, error) {
	out := map[string]map[string]VerdictRow{}
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for n := 1; sc.Scan(); n++ {
			if len(bytes.TrimSpace(sc.Bytes())) == 0 {
				continue
			}
			var r VerdictRow
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				f.Close()
				return nil, fmt.Errorf("%s:%d: %v", p, n, err)
			}
			if out[r.Card] == nil {
				out[r.Card] = map[string]VerdictRow{}
			}
			out[r.Card][r.Template] = r
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MergeVerdicts writes rows into dir, replacing any row with the same card
// and template, and keeps each shard sorted so diffs read card by card. A
// replaced row keeps its Ruling when the new row has the same status, and a
// triaged row (gorge_wrong/xmage_wrong) is kept over an untriaged diverge
// for the same scenario.
func MergeVerdicts(dir string, rows []VerdictRow) error {
	all, err := LoadVerdicts(dir)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if all[r.Card] == nil {
			all[r.Card] = map[string]VerdictRow{}
		}
		if old, ok := all[r.Card][r.Template]; ok {
			triaged := old.Status == StatusGorgeWrong || old.Status == StatusXMageWrong
			if triaged && r.Status == StatusDiverge && old.ScenarioSHA == r.ScenarioSHA {
				// A re-run of the same scenario still disagreeing keeps its
				// triage ruling.
				continue
			}
			if old.Status == r.Status && r.Ruling == "" {
				r.Ruling = old.Ruling
			}
		}
		all[r.Card][r.Template] = r
	}
	shards := map[string][]VerdictRow{}
	for card, byT := range all {
		for _, r := range byT {
			shards[shard(card)] = append(shards[shard(card)], r)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for s, rs := range shards {
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].Card != rs[j].Card {
				return rs[i].Card < rs[j].Card
			}
			return rs[i].Template < rs[j].Template
		})
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		for _, r := range rs {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, s+".jsonl"), b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Declared is compliance/declared.json: set code -> level ("A" or "B").
type Declared map[string]string

// LoadDeclared reads the declared sets.
func LoadDeclared(path string) (Declared, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Declared
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	for set, lvl := range d {
		if lvl != "A" && lvl != "B" {
			return nil, fmt.Errorf("%s: set %s level %q (want A or B)", path, set, lvl)
		}
	}
	return d, nil
}

// LoadManifest reads one committed set manifest by set code.
func LoadManifest(dir, code string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFileName(code)))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	err = json.Unmarshal(raw, &m)
	return m, err
}

// Printed is compliance/printed/<CODE>.json: every card name printed in a
// set (from Forge's edition file), which can exceed XMage's manifest.
type Printed struct {
	Code   string   `json:"code"`
	Source string   `json:"source"`
	Cards  []string `json:"cards"`
}

// LoadPrinted reads a set's printed list.
func LoadPrinted(dir, code string) (Printed, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFileName(code)))
	if err != nil {
		return Printed{}, err
	}
	var p Printed
	err = json.Unmarshal(raw, &p)
	return p, err
}
