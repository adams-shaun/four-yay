//go:build gamepool

// Command gamepool is the experimental, tag-gated driver for internal/gamepool.
//
// It keeps M games in a pool and, per seat, runs N goroutines that pull turn
// notifications. A decision offering more than one option is enqueued to a
// shared batch (flushed at -batch requests, or every -flush-ms) and the
// enqueuing goroutine schedules a callback rather than blocking on the GPU.
// Trivial decisions are answered from the wrapped bot inline.
//
// Build and run:
//
//	make gamepool
//	bin/gamepool -decks internal/testutil/decks -multiply 64 -per-seat 4 -batch 32
//
// The reported width histogram is the point: it is how many decisions a single
// batch (and therefore a single GPU forward) actually covers at a given game
// count, per-seat worker count and flush window.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/gamepool"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

func main() { os.Exit(run(os.Args[1:])) }

type cfg struct {
	decksDir  string
	corpusDir string
	aDeck     string
	bDeck     string
	multiply  int
	seed      uint64
	concurrent int
	perSeat   int
	batch     int
	flushMS   int
	scorer    string
	gpuArtifact string
	maxTurns  int
	maxIntent int
	out       string
	verbose   bool
}

func run(args []string) int {
	fs := flag.NewFlagSet("gamepool", flag.ExitOnError)
	c := &cfg{}
	fs.StringVar(&c.decksDir, "decks", "internal/testutil/decks", "deck directory (.json decks)")
	fs.StringVar(&c.corpusDir, "cards", ".cards", "gorge corpus directory")
	fs.StringVar(&c.aDeck, "a", "", "deck A name (file stem); empty picks the first two decks")
	fs.StringVar(&c.bDeck, "b", "", "deck B name")
	fs.IntVar(&c.multiply, "multiply", 64, "games in the pool; each gets its own shuffle (seed+i)")
	fs.Uint64Var(&c.seed, "seed", 1, "base seed")
	fs.IntVar(&c.concurrent, "concurrent", 0, "games run at once (0 = GOMAXPROCS)")
	fs.IntVar(&c.perSeat, "per-seat", 4, "N: turn-consuming goroutines per player/bot")
	fs.IntVar(&c.batch, "batch", 32, "B: flush the batch at this many decisions")
	fs.IntVar(&c.flushMS, "flush-ms", 2, "X: flush a partial batch after this many ms (0 = serve the ready set now)")
	fs.StringVar(&c.scorer, "scorer", "local", "scorer: local | gpu")
	fs.StringVar(&c.gpuArtifact, "gpu-artifact", "../../scratch/gpupoc/artifacts/flat_train.cudafile", "flat_train cudafile for -scorer gpu")
	fs.IntVar(&c.maxTurns, "max-turns", 100, "turn cap")
	fs.IntVar(&c.maxIntent, "max-intents", 20000, "decision cap per game")
	fs.StringVar(&c.out, "out", "", "games JSONL (default stdout)")
	fs.BoolVar(&c.verbose, "v", false, "per-game lines to stderr")
	fs.Parse(args)

	if !gamepool.Enabled {
		fmt.Fprintln(os.Stderr, "gamepool: this binary was built without -tags gamepool")
		return 2
	}
	if c.concurrent <= 0 {
		c.concurrent = runtime.GOMAXPROCS(0)
	}
	if c.multiply <= 0 {
		c.multiply = 1
	}
	return runPool(c)
}

type rec struct {
	Game   int     `json:"game"`
	Seed   uint64  `json:"seed"`
	Winner int     `json:"winner"`
	Result string  `json:"result"`
	Turns  int     `json:"turns"`
	Intent int     `json:"intents"`
	Millis float64 `json:"ms"`
	Err    string  `json:"err,omitempty"`
}

