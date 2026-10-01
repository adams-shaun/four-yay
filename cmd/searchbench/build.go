package main

// searchbench build: the Go port of upstream items.py cmd_build. The Python
// half of make_item runs in `prep.py serve` workers; the selection loop and
// every engine check run here (internal/searchbench Build, PrepareItem).

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/searchbench"
)

const (
	defaultPrepDir = "/mnt/sata/gorge-training/searchbench/sb-v1/prep"
	defaultPython  = "/mnt/sata/gorge-training/searchbench/venv/bin/python"
	datasetName    = "17lands FDN PremierDraft public replay data"
	datasetLicense = "CC BY 4.0"
	datasetURI     = "https://17lands-public.s3.amazonaws.com/analysis_data/replay_data/replay_data_public.FDN.PremierDraft.csv.gz"
)

type gamesHeader struct {
	Format  string `json:"format"`
	Version string `json:"version"`
	Quota   map[searchbench.Split]map[searchbench.DecisionType]int
	Inputs  map[string]struct {
		Name   string  `json:"name"`
		SHA256 *string `json:"sha256"`
	} `json:"inputs"`
	RowsFile struct {
		Name string `json:"name"`
	} `json:"rows_file"`
	Upstream struct {
		Head string `json:"head"`
	} `json:"upstream"`
}

// readGames reads the prep games file: its header and the games in order.
func readGames(path string) (gamesHeader, []searchbench.BuildGame, error) {
	var h gamesHeader
	f, err := os.Open(path)
	if err != nil {
		return h, nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return h, nil, err
	}
	var wrap struct {
		Header gamesHeader `json:"header"`
	}
	if err := json.Unmarshal(line, &wrap); err != nil {
		return h, nil, fmt.Errorf("%s header: %w", path, err)
	}
	h = wrap.Header
	if h.Format != "sbrep-prep/v1" {
		return h, nil, fmt.Errorf("%s: format %q, want sbrep-prep/v1", path, h.Format)
	}
	var games []searchbench.BuildGame
	dec := json.NewDecoder(r)
	for {
		var g struct {
			Index      int     `json:"index"`
			Row        int     `json:"row"`
			Split      string  `json:"split"`
			Partner    *int    `json:"partner"`
			Candidates [][]any `json:"candidates"`
		}
		if err := dec.Decode(&g); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return h, nil, fmt.Errorf("%s: %w", path, err)
		}
		bg := searchbench.BuildGame{Index: g.Index, Row: g.Row, Split: searchbench.Split(g.Split), Partner: g.Partner}
		if g.Index != len(games) {
			return h, nil, fmt.Errorf("%s: game %d has index %d", path, len(games), g.Index)
		}
		for _, c := range g.Candidates {
			if len(c) != 2 {
				return h, nil, fmt.Errorf("%s: row %d: bad candidate %v", path, g.Row, c)
			}
			n, ok1 := c[0].(float64)
			k, ok2 := c[1].(string)
			if !ok1 || !ok2 {
				return h, nil, fmt.Errorf("%s: row %d: bad candidate %v", path, g.Row, c)
			}
			bg.Candidates = append(bg.Candidates, searchbench.BuildCandidate{Turn: int(n), Kind: searchbench.DecisionType(k)})
		}
		games = append(games, bg)
	}
	return h, games, nil
}

// readDraftDigests maps each row of the prep rows file to a digest of its
// 17lands draft_id (the raw id is never written).
func readDraftDigests(path string) (map[int]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bufio.NewReaderSize(zr, 1<<20))
	var head struct {
		Header string `json:"header"`
	}
	if err := dec.Decode(&head); err != nil {
		return nil, err
	}
	hdr, err := csv.NewReader(strings.NewReader(head.Header)).Read()
	if err != nil {
		return nil, err
	}
	col := -1
	for i, h := range hdr {
		if h == "draft_id" {
			col = i
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("%s: no draft_id column", path)
	}
	out := map[int]string{}
	for {
		var row struct {
			Row  int    `json:"row"`
			Line string `json:"line"`
		}
		if err := dec.Decode(&row); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		rec, err := csv.NewReader(strings.NewReader(row.Line)).Read()
		if err != nil || col >= len(rec) {
			return nil, fmt.Errorf("%s row %d: %v", path, row.Row, err)
		}
		out[row.Row] = draftDigest(rec[col])
	}
}

