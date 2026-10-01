package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// liveWindow is how recent a run's stderr (or newest round file) must be for
// the run to count as running.
const liveWindow = 5 * time.Minute

// maxDepth bounds discovery below each root: <exp>/<group>/<sub>/<run> is the
// deepest layout measured (pn14/results-ds4/runs/<arm>).
const maxDepth = 4

// skipDirs are never descended into: go build scratch, agent scratch, and
// checkpoint/binary directories that hold no run dirs.
var skipDirs = map[string]bool{
	"gotmp": true, ".superpowers": true, ".git": true, ".ds4": true,
	"node_modules": true, "scratch": true, "bin": true, "dump": true,
}

var roundDirRe = regexp.MustCompile(`^(round|gen)(\d+)$`)

// Snapshot is the whole /api/runs payload.
type Snapshot struct {
	Generated   time.Time     `json:"generated"`
	Roots       []string      `json:"roots"`
	Experiments []Experiment  `json:"experiments"`
	Alerts      []Alert       `json:"alerts"`
	Artifacts   []Artifact    `json:"artifacts"`
	DeckLedgers []*DeckLedger `json:"deck_ledgers,omitempty"`
	DeckHistory []DeckHistory `json:"deck_history,omitempty"`
}

// Experiment groups the run dirs under one top-level directory of a root.
type Experiment struct {
	Name string `json:"name"`
	Runs []*Run `json:"runs"`
}

// Run kinds.
const (
	KindExitloop = "exitloop"
	KindAdhoc    = "adhoc"
)

// Run is one exitloop run directory, or (Kind adhoc) an ad-hoc run: a
// directory holding report.md and/or *.jsonl, or a <name>.stdout/.stderr
// pair with no <name> directory beside it.
type Run struct {
	ID            string      `json:"id"`
	Kind          string      `json:"kind"` // exitloop, adhoc
	Experiment    string      `json:"experiment"`
	Group         string      `json:"group"`
	Name          string      `json:"name"`
	Path          string      `json:"path"`
	Status        string      `json:"status"` // running, stale, done
	Stage         string      `json:"stage"`  // last exitloop stage line
	CurrentRound  *int        `json:"current_round,omitempty"`
	PlannedRounds *int        `json:"planned_rounds,omitempty"`
	KLTarget      *float64    `json:"kl_target,omitempty"`
	Control       *EvalResult `json:"control,omitempty"`
	Rounds        []Round     `json:"rounds"`
	Timing        *Table      `json:"timing,omitempty"`
	StderrTail    []string    `json:"stderr_tail,omitempty"`
	LastModified  time.Time   `json:"last_modified"`
	Warnings      []string    `json:"warnings,omitempty"`
	Alerts        []Alert     `json:"alerts,omitempty"`

	// Adhoc-only fields.
	JSONL      []JSONLFile `json:"jsonl,omitempty"`
	Report     string      `json:"report,omitempty"` // report.md path, if present
	ReportTail []string    `json:"report_tail,omitempty"`
}

// JSONLFile is one *.jsonl file of an adhoc run. Records is the line count,
// omitted when the file is too large to count cheaply.
type JSONLFile struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	Records *int   `json:"records,omitempty"`
}

// Round is one roundN (or genN) directory's parsed outputs.
type Round struct {
	N       int         `json:"n"`
	Eval    *EvalResult `json:"eval,omitempty"`
	Collect *EvalResult `json:"collect,omitempty"`
	Stats   *Stats      `json:"stats,omitempty"`
}

// EvalResult is a botbench -out json document reduced to what the page draws.
type EvalResult struct {
	WinRate   *float64    `json:"win_rate,omitempty"`
	CI        []float64   `json:"ci,omitempty"`
	MeanTurns *float64    `json:"mean_turns,omitempty"`
	Games     *float64    `json:"games,omitempty"`
	Pairs     []PairShare `json:"pairs,omitempty"`
}

// PairShare is one deck pair's win rate.
type PairShare struct {
	Pair    string    `json:"pair"`
	WinRate *float64  `json:"win_rate,omitempty"`
	CI      []float64 `json:"ci,omitempty"`
}

