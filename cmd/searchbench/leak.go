package main

// searchbench leak: docs/012 §2.5's hidden-information test (E1), the
// gorge run of upstream's tools/search_bench/leak.py over the pairs
// scripts/searchbench/leak_prep.py writes. Every (pair, arm, seed) searches
// both members X and Y with the same seeds; rows go to <out>/probes.jsonl in
// job order (a restart skips the searches already there), then the report
// to <out>/leak_report.json and <out>/leak_report.md.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/searchbench"
)

// leakArms are E1's four methods, IS-MCTS (the slowest) first in job order.
var leakArms = []searchbench.SearchArm{searchbench.ArmClairvoyant, searchbench.ArmPIMC1, searchbench.ArmPIMC4, searchbench.ArmISMCTS}

// leakPairNames are docs/016 §3's display names for leak.py's probe names.
var leakPairNames = map[string]string{
	"counterspell": "counterspell", "counterspell_x": "counterspell, extreme", "cantrip": "cantrip",
	"cantrip_x": "cantrip, extreme", "decklist": "decklist", "canary": "canary",
}

var leakPairWorlds = map[string]string{
	"counterspell":   "the opponent holds Refute + Island / Island + Island",
	"counterspell_x": "Refute ×3 / Island ×3",
	"cantrip":        "the searcher's own next draw is Llanowar Elves / a Plains",
	"cantrip_x":      "its next five draws are Elves / Plains",
	"decklist":       "the opponent's hidden hand is dealt from a list with four counterspells / none",
	"canary":         "the opponent holds Counterspell ×2, a card outside FDN that no belief can deal / Island ×2",
}

