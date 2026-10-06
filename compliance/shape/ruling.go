package shape

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/compliance"
)

// RulingDir is the committed rulings directory, relative to the repo root.
const RulingDir = "compliance/rulings"

// TriageDir is the committed cluster directory, relative to the repo root.
const TriageDir = "compliance/triage"

// ReviewFile, under TriageDir, lists the automatic classifications sampled
// for a human check that no one has confirmed yet.
const ReviewFile = "_review.jsonl"

// Ruling is one reusable triage decision (compliance/rulings/<id>.json):
// every disagreement whose shape it matches gets its status.
type Ruling struct {
	ID string `json:"id"` // equals the file name stem
	// Status is gorge_wrong (feeds one fix ticket for every card it
	// matches), xmage_wrong (gorge's result is frozen as the expectation and
	// counts as passing) or harness (a scenario or driver gap: the card
	// stays outstanding, explained).
	Status string   `json:"status"`
	Match  Match    `json:"match"`
	Ruling string   `json:"ruling"` // who is wrong and why
	CR     []string `json:"cr,omitempty"`
	Ticket string   `json:"ticket,omitempty"`
	// Source names the hand triage the ruling generalises.
	Source string `json:"source,omitempty"`
}

// Match selects shapes. Each string is a pattern in which '*' matches any
// run of characters and everything else is literal; empty matches anything.
// API, when set, narrows the ruling to cards whose IR API signature (API)
// matches one of its patterns.
type Match struct {
	Template string   `json:"template,omitempty"`
	Op       string   `json:"op,omitempty"`
	Field    string   `json:"field,omitempty"`
	Diff     string   `json:"diff,omitempty"`
	API      []string `json:"api,omitempty"`
}

// Glob reports whether s matches pattern p ('*' = any run, else literal).
func Glob(p, s string) bool {
	if p == "" {
		return true
	}
	parts := strings.Split(p, "*")
	if len(parts) == 1 {
		return p == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// Matches reports whether the ruling covers a shape for a card of the
// given API signature.
func (r Ruling) Matches(s Shape, api string) bool {
	m := r.Match
	if !Glob(m.Template, s.Template) || !Glob(m.Op, s.Op) || !Glob(m.Field, s.Field) || !Glob(m.Diff, s.Diff) {
		return false
	}
	if len(m.API) == 0 {
		return true
	}
	for _, p := range m.API {
		if Glob(p, api) {
			return true
		}
	}
	return false
}

// Text is the verdict row's Ruling for an automatic classification.
func (r Ruling) Text() string {
	t := r.Ruling
	if len(r.CR) > 0 {
		t += " (CR " + strings.Join(r.CR, ", ") + ")"
	}
	return t
}

// LoadRulings reads every ruling under dir, sorted by id.
func LoadRulings(dir string) ([]Ruling, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []Ruling
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r Ruling
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("%s: %v", p, err)
		}
		if want := strings.TrimSuffix(filepath.Base(p), ".json"); r.ID != want {
			return nil, fmt.Errorf("%s: id %q, want the file name %q", p, r.ID, want)
		}
		switch r.Status {
		case compliance.StatusGorgeWrong, compliance.StatusXMageWrong, compliance.StatusHarness:
		default:
			return nil, fmt.Errorf("%s: status %q (want gorge_wrong, xmage_wrong or harness)", p, r.Status)
		}
		if r.Ruling == "" {
			return nil, fmt.Errorf("%s: empty ruling text", p)
		}
		if r.Match.Template == "" && r.Match.Field == "" && r.Match.Diff == "" && len(r.Match.API) == 0 {
			return nil, fmt.Errorf("%s: a ruling must narrow its match", p)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Find returns the rulings matching a shape, in id order. More than one is
// an overlap the caller reports; the first applies.
func Find(rs []Ruling, s Shape, api string) []Ruling {
	var out []Ruling
	for _, r := range rs {
		if r.Matches(s, api) {
			out = append(out, r)
		}
	}
	return out
}

// Sampled is the one-in-ten draw of automatic classifications a human
// checks (spec 2026-10-02 section 8: a wrong ruling cannot spread
// unchecked). Deterministic in the card, template and ruling, so a re-run
// samples the same rows.
func Sampled(card, template, rulingID string) bool {
	h := fnv.New32a()
	h.Write([]byte(card + "\x00" + template + "\x00" + rulingID))
	return h.Sum32()%10 == 0
}

// Review states of an automatic classification.
const (
	ReviewPending   = "pending"   // sampled; a human has not checked it
	ReviewConfirmed = "confirmed" // sampled and checked
)

// Classify applies the first matching ruling to a diverge or harness row,
// or to a row an earlier ruling classified, and reports what changed. A
// row whose earlier automatic ruling no longer matches reverts to diverge
// (or stays harness). Hand rulings (no RulingID) are never touched. The
// caller freezes gorge's result when the row becomes xmage_wrong.
func Classify(r *compliance.VerdictRow, rs []Ruling, api string) (applied *Ruling, overlap []string, changed bool) {
	auto := r.RulingID != ""
	if !auto && r.Status != compliance.StatusDiverge && r.Status != compliance.StatusHarness {
		return nil, nil, false
	}
	if !auto && r.Status == compliance.StatusHarness && r.Ruling != "" {
		return nil, nil, false
	}
	s, ok := Of(*r)
	if !ok {
		return nil, nil, false
	}
	before := *r
	ms := Find(rs, s, api)
	if len(ms) == 0 {
		if !auto {
			return nil, nil, false
		}
		if r.Status != compliance.StatusHarness {
			r.Status = compliance.StatusDiverge
		}
		r.Ruling, r.RulingID, r.Review, r.CanonSHA, r.Frozen = "", "", "", "", nil
		return nil, nil, true
	}
	for _, m := range ms[1:] {
		overlap = append(overlap, m.ID)
	}
	m := ms[0]
	if r.RulingID != m.ID {
		r.Review = ""
		if Sampled(r.Card, r.Template, m.ID) {
			r.Review = ReviewPending
		}
	}
	r.Status, r.Ruling, r.RulingID = m.Status, m.Text(), m.ID
	if r.Status != compliance.StatusXMageWrong {
		r.CanonSHA, r.Frozen = "", nil
	}
	return &m, overlap, !rowEqual(*r, before)
}

// rowEqual is the typed equality Classify reports change by. It compares
// every VerdictRow field, and Frozen element by element with nil distinct
// from empty (the exact semantics the reflect.DeepEqual it replaced had).
// TestRowEqualCoversEveryField guards against a field added to VerdictRow
// but not here.
func rowEqual(a, b compliance.VerdictRow) bool {
	if a.Card != b.Card || a.Template != b.Template || a.ID != b.ID ||
		a.ScenarioSHA != b.ScenarioSHA || a.XMageRef != b.XMageRef ||
		a.Status != b.Status || a.CanonSHA != b.CanonSHA || a.Detail != b.Detail ||
		a.Ruling != b.Ruling || a.RulingID != b.RulingID || a.Review != b.Review {
		return false
	}
	if (a.Frozen == nil) != (b.Frozen == nil) || len(a.Frozen) != len(b.Frozen) {
		return false
	}
	for i := range a.Frozen {
		if a.Frozen[i] != b.Frozen[i] {
			return false
		}
	}
	return true
}
