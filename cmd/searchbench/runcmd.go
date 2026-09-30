package main

// searchbench run: one arm over a manifest split, one process, one pool of
// worker goroutines sharing one card registry. Rows are written in manifest
// order whatever the worker count, and a restart skips the items -out
// already holds.
//
// CoreSeconds is the wall time of the item on its worker goroutine,
// measured here at the command boundary: from before the item's engines
// are rebuilt from the store to after its result is projected. Each worker
// runs one item at a time on one goroutine and RunArm is single-threaded,
// so with -workers no larger than the cores available it is the item's
// core time; the garbage collector's background work is not attributed.
// It is the analogue of upstream's per-decision wall_s on one bridge
// worker, not of pod-seconds.

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
	"strconv"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/searchbench"
)

// runName is the result file's arm label: upstream's run-id convention
// (method, budget, discount and unit unless the default ply, leaf tag, seed
// unless 0), with gorge's arm names.
func runName(arm searchbench.SearchArm, sims int, discount float64, unit azmcts.DiscountUnit, leaf string, seed uint64) string {
	if arm == searchbench.ArmNoSearch {
		return string(arm)
	}
	s := fmt.Sprintf("%s-b%d", arm, sims)
	if discount > 0 && discount < 1 {
		s += "-d" + strconv.FormatFloat(discount, 'g', -1, 64)
		if unit != azmcts.DiscountPly {
			s += "-" + unit.String()
		}
	}
	if leaf != "" && leaf != "heuristic" {
		s += "-net"
	}
	if seed != 0 {
		s += fmt.Sprintf("-s%d", seed)
	}
	return s
}

func runArm(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifestPath := fs.String("manifest", "", "sealed manifest")
	storePath := fs.String("store", "", "item store (items.jsonl.gz)")
	armText := fs.String("arm", "", "no-search, clairvoyant-mcts, pimc-1, pimc-4 or is-mcts")
	sims := fs.Int("sims", 0, "simulations per decision")
	split := fs.String("split", string(searchbench.SplitTest), "split: test or dev")
	workers := fs.Int("workers", 1, "worker goroutines")
	seed := fs.Uint64("seed", 0, "run seed (each item's search seed is derived from it and the item ID)")
	leaf := fs.String("leaf", "heuristic", "leaf evaluator: heuristic, or a policynet value checkpoint")
	discount := fs.Float64("discount", 1, "backup discount gamma (1: off)")
	unitText := fs.String("discount-unit", "ply", "discount unit: ply, action or turn")
	name := fs.String("name", "", "result arm label (default: from the arm, budget, discount, leaf and seed)")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	limit := fs.Int("limit", 0, "answer only the first N items of the split (0: all)")
	outPath := fs.String("out", "", "result JSONL (appended; items already in it are skipped)")
	if err := fs.Parse(args); err != nil || *manifestPath == "" || *storePath == "" || *armText == "" || *outPath == "" || *workers < 1 || *sims < 0 || *limit < 0 || *discount < 0 || *discount > 1 || fs.NArg() != 0 {
		return usage()
	}
	arm, err := searchbench.ParseArm(*armText)
	if err != nil {
		return err
	}
	if arm != searchbench.ArmNoSearch && *sims < 1 {
		return fmt.Errorf("searchbench: %s needs -sims >= 1", arm)
	}
	unit, err := azmcts.ParseDiscountUnit(*unitText)
	if err != nil {
		return err
	}
	if arm == searchbench.ArmClairvoyant {
		clairvoyant.AllowClairvoyant() // the measurement command's explicit opt-in
	}
	t0 := time.Now()
	m, err := searchbench.Read(*manifestPath)
	if err != nil {
		return err
	}
	store, err := searchbench.ReadStore(*storePath)
	if err != nil {
		return err
	}
	items := searchbench.SplitItems(m, searchbench.Split(*split))
	if len(items) == 0 {
		return fmt.Errorf("searchbench: manifest has no %s items", *split)
	}
	if *limit > 0 && *limit < len(items) {
		items = items[:*limit]
	}
	for _, it := range items {
		if _, ok := store[it.ID]; !ok {
			return fmt.Errorf("searchbench: item %s is not in %s", it.ID, *storePath)
		}
	}
	label := *name
	if label == "" {
		label = runName(arm, *sims, *discount, unit, *leaf, *seed)
	}
	cfg := searchbench.RunConfig{Arm: arm, Name: label, Sims: *sims, Discount: *discount, DiscountUnit: unit, Seed: *seed, Digest: m.Digest}
	if *leaf != "heuristic" {
		if cfg.Net, err = searchbench.LoadLeafNet(*leaf); err != nil {
			return err
		}
	}
	done, err := resumeSet(*outPath, m.Digest, label)
	if err != nil {
		return err
	}
	var todo []searchbench.Item
	for _, it := range items {
		if !done[it.ID] {
			todo = append(todo, it)
		}
	}
	if len(todo) == 0 {
		_, err := fmt.Fprintf(out, "%s: %d %s items done already\n", label, len(items), *split)
		return err
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	tLoad := time.Since(t0)
	f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)

	type slot struct {
		r   searchbench.Result
		err error
	}
	results := make([]chan slot, len(todo))
	for i := range results {
		results[i] = make(chan slot, 1)
	}
	jobs := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for k := 0; k < *workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				it := todo[i]
				ts := time.Now()
				r, err := runGuarded(ctx, reg, it, store[it.ID], cfg)
				r.CoreSeconds = math.Round(time.Since(ts).Seconds()*1e4) / 1e4
				results[i] <- slot{r, err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range todo {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	tRun := time.Now()
	var core float64
	var firstErr error
	fallbacks := map[string]int{}
	for i := range todo {
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
		core += s.r.CoreSeconds
		if s.r.Fallback != "" {
			fallbacks[s.r.Fallback]++
		}
		if (i+1)%50 == 0 {
			el := time.Since(tRun).Seconds()
			fmt.Fprintf(os.Stderr, "[%s] %d/%d  %.0fs  %.1f items/min  %.3f core-s/decision\n", label, i+1, len(todo), el, float64(i+1)/el*60, core/float64(i+1))
		}
	}
	cancel()
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	el := time.Since(tRun).Seconds()
	_, err = fmt.Fprintf(out, "%s: %d %s items (%d skipped as done) in %.1fs, load %.1fs, %.1f items/min, %.3f core-s/decision, workers %d, fallbacks %v\n",
		label, len(todo), *split, len(items)-len(todo), el, tLoad.Seconds(), float64(len(todo))/el*60, core/float64(len(todo)), *workers, fallbacks)
	return err
}

// runGuarded is RunItem with an engine panic turned into an error.
func runGuarded(ctx context.Context, reg *cards.Registry, it searchbench.Item, s *searchbench.StoreItem, cfg searchbench.RunConfig) (r searchbench.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("searchbench: %s: panic: %v", it.ID, p)
		}
	}()
	return searchbench.RunItem(ctx, reg, it, s, cfg)
}

// resumeSet reads an existing result file: the item IDs it answers. Every
// row must name this manifest and this arm label.
func resumeSet(path, digest, label string) (map[string]bool, error) {
	done := map[string]bool{}
	rows, err := searchbench.ReadResults(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ManifestDigest != digest || r.Arm != label {
			return nil, fmt.Errorf("searchbench: %s holds %s rows for manifest %s; this run is %s on %s", path, r.Arm, r.ManifestDigest, label, digest)
		}
		done[r.ItemID] = true
	}
	return done, nil
}