// Stats is policytrain -stats-out reduced to what the page draws.
type Stats struct {
	Shape            *float64 `json:"shape,omitempty"`
	MeanAdv          *float64 `json:"mean_adv,omitempty"`
	FinalKL          *float64 `json:"final_kl,omitempty"`
	FinalClip        *float64 `json:"final_clip,omitempty"`
	ValueLogLoss     *float64 `json:"value_log_loss,omitempty"`
	InitValueLogLoss *float64 `json:"init_value_log_loss,omitempty"`
	BaseLogLoss      *float64 `json:"base_log_loss,omitempty"`
	Kinds            []Kind   `json:"kinds,omitempty"`
	Epochs           []Epoch  `json:"epochs,omitempty"`
}

// Kind is one by_kind entry.
type Kind struct {
	Kind         string   `json:"kind"`
	N            *float64 `json:"n,omitempty"`
	DeviatedPct  *float64 `json:"deviated_pct,omitempty"`
	FinalKL      *float64 `json:"final_kl,omitempty"`
	FinalClip    *float64 `json:"final_clip,omitempty"`
	FinalFlip    *float64 `json:"final_flip,omitempty"`
	GreedyFlip   *float64 `json:"greedy_flip,omitempty"`
	OffGreedyPct *float64 `json:"off_greedy_pct,omitempty"`
	MeanAbsAdv   *float64 `json:"mean_abs_adv,omitempty"`
	MeanAdv      *float64 `json:"mean_adv,omitempty"`
}

// Epoch is one epochs[] entry.
type Epoch struct {
	Epoch        *float64 `json:"epoch,omitempty"`
	Surrogate    *float64 `json:"surrogate,omitempty"`
	ClipFrac     *float64 `json:"clip_frac,omitempty"`
	KL           *float64 `json:"kl,omitempty"`
	ValueLogLoss *float64 `json:"value_log_loss,omitempty"`
}

// Table is a parsed TSV.
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
}

// Artifact is a markdown report found under a root.
type Artifact struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
}

// Scanner discovers and parses runs, caching every parsed file by
// (mtime, size) so a rescan only re-reads what changed. It never writes.
type Scanner struct {
	Roots []string
	Now   func() time.Time

	// DeckFocus is the comma list of policy names whose per-deck win rate the
	// deck ledgers report. Empty means auto: the policy appearing in the most
	// seats of each ledger (ties broken lexically).
	DeckFocus []string

	// DeckRoots are the directories scanned for matches.jsonl per-deck
	// ledgers, separate from Roots. A training root (the default
	// /mnt/sata/gorge-training) holds hundreds of matches.jsonl files in
	// scratch dirs; walking every one of them to build per-deck charts both
	// costs seconds on a cold scan and floods the page with unrelated
	// ledgers. Deck ledgers therefore have their own narrow root, defaulting
	// to the gauntlet cache. Empty disables the per-deck panel.
	DeckRoots []string

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	mod  time.Time
	size int64
	val  any
}

// NewScanner returns a scanner over roots.
func NewScanner(roots []string) *Scanner {
	return &Scanner{Roots: roots, Now: time.Now, cache: map[string]cacheEntry{}}
}

// cached returns parse(path) memoised on the file's mtime and size. A missing
// file yields (nil, false).
func (s *Scanner) cached(path string, parse func([]byte) any) (any, bool) {
	return s.cachedAs(path, "", parse)
}

// cachedAs is cached with a discriminator in the key. Two readers of the SAME
// file (parseAdhoc counts matches.jsonl lines as an int; the deck ledger parses
// the same file into a *DeckLedger) must not overwrite each other's cache
// entry, so tag names the reader. With an empty tag it is cached().
func (s *Scanner) cachedAs(path, tag string, parse func([]byte) any) (any, bool) {
	key := path + "\x00" + tag
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil, false
	}
	s.mu.Lock()
	e, ok := s.cache[key]
	s.mu.Unlock()
	if ok && e.mod.Equal(fi.ModTime()) && e.size == fi.Size() {
		return e.val, true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	v := parse(b)
	s.mu.Lock()
	s.cache[key] = cacheEntry{mod: fi.ModTime(), size: fi.Size(), val: v}
	s.mu.Unlock()
	return v, true
}

// jsonDoc loads path as a generic JSON object. A truncated or malformed file
// (a stage mid-write) returns nil, ok=true, bad=true.
func (s *Scanner) jsonDoc(path string) (doc map[string]any, present, bad bool) {
	// A zero-byte file is a stage that has opened its output but not yet
	// written it (botbench's stdout redirect): not there yet, not broken.
	if fi, err := os.Stat(path); err == nil && fi.Size() == 0 {
		return nil, false, false
	}
	v, ok := s.cached(path, func(b []byte) any {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil
		}
		return m
	})
	if !ok {
		return nil, false, false
	}
	m, _ := v.(map[string]any)
	return m, true, m == nil
}