func runPool(c *cfg) int {
	sc, err := newScorer(c.scorer, c.gpuArtifact)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gamepool:", err)
		return 1
	}
	reg, err := cards.SharedCorpus(c.corpusDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gamepool: corpus:", err)
		return 1
	}
	aName, bName, err := pickDecks(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gamepool:", err)
		return 1
	}
	_, dA, err := deck.Load(reg, c.decksDir+"/"+aName+".json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gamepool: deck A:", err)
		return 1
	}
	_, dB, err := deck.Load(reg, c.decksDir+"/"+bName+".json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gamepool: deck B:", err)
		return 1
	}

	pool, err := gamepool.New(2, sc, gamepool.Options{
		Workers:    c.perSeat,
		MaxBatch:   c.batch,
		FlushEvery: time.Duration(c.flushMS) * time.Millisecond,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gamepool:", err)
		return 1
	}

	out := os.Stdout
	if c.out != "" {
		f, err := os.Create(c.out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gamepool:", err)
			return 1
		}
		defer f.Close()
		out = f
	}
	w := bufio.NewWriter(out)
	defer w.Flush()

	fmt.Fprintf(os.Stderr, "gamepool: %s vs %s  x%d = %d games, %d at once, scorer %s, %d workers/seat, batch %d, flush %dms\n",
		aName, bName, c.multiply, c.multiply, c.concurrent, sc.Name(), c.perSeat, c.batch, c.flushMS)

	var mu sync.Mutex
	recs := make([]rec, 0, c.multiply)
	ch := make(chan int)
	var wg sync.WaitGroup
	t0 := time.Now()
	for k := 0; k < c.concurrent; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				g := rec{Game: i, Seed: c.seed + uint64(i), Winner: -1}
				playOne(pool, reg, &g, dA, dB, c)
				mu.Lock()
				recs = append(recs, g)
				mu.Unlock()
				if c.verbose {
					fmt.Fprintf(os.Stderr, "  game %d seed %d -> %s (%d turns, %.0f ms)\n",
						g.Game, g.Seed, g.Result, g.Turns, g.Millis)
				}
			}
		}()
	}
	for i := 0; i < c.multiply; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
	stats := pool.Stop()

	sort.Slice(recs, func(i, j int) bool { return recs[i].Game < recs[j].Game })
	aW, bW, dr, er := 0, 0, 0, 0
	for i := range recs {
		b, _ := json.Marshal(recs[i])
		w.Write(b)
		w.WriteByte('\n')
		switch recs[i].Result {
		case "A":
			aW++
		case "B":
			bW++
		case "draw":
			dr++
		default:
			er++
		}
	}
	w.Flush()
	el := time.Since(t0).Seconds()
	fmt.Fprintf(os.Stderr, "\n%d games in %.1fs (%.1f games/s)\n", len(recs), el, float64(len(recs))/el)
	fmt.Fprintf(os.Stderr, "A %d  B %d  draw %d  error %d\n", aW, bW, dr, er)
	fmt.Fprintf(os.Stderr, "pool: %s\n", stats)
	fmt.Fprintf(os.Stderr, "width histogram (bucket = width 1,2,4,...,256):\n")
	for i, n := range stats.Buckets {
		if n == 0 {
			continue
		}
		lo := 1 << i
		fmt.Fprintf(os.Stderr, "  %4d+: %9d calls (%.1f%%)\n", lo, n, 100*float64(n)/float64(stats.Served))
	}
	return 0
}

// playOne runs one pooled game: the same decks, this game's seed, both seats
// wrapped behind the pool. Wrapping changes only WHEN a decision is served,
// never WHAT: the served intent is what the wrapped bot chose (local scorer).
func playOne(pool *gamepool.Pool, reg *cards.Registry, g *rec, dA, dB []*cards.Card, c *cfg) {
	seats := []seat.Seat{
		pool.Wrap(seat.NewBot(g.Seed^1), 0),
		pool.Wrap(seat.NewBot(g.Seed^2), 1),
	}
	cfgr := rules.Config{
		Seed:   g.Seed,
		Names:  []string{"p0", "p1"},
		Decks:  [][]*cards.Card{dA, dB},
		Tokens: reg.Tokens,
	}
	hooks := bench.Hooks{
		Setup: func(e *rules.Engine) { e.SetDecisionArena(true) },
	}
	t1 := time.Now()
	o, e, err := bench.PlayGame(cfgr, seats, c.maxTurns, c.maxIntent, hooks)
	g.Millis = float64(time.Since(t1).Microseconds()) / 1000
	g.Turns, g.Intent = int(o.Turns), o.Intents
	switch {
	case err != nil:
		g.Result, g.Err = "error", err.Error()
	case o.IsStalled():
		g.Result, g.Err = "stall", o.StallOn
		if o.Livelock != "" {
			g.Err += ": " + o.Livelock
		}
	case o.Draw:
		g.Result = "draw"
	default:
		g.Winner = o.WinnerSeat
		if o.WinnerSeat == 0 {
			g.Result = "A"
		} else {
			g.Result = "B"
		}
	}
	if e != nil {
		e.Release()
	}
}

// pickDecks resolves the two deck names: the flags, or the first two stems.
func pickDecks(c *cfg) (string, string, error) {
	if c.aDeck != "" && c.bDeck != "" {
		return c.aDeck, c.bDeck, nil
	}
	ents, err := os.ReadDir(c.decksDir)
	if err != nil {
		return "", "", err
	}
	var stems []string
	for _, e := range ents {
		n := e.Name()
		if len(n) > 5 && n[len(n)-5:] == ".json" {
			stems = append(stems, n[:len(n)-5])
		}
	}
	sort.Strings(stems)
	if len(stems) < 2 {
		return "", "", fmt.Errorf("need at least two decks in %s", c.decksDir)
	}
	a, b := c.aDeck, c.bDeck
	if a == "" {
		a = stems[0]
	}
	if b == "" {
		b = stems[1]
	}
	return a, b, nil
}
