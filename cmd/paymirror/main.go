// Command paymirror plays auto-pay bot games over the repo decks and mirror-
// checks every planned cast: at each priority decision where a bot submits a
// payment-plan intent, a pre-submit clone reproduces the same cast manually
// (floating each planned source through the ordinary priority "activate"
// option, then casting and replaying the planned run's follow-up answers) and
// the two engines are compared for state equivalence
// (internal/paymirror). It prints the planned casts checked, how many were
// equivalent, the mismatches grouped by signature and the unmirrorable casts
// grouped by reason, and writes every non-equivalent report as JSONL.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/paymirror"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func main() {
	dir := flag.String("dir", ".cards", "corpus directory (holds ir.gob.gz / cardsfolder)")
	games := flag.Int("games", 10, "games per (format, seat count) configuration")
	seed := flag.Uint64("seed", 1, "base seed; configuration c game g plays seed+g (decks picked from the seed)")
	seatsFlag := flag.String("seats", "2,4", "comma-separated seat counts")
	formats := flag.String("formats", "constructed,commander", "comma-separated formats: constructed, commander, random (two-colour random corpus decks)")
	policy := flag.String("policy", "bot", "auto-pay bot policy: bot or lethal")
	workers := flag.Int("workers", 4, "games played in parallel (results are deterministic regardless)")
	control := flag.Bool("control", true, "also replay run A on a clone and compare it with the live engine (Clone fidelity control)")
	maxTurns := flag.Int("max-turns", 60, "turn cap per game")
	maxIntents := flag.Int("max-intents", 20000, "intent cap per game")
	out := flag.String("out", "", "output directory for findings.jsonl and summary.txt (empty = stdout summary only)")
	specFile := flag.String("spec", "", "play exactly one game from a GameSpec JSON file (a findings.jsonl record's \"spec\", random-deck lists included)")
	decksFlag := flag.String("decks", "", "play exactly one game with these comma-separated repo decks at -seed (overrides -games/-seats/-formats deck picking; -formats names the one format)")
	trace := flag.Bool("trace", false, "record full event streams in every report (use with -decks for one game)")
	progress := flag.Bool("progress", false, "print one stderr line per finished game (wall time is diagnostic only; never part of the results)")
	maxObjects := flag.Int("max-objects", 2500, "end a game whose object arena passes this size (runaway boards make clones and diffs expensive)")
	budget := flag.Duration("budget", 0, "wall-time budget per game, checked between intents; 0 = none. A harness bound only: it can truncate a game, never change a verdict")
	resolve := flag.Bool("resolve", true, "also drive both sides of every equivalent route (deterministic passer) until the planned spell leaves the stack, and compare again")
	flag.Parse()

	if err := run(*dir, *games, *seed, *seatsFlag, *formats, *policy, *workers, *control, *maxTurns, *maxIntents, *out, *decksFlag, *specFile, *trace, *resolve, *progress, *maxObjects, *budget, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "paymirror:", err)
		os.Exit(1)
	}
}