// Scan walks every root and returns a fresh snapshot.
func (s *Scanner) Scan() *Snapshot {
	now := s.Now()
	snap := &Snapshot{Generated: now, Roots: s.Roots}
	byExp := map[string]*Experiment{}
	var order []string
	multi := len(s.Roots) > 1
	for _, root := range s.Roots {
		var found []foundRun
		findRuns(root, 0, &found)
		for _, fr := range found {
			dir := fr.path
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			id := rel
			if multi {
				id = filepath.Base(root) + "/" + rel
			}
			parts := strings.Split(rel, "/")
			expName := parts[0]
			if multi {
				expName = filepath.Base(root) + "/" + parts[0]
			}
			var r *Run
			if fr.kind == KindAdhoc {
				r = s.parseAdhoc(dir, fr.pair, now)
			} else {
				r = s.parseRun(dir, now)
			}
			r.ID = id
			r.Experiment = expName
			r.Name = parts[len(parts)-1]
			r.Group = strings.Join(parts[:len(parts)-1], "/")
			r.Alerts = RunAlerts(r)
			exp := byExp[expName]
			if exp == nil {
				exp = &Experiment{Name: expName}
				byExp[expName] = exp
				order = append(order, expName)
			}
			exp.Runs = append(exp.Runs, r)
		}
		snap.Artifacts = append(snap.Artifacts, findArtifacts(root, multi)...)
	}
	for _, droot := range s.DeckRoots {
		dmulti := len(s.DeckRoots) > 1
		for _, dl := range s.scanDeckLedgers(droot, dmulti) {
			snap.DeckLedgers = append(snap.DeckLedgers, dl)
		}
		snap.DeckHistory = append(snap.DeckHistory, scanDeckHistory(droot)...)
	}
	sort.Strings(order)
	for _, name := range order {
		exp := byExp[name]
		sort.Slice(exp.Runs, func(i, j int) bool { return exp.Runs[i].ID < exp.Runs[j].ID })
		snap.Experiments = append(snap.Experiments, *exp)
		for _, r := range exp.Runs {
			snap.Alerts = append(snap.Alerts, r.Alerts...)
		}
	}
	if snap.Experiments == nil {
		snap.Experiments = []Experiment{}
	}
	if snap.Alerts == nil {
		snap.Alerts = []Alert{}
	}
	if snap.Artifacts == nil {
		snap.Artifacts = []Artifact{}
	}
	if snap.DeckLedgers == nil {
		snap.DeckLedgers = []*DeckLedger{}
	}
	if snap.DeckHistory == nil {
		snap.DeckHistory = []DeckHistory{}
	}
	return snap
}

// scanDeckLedgers parses every matches.jsonl under root into a per-deck
// ledger, cached on the file's mtime and size like every other scan read. One
// DeckLedger is produced per configured focus policy; with no focus it is the
// auto-detected dominant policy.
func (s *Scanner) scanDeckLedgers(root string, multi bool) []*DeckLedger {
	focuses := s.DeckFocus
	if len(focuses) == 0 {
		focuses = []string{""}
	}
	var out []*DeckLedger
	for _, f := range findDeckLedgers(root) {
		id := f.id
		if multi {
			id = filepath.Base(root) + "/" + id
		}
		for _, focus := range focuses {
			fc := focus
			v, ok := s.cachedAs(f.source, "deckledger:"+fc, func(b []byte) any { return parseDeckLedger(id, f.source, b, fc) })
			if !ok {
				continue
			}
			if dl, _ := v.(*DeckLedger); dl != nil {
				out = append(out, dl)
			}
		}
	}
	return out
}

// isRunDir reports whether a directory's entries mark it as an exitloop run:
// plan.txt, timing.tsv, or a roundN/genN subdirectory.
func isRunDir(entries []os.DirEntry) bool {
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && (n == "plan.txt" || n == "timing.tsv") {
			return true
		}
		if e.IsDir() && roundDirRe.MatchString(n) {
			return true
		}
	}
	return false
}

// isAdhocDir reports whether a (non-exitloop) directory's entries mark it as
// an ad-hoc run: a report.md or any *.jsonl file.
func isAdhocDir(entries []os.DirEntry) bool {
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && (n == "report.md" || strings.HasSuffix(n, ".jsonl")) {
			return true
		}
	}
	return false
}