func leak(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench leak", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	probesPath := fs.String("probes", "", "leak_prep.py output (probes.json.gz)")
	outDir := fs.String("out", "", "output directory")
	armsText := fs.String("arms", "", "comma-separated arms (default clairvoyant-mcts,pimc-1,pimc-4,is-mcts)")
	pairsText := fs.String("pairs", "", "comma-separated leak.py pair names (default all)")
	seeds := fs.Int("seeds", 16, "seeds per member")
	sims := fs.Int("sims", 3000, "simulations per search")
	workers := fs.Int("workers", 1, "worker goroutines")
	discount := fs.Float64("discount", 0.99, "backup discount gamma per -discount-unit (docs/012 default 0.99 per ply)")
	unitText := fs.String("discount-unit", "ply", "discount unit: ply, action or turn")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	nodeCache := fs.Int("node-cache", azmcts.DefaultNodeCache, "fixed-world tree node cache (0: off)")
	gcPercent := fs.Int("gc-percent", 0, "runtime GC percent (0: leave GOGC; negative: off)")
	memLimit := fs.String("mem-limit", "", "runtime soft memory limit, e.g. 1500MiB")
	analyzeOnly := fs.Bool("analyze", false, "only analyse <out>/probes.jsonl")
	if err := fs.Parse(args); err != nil || *outDir == "" || (*probesPath == "" && !*analyzeOnly) || *seeds < 1 || *sims < 1 || *workers < 1 || *discount < 0 || *discount > 1 || fs.NArg() != 0 {
		return usage()
	}
	rowsPath := filepath.Join(*outDir, "probes.jsonl")
	if *analyzeOnly {
		return leakReport(rowsPath, *outDir, nil, out)
	}
	unit, err := azmcts.ParseDiscountUnit(*unitText)
	if err != nil {
		return err
	}
	arms := leakArms
	if *armsText != "" {
		arms = nil
		for _, a := range strings.Split(*armsText, ",") {
			arm, err := searchbench.ParseArm(a)
			if err != nil {
				return err
			}
			if arm == searchbench.ArmNoSearch {
				return errors.New("searchbench: leak probes search; no-search has no root")
			}
			arms = append(arms, arm)
		}
	}
	if *gcPercent != 0 {
		debug.SetGCPercent(*gcPercent)
	}
	if *memLimit != "" {
		n, err := parseBytes(*memLimit)
		if err != nil {
			return err
		}
		debug.SetMemoryLimit(n)
	}
	lf, err := searchbench.ReadLeakFile(*probesPath)
	if err != nil {
		return err
	}
	if *seeds > lf.Seeds {
		return fmt.Errorf("searchbench: %s holds %d seeds, -seeds %d", *probesPath, lf.Seeds, *seeds)
	}
	var pairs []*searchbench.LeakPair
	var order []string
	for i := range lf.Pairs {
		p := &lf.Pairs[i]
		order = append(order, p.Name)
		if *pairsText == "" || containsComma(*pairsText, p.Name) {
			pairs = append(pairs, p)
		}
	}
	if len(pairs) == 0 {
		return fmt.Errorf("searchbench: no pair matches -pairs %q", *pairsText)
	}
	for _, a := range arms {
		if a == searchbench.ArmClairvoyant {
			clairvoyant.AllowClairvoyant() // the measurement command's explicit opt-in
		}
	}
	names, err := searchbench.LeakCardNames(lf)
	if err != nil {
		return err
	}
	reg, err := cards.OpenCorpusFor(*corpus, names)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	cfg := searchbench.LeakConfig{Sims: *sims, Discount: *discount, DiscountUnit: unit, NodeCache: *nodeCache}
	type job struct {
		pair  *searchbench.LeakPair
		world string
		arm   searchbench.SearchArm
		seed  int
	}
	done := map[string]bool{}
	jobKey := func(pair, world, arm string, seed, sims int, disc float64, unit string) string {
		return fmt.Sprintf("%s|%s|%s|%d|%d|%g|%s", pair, world, arm, seed, sims, disc, unit)
	}
	if rows, err := readLeakRows(rowsPath); err == nil {
		for _, r := range rows {
			done[jobKey(r.Pair, r.World, r.Arm, r.Seed, r.Sims, r.Discount, r.Unit)] = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var jobs []job
	for ai := len(arms) - 1; ai >= 0; ai-- { // IS-MCTS (listed last) first: the slowest
		for _, p := range pairs {
			for s := 0; s < *seeds; s++ {
				for _, w := range []string{"X", "Y"} {
					if !done[jobKey(p.Name, w, string(arms[ai]), s, *sims, *discount, unit.String())] {
						jobs = append(jobs, job{p, w, arms[ai], s})
					}
				}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "searchbench leak: %d searches (%d done), %d workers\n", len(jobs), len(done), *workers)
	f, err := os.OpenFile(rowsPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	type slot struct {
		r   searchbench.LeakRow
		err error
	}
	results := make([]chan slot, len(jobs))
	for i := range results {
		results[i] = make(chan slot, 1)
	}
	next := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for k := 0; k < *workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				j := jobs[i]
				ts := time.Now()
				r, err := runLeakGuarded(ctx, reg, j.pair, j.world, j.arm, j.seed, cfg)
				r.CoreSeconds = math.Round(time.Since(ts).Seconds()*1e4) / 1e4
				results[i] <- slot{r, err}
			}
		}()
	}
	go func() {
		defer close(next)
		for i := range jobs {
			select {
			case next <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	t0 := time.Now()
	var firstErr error
	for i := range jobs {
		s := <-results[i]
		if s.err != nil {
			firstErr = s.err
			break
		}
		if err := enc.Encode(s.r); err != nil {
			firstErr = err
			break
		}
		if err := w.Flush(); err != nil {
			firstErr = err
			break
		}
		if (i+1)%32 == 0 {
			fmt.Fprintf(os.Stderr, "  %d/%d  %.0fs\n", i+1, len(jobs), time.Since(t0).Seconds())
		}
	}
	cancel()
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return leakReport(rowsPath, *outDir, order, out)
}

func containsComma(list, v string) bool {
	for _, x := range strings.Split(list, ",") {
		if x == v {
			return true
		}
	}
	return false
}

func runLeakGuarded(ctx context.Context, reg *cards.Registry, p *searchbench.LeakPair, world string, arm searchbench.SearchArm, seed int, cfg searchbench.LeakConfig) (r searchbench.LeakRow, err error) {
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("searchbench: %s/%s seed %d %s: panic: %v", p.Name, world, seed, arm, x)
		}
	}()
	return searchbench.RunLeak(ctx, reg, p, world, arm, seed, cfg)
}

func readLeakRows(path string) ([]searchbench.LeakRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []searchbench.LeakRow
	dec := json.NewDecoder(bufio.NewReader(f))
	dec.DisallowUnknownFields()
	for {
		var r searchbench.LeakRow
		if err := dec.Decode(&r); errors.Is(err, io.EOF) {
			return rows, nil
		} else if err != nil {
			return nil, fmt.Errorf("searchbench: %s: %w", path, err)
		}
		rows = append(rows, r)
	}
}

// leakReport analyses rowsPath into leak_report.json and leak_report.md.
func leakReport(rowsPath, outDir string, pairOrder []string, out io.Writer) error {
	rows, err := readLeakRows(rowsPath)
	if err != nil {
		return err
	}
	if pairOrder == nil {
		pairOrder = []string{"counterspell", "counterspell_x", "cantrip", "cantrip_x", "decklist", "canary"}
	}
	armOrder := make([]string, len(leakArms))
	for i, a := range leakArms {
		armOrder[i] = string(a)
	}
	vs := searchbench.AnalyzeLeak(rows, pairOrder, armOrder)
	b, err := json.MarshalIndent(vs, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "leak_report.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	md := leakMarkdown(vs, rows)
	if err := os.WriteFile(filepath.Join(outDir, "leak_report.md"), []byte(md), 0o644); err != nil {
		return err
	}
	_, err = io.WriteString(out, md)
	return err
}

func fmtChosen(c map[string]int, seeds int) string {
	type kv struct {
		k string
		v int
	}
	var xs []kv
	for k, v := range c {
		xs = append(xs, kv{k, v})
	}
	// most chosen first, then by label
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && (xs[j].v > xs[j-1].v || xs[j].v == xs[j-1].v && xs[j].k < xs[j-1].k); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
	var parts []string
	for _, x := range xs {
		parts = append(parts, fmt.Sprintf("%s %d of %d", x.k, x.v, seeds))
	}
	return strings.Join(parts, ", ")
}

func leakMarkdown(vs []searchbench.LeakVerdict, rows []searchbench.LeakRow) string {
	var b strings.Builder
	sims, disc, unit := 0, 0.0, ""
	if len(rows) > 0 {
		sims, disc, unit = rows[0].Sims, rows[0].Discount, rows[0].Unit
	}
	fmt.Fprintf(&b, "# E1: the hidden-information test (gorge)\n\n")
	fmt.Fprintf(&b, "`searchbench leak`, heuristic leaf, %d simulations, discount %g per %s, seeds paired across the two worlds of a pair. ", sims, disc, unit)
	b.WriteString("ΔQ is X − Y on upstream's [−1, 1] scale (2 × gorge's [0, 1] difference); the rule (docs/012 §2.5): every option's paired ΔQ 95% CI inside ±0.1 and the chosen actions not different (permutation test, p > 0.01); for the canary, no search of either world may walk a world holding Counterspell.\n\n")
	b.WriteString("| Pair | World X / world Y | Method | Worst option's ΔQ (X − Y), 95% CI | max abs ΔQ | Choices X / Y | p | Identical seeds | Canary hits X / Y | Verdict |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, v := range vs {
		w := searchbench.LeakOptionStat{CI: &[2]float64{math.Inf(-1), math.Inf(1)}}
		maxAbs := 0.0
		for _, o := range v.Options {
			if o.Label == v.Worst {
				w.Label, w.DQ = o.Label, o.DQ
				if o.CI != nil {
					w.CI = o.CI
				}
			}
			maxAbs = math.Max(maxAbs, o.MaxAbs)
		}
		canary := "—"
		if v.CanaryX != nil {
			canary = fmt.Sprintf("%d / %d", *v.CanaryX, *v.CanaryY)
		}
		verdict := "fails"
		if v.Pass {
			verdict = "passes"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s %+.3f [%+.3f, %+.3f] | %.3f | %s / %s | %.4g | %d of %d | %s | %s |\n",
			leakPairNames[v.Pair], leakPairWorlds[v.Pair], v.Arm, v.Worst, w.DQ, w.CI[0], w.CI[1], maxAbs,
			fmtChosen(v.ChosenX, v.Seeds), fmtChosen(v.ChosenY, v.Seeds), v.PermP, v.Identical, v.Seeds, canary, verdict)
	}
	b.WriteString("\nPer method:")
	verdict := map[string]bool{}
	var arms []string
	for _, v := range vs {
		if _, ok := verdict[v.Arm]; !ok {
			verdict[v.Arm] = true
			arms = append(arms, v.Arm)
		}
		verdict[v.Arm] = verdict[v.Arm] && v.Pass
	}
	for _, a := range arms {
		s := "fails"
		if verdict[a] {
			s = "passes"
		}
		fmt.Fprintf(&b, " %s %s;", a, s)
	}
	b.WriteString("\n")
	return b.String()
}