func run(dir string, games int, seed uint64, seatsFlag, formats, policy string, workers int, control bool,
	maxTurns, maxIntents int, out, decksFlag, specFile string, trace, resolve, progress bool, maxObjects int, budget time.Duration, stdout io.Writer) error {
	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		return fmt.Errorf("opening corpus at %s: %w", dir, err)
	}
	decks, err := paymirror.LoadDecks(reg)
	if err != nil {
		return err
	}
	var specs []paymirror.GameSpec
	var pool *paymirror.RandomPool
	if specFile != "" {
		raw, err := os.ReadFile(specFile)
		if err != nil {
			return err
		}
		var sp paymirror.GameSpec
		if err := json.Unmarshal(raw, &sp); err != nil {
			return fmt.Errorf("-spec %s: %w", specFile, err)
		}
		specs = append(specs, sp)
		decksFlag = "-" // one game only: skip the generated specs below
	} else if decksFlag != "" {
		names := strings.Split(decksFlag, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		specs = append(specs, paymirror.GameSpec{Seed: seed, Decks: names, Commander: strings.TrimSpace(formats) == "commander", Policy: policy})
	}
	for _, f := range strings.Split(formats, ",") {
		if decksFlag != "" {
			break
		}
		f = strings.TrimSpace(f)
		if f != "constructed" && f != "commander" && f != "random" {
			return fmt.Errorf("unknown format %q", f)
		}
		for _, sv := range strings.Split(seatsFlag, ",") {
			seats, err := strconv.Atoi(strings.TrimSpace(sv))
			if err != nil || seats < 2 {
				return fmt.Errorf("bad seat count %q", sv)
			}
			for g := 0; g < games; g++ {
				s := seed + uint64(g)
				if f == "random" {
					if pool == nil {
						pool = paymirror.NewRandomPool(reg)
					}
					spec := paymirror.GameSpec{Seed: s, Policy: policy}
					for i := 0; i < seats; i++ {
						label, list := pool.Generate(s*7919 + uint64(seats)*131 + uint64(i))
						spec.Decks = append(spec.Decks, label)
						spec.Lists = append(spec.Lists, list)
					}
					specs = append(specs, spec)
					continue
				}
				specs = append(specs, paymirror.GameSpec{Seed: s, Decks: decks.Pick(s*31+uint64(seats), seats, f == "commander"),
					Commander: f == "commander", Policy: policy})
			}
		}
	}

	var findings *bufio.Writer
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		ff, err := os.Create(filepath.Join(out, "findings.jsonl"))
		if err != nil {
			return err
		}
		defer ff.Close()
		findings = bufio.NewWriter(ff)
		defer findings.Flush()
	}

	results := make([]paymirror.GameResult, len(specs))
	done := make([]bool, len(specs))
	sum := paymirror.NewSummary()
	enc := json.NewEncoder(io.Discard)
	if findings != nil {
		enc = json.NewEncoder(findings)
	}
	var mu sync.Mutex
	flushed := 0
	// flush writes every finished game in spec order, so findings.jsonl is
	// streamed as the run progresses yet byte-identical across worker counts.
	flush := func() {
		for flushed < len(specs) && done[flushed] {
			g := results[flushed]
			sum.Add(g)
			if g.Err != "" || g.RestViolation != "" {
				_ = enc.Encode(map[string]any{"game_error": g.Err, "rest_violation": g.RestViolation, "spec": g.Spec, "turns": g.Turns, "intents": g.Intents})
			}
			for _, r := range g.Reports {
				st, _ := r.Verdict()
				controlBad := r.Control != nil && r.Control.Status != paymirror.Equivalent
				if st == paymirror.Equivalent && !controlBad && !trace {
					continue
				}
				_ = enc.Encode(map[string]any{"spec": g.Spec, "report": r})
			}
			if findings != nil {
				_ = findings.Flush()
			}
			results[flushed] = paymirror.GameResult{Spec: g.Spec}
			flushed++
		}
	}
	opt := paymirror.DriverOptions{MaxIntents: maxIntents, MaxTurns: int32(maxTurns), Control: control, Trace: trace, Resolve: resolve,
		MaxObjects: maxObjects}

	var wg sync.WaitGroup
	next := make(chan int)
	if workers < 1 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				t0 := time.Now()
				gameOpt := opt
				if budget > 0 {
					gameOpt.BudgetExceeded = func() bool { return time.Since(t0) > budget }
				}
				g := paymirror.PlayGame(decks, specs[i], gameOpt)
				if progress {
					fmt.Fprintf(os.Stderr, "game %d/%d seed=%d decks=%s turns=%d intents=%d planned=%d err=%q %.1fs\n",
						i+1, len(specs), g.Spec.Seed, strings.Join(g.Spec.Decks, ","), g.Turns, g.Intents, len(g.Reports), g.Err, time.Since(t0).Seconds())
				}
				mu.Lock()
				results[i], done[i] = g, true
				flush()
				mu.Unlock()
			}
		}()
	}
	for i := range specs {
		next <- i
	}
	close(next)
	wg.Wait()

	flush()
	sum.Write(stdout)
	if out != "" {
		f, err := os.Create(filepath.Join(out, "summary.txt"))
		if err != nil {
			return err
		}
		defer f.Close()
		sum.Write(f)
	}
	return nil
}
