package adopt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
)

// Reason buckets: every gate.Problem lands in exactly one (Bucket).
const (
	BucketNoPrintedList = "no printed list"
	BucketNotInCorpus   = "not in corpus"
	BucketUnsupported   = "unsupported"
	BucketGorgeWrong    = "gorge wrong"
	BucketXMageLacks    = "xmage lacks (hand scenario)"
	BucketTemplateGap   = "template gap"
	BucketNoVerdict     = "no verdict"
	BucketDiverge       = "diverge"
	BucketHarness       = "harness"
	BucketReview        = "review pending"
	BucketStale         = "stale verdict"
	BucketExpectation   = "expectation unmet"
	BucketLevel         = "level not built"
)

// Buckets lists the buckets in dashboard column order.
var Buckets = []string{BucketNoPrintedList, BucketNotInCorpus, BucketUnsupported, BucketGorgeWrong,
	BucketXMageLacks, BucketTemplateGap, BucketNoVerdict, BucketDiverge, BucketHarness, BucketReview,
	BucketStale, BucketExpectation, BucketLevel}

// Bucket classifies one gate problem by its reason (gate.Check's wording).
func Bucket(p gate.Problem) string {
	r := p.Reason
	switch {
	case strings.HasPrefix(r, "no printed list"):
		return BucketNoPrintedList
	case strings.HasPrefix(r, "not in the corpus"):
		return BucketNotInCorpus
	case strings.HasPrefix(r, "unsupported"):
		return BucketUnsupported
	case strings.HasPrefix(r, "gorge_wrong"), strings.HasPrefix(r, "verdict gorge_wrong"):
		return BucketGorgeWrong
	case strings.HasPrefix(r, "XMage does not implement"):
		return BucketXMageLacks
	case strings.HasPrefix(r, "no generated scenario"):
		return BucketTemplateGap
	case strings.HasPrefix(r, "no verdict"):
		return BucketNoVerdict
	case strings.HasPrefix(r, "verdict diverge"):
		return BucketDiverge
	case strings.HasPrefix(r, "verdict harness"):
		return BucketHarness
	case strings.HasPrefix(r, "automatic ruling"):
		return BucketReview
	case strings.HasPrefix(r, "verdict is for an older scenario"):
		return BucketStale
	case strings.HasPrefix(r, "level "):
		return BucketLevel
	}
	return BucketExpectation
}

// SetStatus is one set's line on the dashboard.
type SetStatus struct {
	Set         string         `json:"set"`
	Name        string         `json:"name"`
	SetType     string         `json:"set_type"`
	Released    string         `json:"released"`
	Formats     []string       `json:"formats,omitempty"`
	Level       string         `json:"level"`              // the level checked
	Declared    string         `json:"declared,omitempty"` // compliance/declared.json
	Printed     bool           `json:"printed"`            // has a compliance/printed list
	Cards       int            `json:"cards"`              // cards the claim covers
	Outstanding int            `json:"outstanding"`
	Buckets     map[string]int `json:"buckets,omitempty"`
	// NonTournament counts the outstanding problems on non-tournament cards
	// (planes, schemes, ...), which no tournament target needs.
	NonTournament int `json:"non_tournament,omitempty"`
}

// ChildMaxCards bounds one gate child's cards (Chunks). The full run
// happens in child processes, at most four at a time, so its parallelism
// never multiplies one heap and a child's heap is returned when it exits.
// Before rules published face facts by copy, each scenario pinned a ~1 MB
// engine table for the life of the process (1 set 0.59 GB, 24 sets 3.3 GB,
// 589 sets past 15 GB in one process); with the copy a child stays near
// 0.6 GB, and the batches remain the bound if a pin like it comes back.
const ChildMaxCards = 2000