// adhocPairs returns the <base> names of every <base>.stdout/<base>.stderr
// pair among entries that has no <base> directory beside it (an exitloop or
// adhoc run dir owns its own sibling logs).
func adhocPairs(entries []os.DirEntry) []string {
	files, dirs := map[string]bool{}, map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = true
		} else {
			files[e.Name()] = true
		}
	}
	var out []string
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".stdout")
		if e.IsDir() || !ok || base == "" || dirs[base] || !files[base+".stderr"] {
			continue
		}
		out = append(out, base)
	}
	return out
}

// foundRun is one discovered run: an exitloop or adhoc directory, or an
// adhoc stdout/stderr pair (path is then <dir>/<base>, not a directory).
type foundRun struct {
	path string
	kind string
	pair bool
}

func findRuns(dir string, depth int, out *[]foundRun) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if depth > 0 && isRunDir(entries) {
		*out = append(*out, foundRun{path: dir, kind: KindExitloop})
		return
	}
	// Ad-hoc runs sit at the same depth as an exitloop run dir: below an
	// experiment-level directory. Discovery still descends past them so an
	// exitloop run nested below is never hidden.
	if depth > 1 && isAdhocDir(entries) {
		*out = append(*out, foundRun{path: dir, kind: KindAdhoc})
	}
	if depth > 0 {
		for _, base := range adhocPairs(entries) {
			*out = append(*out, foundRun{path: filepath.Join(dir, base), kind: KindAdhoc, pair: true})
		}
	}
	if depth >= maxDepth {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || skipDirs[e.Name()] || strings.HasPrefix(e.Name(), "bin-") {
			continue
		}
		findRuns(filepath.Join(dir, e.Name()), depth+1, out)
	}
}

var (
	stageRe   = regexp.MustCompile(`\[(?:round|gen) (\d+)\]`)
	planGenRe = regexp.MustCompile(`(?m)^\[gen (\d+)\]`)
	planKLRe  = regexp.MustCompile(`-ppo-kl[ =]([0-9.eE+-]+)`)
)

func (s *Scanner) parseRun(dir string, now time.Time) *Run {
	r := &Run{Path: dir, Kind: KindExitloop, Rounds: []Round{}}
	latest := time.Time{}
	touch := func(p string) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
	}

	// plan.txt: planned rounds and the PPO KL target.
	if v, ok := s.cached(filepath.Join(dir, "plan.txt"), func(b []byte) any { return parsePlan(b) }); ok {
		p := v.(planInfo)
		r.PlannedRounds, r.KLTarget = p.rounds, p.klTarget
	}

	if doc, present, bad := s.jsonDoc(filepath.Join(dir, "control.json")); present {
		if bad {
			r.Warnings = append(r.Warnings, "control.json unreadable (partial write?)")
		} else {
			r.Control = evalFromDoc(doc)
		}
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		m := roundDirRe.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		rd := filepath.Join(dir, e.Name())
		rnd := Round{N: n}
		evalPath := filepath.Join(rd, "eval.json")
		if _, err := os.Stat(evalPath); err != nil {
			// genN layouts name evals eval-<kinds>.json; take the widest
			// kind set (longest name, then lexically last).
			if ms, _ := filepath.Glob(filepath.Join(rd, "eval*.json")); len(ms) > 0 {
				sort.Slice(ms, func(i, j int) bool {
					if len(ms[i]) != len(ms[j]) {
						return len(ms[i]) < len(ms[j])
					}
					return ms[i] < ms[j]
				})
				evalPath = ms[len(ms)-1]
			}
		}
		for _, f := range []struct {
			path string
			set  func(map[string]any)
		}{
			{evalPath, func(d map[string]any) { rnd.Eval = evalFromDoc(d) }},
			{filepath.Join(rd, "collect.json"), func(d map[string]any) { rnd.Collect = evalFromDoc(d) }},
			{filepath.Join(rd, "stats.json"), func(d map[string]any) { rnd.Stats = statsFromDoc(d) }},
		} {
			doc, present, bad := s.jsonDoc(f.path)
			if !present {
				continue
			}
			touch(f.path)
			if bad {
				r.Warnings = append(r.Warnings, e.Name()+"/"+filepath.Base(f.path)+" unreadable (partial write?)")
				continue
			}
			f.set(doc)
		}
		if rnd.Eval != nil || rnd.Collect != nil || rnd.Stats != nil {
			r.Rounds = append(r.Rounds, rnd)
		}
	}
	sort.Slice(r.Rounds, func(i, j int) bool { return r.Rounds[i].N < r.Rounds[j].N })

	timingPath := filepath.Join(dir, "timing.tsv")
	touch(timingPath)
	if v, ok := s.cached(timingPath, func(b []byte) any { return parseTSV(b) }); ok {
		r.Timing = v.(*Table)
	}

	// Sibling <run>.stderr carries the exitloop stage lines.
	stderrPath := dir + ".stderr"
	var stderrMod time.Time
	haveStderr := false
	if fi, err := os.Stat(stderrPath); err == nil {
		haveStderr = true
		stderrMod = fi.ModTime()
		touch(stderrPath)
		r.StderrTail = tailLines(stderrPath, 20)
	}
	for i := len(r.StderrTail) - 1; i >= 0; i-- {
		line := strings.TrimSpace(r.StderrTail[i])
		if m := stageRe.FindStringSubmatch(line); m != nil {
			r.Stage = strings.TrimPrefix(line, "exitloop: ")
			n, _ := strconv.Atoi(m[1])
			r.CurrentRound = &n
			break
		}
	}
	if r.CurrentRound == nil && len(r.Rounds) > 0 {
		n := r.Rounds[len(r.Rounds)-1].N
		r.CurrentRound = &n
	}
	r.LastModified = latest

	r.Status = runStatus(haveStderr, stderrMod, r.StderrTail, latest, now)
	return r
}