func draftDigest(id string) string {
	b, _ := json.Marshal(id)
	return fmt.Sprintf("%x", sha256Sum(append([]byte("17lands-draft:"), b...)))[:16]
}

// prepWorker is one `prep.py serve` process.
type prepWorker struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	log *os.File
}

func startPrep(python, script, upstream, games, rows string, lru int, logPath string) (*prepWorker, error) {
	args := []string{script}
	if upstream != "" {
		args = append(args, "--upstream", upstream)
	}
	args = append(args, "serve", "--games", games, "--lru", fmt.Sprint(lru), "--warm")
	if rows != "" {
		args = append(args, "--rows", rows)
	}
	cmd := exec.Command(python, args...)
	lf, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd.Stderr = lf
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &prepWorker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20), log: lf}, nil
}

func (p *prepWorker) request(row, turn int, kind string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q, _ := json.Marshal(map[string]any{"row": row, "turn": turn, "kind": kind})
	if _, err := p.in.Write(append(q, '\n')); err != nil {
		return nil, fmt.Errorf("prep worker: %w", err)
	}
	line, err := p.out.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("prep worker: %w (see %s)", err, p.log.Name())
	}
	return line, nil
}

func (p *prepWorker) close() {
	_ = p.in.Close()
	_ = p.cmd.Wait()
	_ = p.log.Close()
}

// orderedCounts marshals as a JSON object in slice order (upstream's
// Counter.most_common order).
type orderedCounts []struct {
	K string
	N int
}