// Chunks splits set codes into child-process batches of at most maxCards
// cards (a larger set is a batch of its own), in code order.
func (cs *Census) Chunks(codes []string, maxCards int) [][]string {
	var out [][]string
	var cur []string
	n := 0
	for _, c := range codes {
		k := len(cs.setCards[c])
		if len(cur) > 0 && n+k > maxCards {
			out = append(out, cur)
			cur, n = nil, 0
		}
		cur = append(cur, c)
		n += k
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// StatusChunked runs Status over codes in batches (Chunks), at most procs
// at a time, through run -- which must execute each batch in a fresh
// process (see ChildMaxCards) and return its set lines. The lines come
// back in code order.
func (cs *Census) StatusChunked(codes []string, procs int, run func(batch []string) ([]SetStatus, error)) ([]SetStatus, error) {
	if procs < 1 {
		procs = 1
	}
	if procs > 4 {
		procs = 4
	}
	batches := cs.Chunks(codes, ChildMaxCards)
	res := make([][]SetStatus, len(batches))
	errs := make([]error, len(batches))
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < procs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				res[i], errs[i] = run(batches[i])
			}
		}()
	}
	for i := range batches {
		work <- i
	}
	close(work)
	wg.Wait()
	var out []SetStatus
	for i, b := range batches {
		if errs[i] != nil {
			return nil, fmt.Errorf("batch %v: %v", b, errs[i])
		}
		out = append(out, res[i]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Set < out[j].Set })
	return out, nil
}

