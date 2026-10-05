package compliance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	// StatusXMageLacks is XMage's card database not holding the card at all
	// (a set marks it unfinished). It is the same endpoint as a card missing
	// from the manifest: the gate's no-XMage bucket, never a harness gap.
	StatusXMageLacks = "xmage_lacks"
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
	// Frozen is the expectation the gate holds gorge to on an agree or
	// xmage_wrong row: the comparator fields the scenario changed, at each
	// checkpoint (oraclediff.Freeze).
	Frozen []Frozen `json:"frozen,omitempty"`
	// CanonSHA is the legacy whole-snapshot expectation: the hash of
	// gorge's canonical snapshots. Rows from before field-level freezing
	// that `oraclediff refreeze` could not convert still carry it.
	CanonSHA string `json:"canon_sha,omitempty"`
	Detail   string `json:"detail,omitempty"` // first difference or harness message
	Ruling   string `json:"ruling,omitempty"` // triage note: who is wrong and why (CR cite)
	// RulingID names the compliance/rulings/<id>.json shape ruling that
	// classified this row automatically; empty for a hand ruling.
	RulingID string `json:"ruling_id,omitempty"`
	// Review is "pending" on an automatic classification sampled for a
	// human check (one in ten) until someone confirms it ("confirmed").
	Review string `json:"review,omitempty"`
}

// Frozen is one frozen expectation: the value a comparator field had at a
// checkpoint when the engines agreed (spec 2026-10-02 section 7; 2026-10-03
// section 11.3 C3). Only the fields the scenario changed are frozen, so a
// verdict never depends on board state the card did not touch.
type Frozen struct {
	At    string `json:"at"` // checkpoint name; "" for whole-run fields
	Field string `json:"field"`
	Value string `json:"value"`
}

// HasExpectation reports whether a passing row carries something the gate
// can hold gorge to.
func (r VerdictRow) HasExpectation() bool { return len(r.Frozen) > 0 || r.CanonSHA != "" }

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
// classified disagreement carries its ruling forward only while its
// divergence signature (field and engine values) is unchanged.
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
			if hasRuling(old) {
				oldSig, oldOK := divergenceSignature(old)
				newSig, newOK := divergenceSignature(r)
				if oldOK && newOK && oldSig == newSig {
					// Keep the operator's classification only while the underlying
					// field and values being classified are unchanged.
					r.Status, r.Ruling, r.RulingID, r.Review = old.Status, old.Ruling, old.RulingID, old.Review
					r.CanonSHA, r.Frozen = old.CanonSHA, old.Frozen
				} else {
					fmt.Printf("dropped ruling for %s (%s): divergence changed\n", r.Card, r.Template)
				}
			} else if old.Status == r.Status && r.Ruling == "" {
				r.Ruling = old.Ruling
			}
		}
		all[r.Card][r.Template] = r
	}
	return writeVerdicts(dir, all)
}

// hasRuling reports whether the row carries an operator-authored or
// shape-applied classification worth preserving across an identical divergence.
func hasRuling(r VerdictRow) bool {
	return r.Ruling != "" || r.RulingID != "" || r.Review == "confirmed"
}

type divergence struct {
	field, gorge, xmage string
}

// divergenceSignature extracts the comparator identity from the diff detail.
// The checkpoint is intentionally excluded: a ruling concerns the field and
// the two observed values, not where a scenario happened to observe them.
func divergenceSignature(r VerdictRow) (divergence, bool) {
	if (r.Status != StatusDiverge && r.Status != StatusGorgeWrong && r.Status != StatusXMageWrong) || r.Detail == "" {
		return divergence{}, false
	}
	marker := ": gorge "
	i := strings.Index(r.Detail, marker)
	if i < 0 {
		return divergence{}, false
	}
	left := strings.TrimSpace(r.Detail[:i])
	space := strings.LastIndexByte(left, ' ')
	if space < 0 || space == len(left)-1 {
		return divergence{}, false
	}
	field := left[space+1:]
	gorgeStart := i + len(marker)
	gorgeEnd := quotedEnd(r.Detail, gorgeStart)
	if gorgeEnd < 0 || !strings.HasPrefix(r.Detail[gorgeEnd:], ", xmage ") {
		return divergence{}, false
	}
	xmageStart := gorgeEnd + len(", xmage ")
	xmageEnd := quotedEnd(r.Detail, xmageStart)
	if xmageEnd < 0 || xmageEnd != len(r.Detail) {
		return divergence{}, false
	}
	gorge, err := strconv.Unquote(r.Detail[gorgeStart:gorgeEnd])
	if err != nil {
		return divergence{}, false
	}
	xmage, err := strconv.Unquote(r.Detail[xmageStart:xmageEnd])
	if err != nil {
		return divergence{}, false
	}
	return divergence{field: field, gorge: gorge, xmage: xmage}, true
}

// quotedEnd returns the index after a Go-quoted string beginning at start.
func quotedEnd(s string, start int) int {
	if start >= len(s) || s[start] != '"' {
		return -1
	}
	escaped := false
	for i := start + 1; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch s[i] {
		case '\\':
			escaped = true
		case '"':
			return i + 1
		}
	}
	return -1
}

// ReplaceVerdicts writes rows into dir, replacing any row with the same
// card and template unconditionally (a re-triage, where MergeVerdicts'
// keep-the-triaged-row policy would undo the change).
func ReplaceVerdicts(dir string, rows []VerdictRow) error {
	all, err := LoadVerdicts(dir)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if all[r.Card] == nil {
			all[r.Card] = map[string]VerdictRow{}
		}
		all[r.Card][r.Template] = r
	}
	return writeVerdicts(dir, all)
}

func writeVerdicts(dir string, all map[string]map[string]VerdictRow) error {
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