// runStatus is the liveness heuristic shared by every run kind: a stderr
// written within liveWindow is running; an older one whose last line ends in
// "..." (a stage that never finished) is stale; otherwise done. With no
// stderr, the newest file's mtime stands in for it.
func runStatus(haveStderr bool, stderrMod time.Time, tail []string, latest, now time.Time) string {
	last := ""
	if n := len(tail); n > 0 {
		last = strings.TrimSpace(tail[n-1])
	}
	fresh := haveStderr && now.Sub(stderrMod) < liveWindow
	if !haveStderr {
		fresh = !latest.IsZero() && now.Sub(latest) < liveWindow
	}
	switch {
	case fresh:
		return "running"
	case strings.HasSuffix(last, "..."):
		return "stale"
	default:
		return "done"
	}
}

// maxCountBytes bounds the jsonl files whose records are counted; a larger
// file reports only its byte size.
const maxCountBytes = 64 << 20

// parseAdhoc reads an adhoc run: path is its directory, or for a pair the
// <dir>/<base> prefix of its .stdout/.stderr files.
func (s *Scanner) parseAdhoc(path string, pair bool, now time.Time) *Run {
	r := &Run{Path: path, Kind: KindAdhoc, Rounds: []Round{}}
	latest := time.Time{}
	var stderrPath string
	var stderrMod time.Time
	consider := func(p string, fi os.FileInfo) {
		if fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
		if strings.HasSuffix(p, ".stderr") || filepath.Base(p) == "stderr" {
			if stderrPath == "" || fi.ModTime().After(stderrMod) {
				stderrPath, stderrMod = p, fi.ModTime()
			}
		}
	}
	for _, sib := range []string{path + ".stderr", path + ".stdout"} {
		if fi, err := os.Stat(sib); err == nil && !fi.IsDir() {
			consider(sib, fi)
		}
	}
	if !pair {
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(path, e.Name())
			fi, err := e.Info()
			if err != nil {
				continue
			}
			consider(p, fi)
			switch {
			case strings.HasSuffix(e.Name(), ".jsonl"):
				jf := JSONLFile{Name: e.Name(), Bytes: fi.Size()}
				if fi.Size() <= maxCountBytes {
					if v, ok := s.cachedAs(p, "jsonlcount", func(b []byte) any { return bytes.Count(b, []byte{'\n'}) }); ok {
						n := v.(int)
						jf.Records = &n
					}
				}
				r.JSONL = append(r.JSONL, jf)
			case e.Name() == "report.md":
				r.Report = p
				r.ReportTail = tailLines(p, 40)
			}
		}
	}
	if stderrPath != "" {
		r.StderrTail = tailLines(stderrPath, 20)
	}
	r.LastModified = latest
	r.Status = runStatus(stderrPath != "", stderrMod, r.StderrTail, latest, now)
	return r
}

type planInfo struct {
	rounds   *int
	klTarget *float64
}