// Status runs the gate (gate.Check at level) over every set, or only the
// given codes, serially in this process: keep it to a batch of at most
// ChildMaxCards cards and use StatusChunked for more.
func (cs *Census) Status(reg *cards.Registry, root, level string, only ...string) ([]SetStatus, error) {
	declared, err := compliance.LoadDeclared(filepath.Join(root, "compliance", "declared.json"))
	if err != nil {
		return nil, err
	}
	codes := cs.SetCodes()
	if len(only) > 0 {
		var keep []string
		for _, c := range codes {
			if contains(only, c) {
				keep = append(keep, c)
			}
		}
		codes = keep
	}
	probs := make([][]gate.Problem, len(codes))
	errs := make([]error, len(codes))
	for i, code := range codes {
		probs[i], errs[i] = gate.Check(reg, root, code, level)
	}
	var out []SetStatus
	for i, code := range codes {
		if errs[i] != nil {
			return nil, fmt.Errorf("%s: %v", code, errs[i])
		}
		m := cs.Sets[code]
		probs := probs[i]
		st := SetStatus{Set: code, Name: m.Name, SetType: m.SetType, Released: m.Released,
			Formats: cs.SetFormats[code], Level: level, Declared: declared[code],
			Cards: len(cs.setCards[code]), Outstanding: len(probs), Buckets: map[string]int{}}
		_, perr := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), code)
		st.Printed = perr == nil
		for _, p := range probs {
			st.Buckets[Bucket(p)]++
			if c, ok := cs.byName[p.Card]; ok && c.NonTournament != "" {
				st.NonTournament++
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// Rollup is one format's line: "Standard:A" once every set is declared.
type Rollup struct {
	Format      string         `json:"format"`
	Level       string         `json:"level"`
	Sets        int            `json:"sets"`
	Declared    int            `json:"declared"` // sets declared at Level or above
	Cards       int            `json:"cards"`    // per-set card entries
	Outstanding int            `json:"outstanding"`
	Buckets     map[string]int `json:"buckets"`
	Label       string         `json:"label"`
}

// Complete reports whether every set of the format is declared.
func (r Rollup) Complete() bool { return r.Sets > 0 && r.Declared == r.Sets }

func levelAtLeast(have, want string) bool { return have != "" && have >= want }

// Rollups sums the set lines per format, in Config order, skipping a
// format none of whose sets were measured. The All target
// leaves out problems on non-tournament cards (C7).
func (cs *Census) Rollups(sets []SetStatus) []Rollup {
	var out []Rollup
	for _, f := range cs.Config.Formats {
		r := Rollup{Format: f.Name, Buckets: map[string]int{}}
		for _, s := range sets {
			if !contains(s.Formats, f.Name) {
				continue
			}
			r.Level = s.Level
			r.Sets++
			r.Cards += s.Cards
			if levelAtLeast(s.Declared, s.Level) {
				r.Declared++
			}
			n := s.Outstanding
			if f.Tournament {
				n -= s.NonTournament
			}
			r.Outstanding += n
			for b, k := range s.Buckets {
				r.Buckets[b] += k
			}
		}
		if r.Sets == 0 {
			continue // a slice that measured none of the format's sets
		}
		if r.Complete() {
			r.Label = f.Name + ":" + r.Level
		} else {
			r.Label = fmt.Sprintf("%s: %d/%d sets at %s, %d of %d entries outstanding", f.Name, r.Declared, r.Sets, r.Level, r.Outstanding, r.Cards)
		}
		out = append(out, r)
	}
	return out
}

// WriteDashboard renders the generated status page (section 11.3 C8): the
// format roll-ups, then one row per set with its reason buckets. It is
// generated by `make compliance-status`, never committed (section 6).
func (cs *Census) WriteDashboard(w io.Writer, sets []SetStatus, head string) {
	rolls := cs.Rollups(sets)
	fmt.Fprintf(w, "# Compliance status\n\nGenerated by `go run ./cmd/oraclediff status -all` at %s; do not commit (spec 2026-10-03 section 11.3 C8). The ratchet is `compliance/ratchet.json`.\n\n", head)
	fmt.Fprintf(w, "Declared levels mean exactly this -- %s.\n\n", gate.LevelMeaning("A"))
	fmt.Fprintf(w, "## Formats\n\n")
	for _, r := range rolls {
		fmt.Fprintf(w, "- **%s**\n", r.Label)
	}
	fmt.Fprintf(w, "\n| format | sets | declared | entries | outstanding |")
	for _, b := range Buckets {
		fmt.Fprintf(w, " %s |", b)
	}
	fmt.Fprintf(w, "\n|---|---|---|---|---|%s\n", strings.Repeat("---|", len(Buckets)))
	for _, r := range rolls {
		fmt.Fprintf(w, "| %s | %d | %d | %d | %d |", r.Format, r.Sets, r.Declared, r.Cards, r.Outstanding)
		for _, b := range Buckets {
			fmt.Fprintf(w, " %d |", r.Buckets[b])
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\n## Sets\n\nOrdered by first target format, then newest first.\n\n| set | name | type | released | formats | declared | printed list | cards | outstanding |")
	for _, b := range Buckets {
		fmt.Fprintf(w, " %s |", b)
	}
	fmt.Fprintf(w, "\n|---|---|---|---|---|---|---|---|---|%s\n", strings.Repeat("---|", len(Buckets)))
	rank := func(s SetStatus) int {
		for i, f := range cs.Config.Formats {
			if contains(s.Formats, f.Name) {
				return i
			}
		}
		return len(cs.Config.Formats)
	}
	sorted := append([]SetStatus(nil), sets...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Released != b.Released {
			return a.Released > b.Released
		}
		return a.Set < b.Set
	})
	for _, s := range sorted {
		pr := "no"
		if s.Printed {
			pr = "yes"
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %d | %d |", s.Set, s.Name, s.SetType, s.Released,
			strings.Join(s.Formats, " "), s.Declared, pr, s.Cards, s.Outstanding)
		for _, b := range Buckets {
			if n := s.Buckets[b]; n > 0 {
				fmt.Fprintf(w, " %d |", n)
			} else {
				fmt.Fprintf(w, " |")
			}
		}
		fmt.Fprintln(w)
	}
}

// RatchetFile is the committed certification ratchet, relative to the
// repo root.
const RatchetFile = "compliance/ratchet.json"

// RatchetEntry is one set's floor and ceiling: the level it is declared at
// (which may only rise), the outstanding count at level A (which may only
// fall), and, once the set has been measured at level B, its level-B
// outstanding count (which may only fall). OutstandingB is nil when the set
// has never been measured at B (an absent field in compliance/ratchet.json);
// only a level-B write sets it, and a level-A write carries it forward.
type RatchetEntry struct {
	Level        string `json:"level,omitempty"`
	Outstanding  int    `json:"outstanding"`
	OutstandingB *int   `json:"outstanding_b,omitempty"`
}

// LoadRatchet reads compliance/ratchet.json under root.
func LoadRatchet(root string) (map[string]RatchetEntry, error) {
	raw, err := os.ReadFile(filepath.Join(root, RatchetFile))
	if err != nil {
		return nil, err
	}
	var r map[string]RatchetEntry
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %v", RatchetFile, err)
	}
	return r, nil
}

// RatchetOf is the ratchet that records sets exactly as they stand at level
// A. It carries each entry's existing level-B floor (OutstandingB) forward
// from prev, because a level-A write measures nothing at B and must not
// drop an entry a level-B write recorded.
func RatchetOf(sets []SetStatus, prev map[string]RatchetEntry) map[string]RatchetEntry {
	out := map[string]RatchetEntry{}
	for _, s := range sets {
		out[s.Set] = RatchetEntry{Level: s.Declared, Outstanding: s.Outstanding, OutstandingB: prev[s.Set].OutstandingB}
	}
	return out
}

// RatchetOfB records the level-B floor for the measured sets: it updates
// only OutstandingB and keeps every other field of every entry (the level-A
// count included), so a B write never moves a level-A number and a set not
// measured at B keeps its previous B floor. sets must have been measured at
// level B.
func RatchetOfB(sets []SetStatus, prev map[string]RatchetEntry) map[string]RatchetEntry {
	out := map[string]RatchetEntry{}
	for k, e := range prev {
		out[k] = e
	}
	for _, s := range sets {
		e := out[s.Set]
		n := s.Outstanding
		e.OutstandingB = &n
		out[s.Set] = e
	}
	return out
}

// MarshalRatchet renders the ratchet one set per line, sorted, so a change
// diffs (and merges) set by set.
func MarshalRatchet(r map[string]RatchetEntry) []byte {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, k := range keys {
		e, _ := json.Marshal(r[k])
		fmt.Fprintf(&b, "%q: %s", k, e)
		if i < len(keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// CheckRatchet compares the measured sets with the committed ratchet.
// committed is every committed set code; measured may be a subset (the
// sets actually run). fails are hard regressions: a declared set or level
// dropped, a measured set's outstanding count grown, or a committed set
// missing from the ratchet (or a ratchet entry for a set no longer
// committed). slack lists the measured sets that improved past the
// ratchet: they pass, and `oraclediff status -all -write-ratchet` records
// the new ceiling.
func CheckRatchet(committed []string, measured []SetStatus, declared compliance.Declared, ratchet map[string]RatchetEntry) (fails, slack []string) {
	isCommitted := map[string]bool{}
	for _, c := range committed {
		isCommitted[c] = true
	}
	have := map[string]SetStatus{}
	for _, s := range measured {
		have[s.Set] = s
	}
	keys := make([]string, 0, len(ratchet))
	for k := range ratchet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := ratchet[k]
		if !isCommitted[k] {
			fails = append(fails, fmt.Sprintf("%s: in the ratchet but no longer a committed set", k))
			continue
		}
		if e.Level != "" && !levelAtLeast(declared[k], e.Level) {
			fails = append(fails, fmt.Sprintf("%s: was declared at %s, now %q -- a declared set or level only grows", k, e.Level, declared[k]))
		}
		s, ok := have[k]
		if !ok {
			continue
		}
		if s.Outstanding > e.Outstanding {
			fails = append(fails, fmt.Sprintf("%s: %d outstanding, ratchet %d -- per-set outstanding only shrinks (oraclediff status -set %s)", k, s.Outstanding, e.Outstanding, k))
		} else if s.Outstanding < e.Outstanding {
			slack = append(slack, fmt.Sprintf("%s: %d -> %d", k, e.Outstanding, s.Outstanding))
		}
	}
	codes := append([]string(nil), committed...)
	sort.Strings(codes)
	for _, k := range codes {
		if _, ok := ratchet[k]; !ok {
			fails = append(fails, fmt.Sprintf("%s: a committed set missing from %s (oraclediff status -all -write-ratchet)", k, RatchetFile))
		}
	}
	dk := make([]string, 0, len(declared))
	for k := range declared {
		dk = append(dk, k)
	}
	sort.Strings(dk)
	for _, k := range dk {
		if e := ratchet[k]; !levelAtLeast(e.Level, declared[k]) {
			fails = append(fails, fmt.Sprintf("%s: declared %s but the ratchet's floor is %q -- record the new floor (oraclediff status -all -write-ratchet)", k, declared[k], e.Level))
		}
	}
	return fails, slack
}