func (o orderedCounts) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.K)
		b.Write(k)
		fmt.Fprintf(&b, ":%d", kv.N)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func mostCommon(m map[string]int) orderedCounts {
	var out orderedCounts
	for k, n := range m {
		out = append(out, struct {
			K string
			N int
		}{k, n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N > out[j].N || out[i].N == out[j].N && out[i].K < out[j].K })
	return out
}

func forgeRef(corpus string) (string, error) {
	b, err := os.ReadFile(filepath.Join(corpus, "cards.lock"))
	if err != nil {
		return "", err
	}
	var lock struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(b, &lock); err != nil {
		return "", err
	}
	return lock.Commit, nil
}

func build(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	gamesPath := fs.String("games", filepath.Join(defaultPrepDir, "games.jsonl"), "prep.py games file")
	rowsPath := fs.String("rows", "", "prep.py rows file (default: the games header's rows_file next to -games)")
	outDir := fs.String("out", "", "output directory")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	python := fs.String("python", defaultPython, "python with upstream's requirements")
	script := fs.String("prep", "scripts/searchbench/prep.py", "prep.py")
	upstream := fs.String("upstream", "", "pinned draft-zero clone (prep.py's default when empty)")
	workers := fs.Int("workers", 4, "prep.py serve workers")
	lru := fs.Int("lru", 4, "parsed games each prep worker caches")
	maxGames := fs.Int("max-games", 0, "scan only the first N games (a smoke build; 0: all)")
	quotaScale := fs.Float64("quota-scale", 1, "scale every quota (a smoke build; rounded up)")
	if err := fs.Parse(args); err != nil || *outDir == "" || *workers < 1 || *maxGames < 0 || *quotaScale <= 0 || fs.NArg() != 0 {
		return usage()
	}
	t0 := time.Now()
	hdr, games, err := readGames(*gamesPath)
	if err != nil {
		return err
	}
	if *rowsPath == "" {
		*rowsPath = filepath.Join(filepath.Dir(*gamesPath), hdr.RowsFile.Name)
	}
	drafts, err := readDraftDigests(*rowsPath)
	if err != nil {
		return err
	}
	for i := range games {
		d, ok := drafts[games[i].Row]
		if !ok {
			return fmt.Errorf("searchbench: row %d is not in %s", games[i].Row, *rowsPath)
		}
		games[i].DraftID = d
	}
	full := *maxGames == 0 && *quotaScale == 1
	if *maxGames > 0 && *maxGames < len(games) {
		games = games[:*maxGames]
	}
	quota := map[searchbench.Split]map[searchbench.DecisionType]int{}
	for sp, q := range searchbench.SBV1Quotas {
		quota[sp] = map[searchbench.DecisionType]int{}
		for k, n := range q {
			if hdr.Quota[sp][k] != n {
				return fmt.Errorf("searchbench: games file quota %s %s = %d, sb-v1 says %d", sp, k, hdr.Quota[sp][k], n)
			}
			quota[sp][k] = int(math.Ceil(float64(n) * *quotaScale))
		}
	}
	ref, err := forgeRef(*corpus)
	if err != nil {
		return err
	}
	replaySHA := ""
	if in, ok := hdr.Inputs["replay"]; ok && in.SHA256 != nil {
		replaySHA = *in.SHA256
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	tReg := time.Since(t0)
	pool := make([]*prepWorker, *workers)
	for i := range pool {
		if pool[i], err = startPrep(*python, *script, *upstream, *gamesPath, *rowsPath, *lru, filepath.Join(*outDir, fmt.Sprintf("prep-worker-%d.log", i))); err != nil {
			return err
		}
		defer pool[i].close()
	}
	seeds := searchbench.SBV1WorldSeeds()
	eval := func(w int, g *searchbench.BuildGame, c searchbench.BuildCandidate) (searchbench.Outcome, error) {
		ts := time.Now()
		line, err := pool[w].request(g.Row, c.Turn, string(c.Kind))
		if err != nil {
			return searchbench.Outcome{}, err
		}
		var p searchbench.Payload
		if err := json.Unmarshal(line, &p); err != nil {
			return searchbench.Outcome{}, fmt.Errorf("prep response: %w", err)
		}
		o := searchbench.Outcome{PythonSeconds: time.Since(ts).Seconds()}
		if p.Row != g.Row || p.Turn != c.Turn || p.Kind != string(c.Kind) {
			return o, fmt.Errorf("prep answered row %d turn %d %s", p.Row, p.Turn, p.Kind)
		}
		switch {
		case p.Reject != "":
			o.Side, o.Key, o.Detail = "python", p.Reject, p.Reject
			return o, nil
		case p.Error != "":
			o.Side, o.Key, o.Detail = "python", "error "+p.Error, p.Message
			return o, nil
		}
		if p.Split != string(g.Split) {
			return o, fmt.Errorf("prep split %q, games file %q", p.Split, g.Split)
		}
		tg := time.Now()
		it, err := prepareGuarded(reg, &p, seeds)
		o.GorgeSeconds = time.Since(tg).Seconds()
		var ir *searchbench.ItemRefusal
		switch {
		case errors.As(err, &ir):
			o.Side, o.Key, o.Detail = "gorge", ir.Key(), ir.Error()
		case err != nil:
			o.Side, o.Key, o.Detail = "gorge", "error", err.Error()
			if strings.HasPrefix(err.Error(), "panic") {
				o.Key = "error panic"
			}
		default:
			o.Item = it
		}
		return o, nil
	}
	fmt.Fprintf(os.Stderr, "%d games, %d prep workers, registry %.1fs\n", len(games), *workers, tReg.Seconds())
	res, err := searchbench.Build(games, searchbench.BuildConfig{Quota: quota, MaxPerGame: searchbench.SBV1Selection.MaximumItemsPerGame, Workers: *workers,
		Progress: func(n, items int, counts map[searchbench.Split]map[searchbench.DecisionType]int) {
			if n%25 == 0 {
				fmt.Fprintf(os.Stderr, "  %d/%d games  %d items  test %v  dev %v  (%.0fs)\n", n, len(games), items, counts[searchbench.SplitTest], counts[searchbench.SplitDev], time.Since(t0).Seconds())
			}
		}}, eval)
	if err != nil {
		return err
	}
	version := "sb-v1"
	if !full {
		version = "sb-v1-smoke"
	}
	searchbench.AssignIDs(res.Items, version)
	m := searchbench.Manifest{Kind: searchbench.ManifestKind, SchemaVersion: searchbench.ManifestSchemaVersion,
		Dataset:   searchbench.Dataset{Name: datasetName, License: datasetLicense, URI: datasetURI, SHA256: replaySHA},
		Corpus:    searchbench.Corpus{ForgeRef: ref, CompilerFingerprint: cards.CompilerFingerprint},
		Selection: searchbench.Selection{MinimumGameWinRate: searchbench.SBV1Selection.MinimumGameWinRate, MinimumGames: searchbench.SBV1Selection.MinimumGames, MaximumItemsPerGame: searchbench.SBV1Selection.MaximumItemsPerGame},
	}
	for _, it := range res.Items {
		m.Items = append(m.Items, it.ManifestItem(fmt.Sprintf("17lands-fdn-row-%d", it.Row), drafts[it.Row]))
		if it.Split == searchbench.SplitDev {
			m.Selection.Dev++
		} else {
			m.Selection.Test++
		}
	}
	if err := m.Seal(); err != nil {
		return fmt.Errorf("searchbench: sealing the manifest: %w", err)
	}
	sbv1 := "ok"
	if err := m.ValidateSBV1(); err != nil {
		sbv1 = err.Error()
	}
	if err := writeJSON(filepath.Join(*outDir, "manifest.json"), m); err != nil {
		return err
	}
	storePath := filepath.Join(*outDir, "items.jsonl.gz")
	if err := searchbench.WriteStore(storePath, res.Items); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(*outDir, "rejections.jsonl"), res.Rejections); err != nil {
		return err
	}
	sides := map[string]int{}
	for _, r := range res.Rejections {
		sides[r.Side]++
	}
	storeSHA, err := fileSHA(storePath)
	if err != nil {
		return err
	}
	meta := map[string]any{
		"version": version, "counts": res.Counts, "quota": quota, "done": res.Done,
		"games_total": res.GamesTotal, "games_scanned": res.GamesUsed, "evaluations": res.Evaluations,
		"speculative_unused": res.Speculative, "rejections": mostCommon(searchbench.RejectionSummary(res.Rejections)),
		"rejections_by_side": sides, "manifest_digest": m.Digest, "sbv1": sbv1, "items_sha256": storeSHA,
		"forge_ref": ref, "compiler_fingerprint": cards.CompilerFingerprint, "upstream_head": hdr.Upstream.Head,
		"workers": *workers, "max_games": *maxGames, "quota_scale": *quotaScale,
		"seconds": map[string]float64{"wall": round1(time.Since(t0).Seconds()), "registry": round1(tReg.Seconds()),
			"python_consumed": round1(res.PythonSeconds), "gorge_consumed": round1(res.GorgeSeconds)},
	}
	if err := writeJSON(filepath.Join(*outDir, "build.json"), meta); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "items=%d test=%v dev=%v games_scanned=%d/%d evaluations=%d rejections=%d done=%v sbv1=%s digest=%s wall=%.0fs\n",
		len(res.Items), res.Counts[searchbench.SplitTest], res.Counts[searchbench.SplitDev], res.GamesUsed, res.GamesTotal, res.Evaluations, len(res.Rejections), res.Done, sbv1, m.Digest, time.Since(t0).Seconds())
	return err
}

// prepareGuarded is PrepareItem with an engine panic turned into a gorge
// rejection, so one bad position does not stop the build (upstream's
// "error <ExcType>").
func prepareGuarded(reg *cards.Registry, p *searchbench.Payload, seeds []uint64) (it *searchbench.StoreItem, err error) {
	defer func() {
		if r := recover(); r != nil {
			it, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return searchbench.PrepareItem(reg, p, seeds)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func writeLines[T any](path string, rows []T) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func sha256Sum(b []byte) [32]byte { return sha256.Sum256(b) }

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