func parsePlan(b []byte) planInfo {
	var p planInfo
	for _, m := range planGenRe.FindAllSubmatch(b, -1) {
		if n, err := strconv.Atoi(string(m[1])); err == nil && (p.rounds == nil || n > *p.rounds) {
			n := n
			p.rounds = &n
		}
	}
	if m := planKLRe.FindSubmatch(b); m != nil {
		if f, err := strconv.ParseFloat(string(m[1]), 64); err == nil && f > 0 {
			p.klTarget = &f
		}
	}
	return p
}

func parseTSV(b []byte) any {
	t := &Table{Rows: [][]string{}}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if t.Header == nil {
			t.Header = cols
			continue
		}
		t.Rows = append(t.Rows, cols)
	}
	if t.Header == nil {
		t.Header = []string{}
	}
	return t
}

// tailLines returns up to n trailing lines of path, reading at most 16 KiB.
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const max = 16 * 1024
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	off := fi.Size() - max
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if off > 0 && len(lines) > 1 {
		lines = lines[1:] // first line is probably cut
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// --- tolerant JSON extraction -------------------------------------------

func num(m map[string]any, path ...string) *float64 {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	if f, ok := cur.(float64); ok {
		return &f
	}
	return nil
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func floats(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, x := range arr {
		f, ok := x.(float64)
		if !ok {
			return nil
		}
		out = append(out, f)
	}
	return out
}

func objs(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func evalFromDoc(d map[string]any) *EvalResult {
	e := &EvalResult{
		WinRate:   num(d, "pooled", "a_win_rate"),
		MeanTurns: num(d, "pooled", "mean_turns"),
		Games:     num(d, "pooled", "games"),
	}
	if p, ok := d["pooled"].(map[string]any); ok {
		e.CI = floats(p["a_win_rate_ci"])
	}
	for _, p := range objs(d["pairs"]) {
		e.Pairs = append(e.Pairs, PairShare{Pair: str(p, "pair"), WinRate: num(p, "a_win_rate"), CI: floats(p["a_win_rate_ci"])})
	}
	return e
}

func statsFromDoc(d map[string]any) *Stats {
	s := &Stats{
		Shape:            num(d, "shape"),
		MeanAdv:          num(d, "mean_adv"),
		FinalKL:          num(d, "final", "kl"),
		FinalClip:        num(d, "final", "clip_frac"),
		ValueLogLoss:     num(d, "final", "value_log_loss"),
		InitValueLogLoss: num(d, "final", "init_value_log_loss"),
		BaseLogLoss:      num(d, "final", "base_log_loss"),
	}
	for _, k := range objs(d["by_kind"]) {
		s.Kinds = append(s.Kinds, Kind{
			Kind: str(k, "kind"), N: num(k, "n"), DeviatedPct: num(k, "deviated_pct"),
			FinalKL: num(k, "final_kl"), FinalClip: num(k, "final_clip"), FinalFlip: num(k, "final_flip"),
			GreedyFlip: num(k, "greedy_flip"), OffGreedyPct: num(k, "off_greedy_pct"),
			MeanAbsAdv: num(k, "mean_abs_adv"), MeanAdv: num(k, "mean_adv"),
		})
	}
	for _, e := range objs(d["epochs"]) {
		s.Epochs = append(s.Epochs, Epoch{
			Epoch: num(e, "epoch"), Surrogate: num(e, "surrogate"), ClipFrac: num(e, "clip_frac"),
			KL: num(e, "kl"), ValueLogLoss: num(e, "value_log_loss"),
		})
	}
	return s
}

// --- artifacts -------------------------------------------------------------

// artifactGlobs are the report locations measured under the training root.
var artifactGlobs = []string{"*/*.md", "*/*/*.md", "*/*/runs/*.md"}

func findArtifacts(root string, multi bool) []Artifact {
	seen := map[string]bool{}
	var out []Artifact
	for _, g := range artifactGlobs {
		ms, _ := filepath.Glob(filepath.Join(root, g))
		for _, p := range ms {
			if seen[p] || strings.Contains(filepath.ToSlash(p), "/scratch/") {
				continue
			}
			seen[p] = true
			fi, err := os.Stat(p)
			if err != nil || fi.IsDir() {
				continue
			}
			rel, _ := filepath.Rel(root, p)
			id := filepath.ToSlash(rel)
			if multi {
				id = filepath.Base(root) + "/" + id
			}
			out = append(out, Artifact{ID: id, Path: p, Modified: fi.ModTime(), Size: fi.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
