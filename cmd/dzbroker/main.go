//go:build broker

// Command dzbroker is the experimental, tag-gated driver for internal/broker.
//
// It runs gorge self-play with a GAME MULTIPLIER and a no-barrier decision
// broker, so the batching envelope (mean/max ready-set width) can be measured
// on real games rather than synthetic tables.
//
// Build and run:
//
//	make dzbroker            # go build -tags broker ./cmd/dzbroker
//	bin/dzbroker -decks internal/testutil/decks -multiply 100 -workers 16
//
// The multiplier: one (deckA, deckB, base seed) matchup expands into N games
// with distinct shuffles (seed = base + i), so the same decks and the same
// bots produce N different outcomes -- a win-rate sample from one matchup.
// Widening N never renumbers an existing game.
//
// The broker: decisions from the wrapped seats are served B at a time
// (-broker-batch), optionally after giving stragglers -broker-wait ms to join.
// The report's width statistics are the measurement; a real GPU scorer would
// replace passScorer without touching the scheduler.
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
	"github.com/adams-shaun/gorge/internal/broker"
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
	workers   int
	brokerBat int
	brokerMS  int
	maxTurns  int
	maxIntent int
	out       string
	verbose   bool
}

func run(args []string) int {
	fs := flag.NewFlagSet("dzbroker", flag.ExitOnError)
	c := &cfg{}
	fs.StringVar(&c.decksDir, "decks", "internal/testutil/decks", "deck directory (.json decks)")
	fs.StringVar(&c.corpusDir, "cards", ".cards", "gorge corpus directory")
	fs.StringVar(&c.aDeck, "a", "", "deck A name (file stem); empty picks the first two decks")
	fs.StringVar(&c.bDeck, "b", "", "deck B name")
	fs.IntVar(&c.multiply, "multiply", 100, "games per matchup; each gets its own shuffle (seed+i)")
	fs.Uint64Var(&c.seed, "seed", 1, "base seed")
	fs.IntVar(&c.workers, "workers", 0, "concurrent games (0 = GOMAXPROCS)")
	fs.IntVar(&c.brokerBat, "broker-batch", 64, "most decisions served in one broker call")
	fs.IntVar(&c.brokerMS, "broker-wait", 0, "ms to wait for stragglers before serving (0 = serve the ready set)")
	fs.IntVar(&c.maxTurns, "max-turns", 100, "turn cap")
	fs.IntVar(&c.maxIntent, "max-intents", 20000, "decision cap per game")
	fs.StringVar(&c.out, "out", "", "games JSONL (default stdout)")
	fs.BoolVar(&c.verbose, "v", false, "per-game lines to stderr")
	fs.Parse(args)

	if !broker.Enabled {
		fmt.Fprintln(os.Stderr, "dzbroker: this binary was built without -tags broker")
		return 2
	}
	if c.workers <= 0 {
		c.workers = runtime.GOMAXPROCS(0)
	}
	if c.multiply <= 0 {
		c.multiply = 1
	}
	return runBroker(c)
}

type rec struct {
	Game   int     `json:"game"`
	Seed   uint64  `json:"seed"`
	Winner int     `json:"winner"` // 0=A, 1=B, -1 none
	Result string  `json:"result"`
	Turns  int     `json:"turns"`
	Intent int     `json:"intents"`
	Millis float64 `json:"ms"`
	Err    string  `json:"err,omitempty"`
}

func runBroker(c *cfg) int {
	reg, err := cards.SharedCorpus(c.corpusDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dzbroker: corpus:", err)
		return 1
	}
	aName, bName, err := pickDecks(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dzbroker:", err)
		return 1
	}
	_, dA, err := deck.Load(reg, c.decksDir+"/"+aName+".json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dzbroker: deck A:", err)
		return 1
	}
	_, dB, err := deck.Load(reg, c.decksDir+"/"+bName+".json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dzbroker: deck B:", err)
		return 1
	}

	br, err := broker.New(broker.Options{
		MaxBatch: c.brokerBat,
		Wait:     time.Duration(c.brokerMS) * time.Millisecond,
	}, broker.PassThrough{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dzbroker:", err)
		return 1
	}

	out := os.Stdout
	if c.out != "" {
		f, err := os.Create(c.out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "dzbroker:", err)
			return 1
		}
		defer f.Close()
		out = f
	}
	w := bufio.NewWriter(out)
	defer w.Flush()

	fmt.Fprintf(os.Stderr, "dzbroker: %s vs %s  x%d = %d games, %d workers, broker batch %d wait %dms\n",
		aName, bName, c.multiply, c.multiply, c.workers, c.brokerBat, c.brokerMS)

	var mu sync.Mutex
	recs := make([]rec, 0, c.multiply)
	ch := make(chan int)
	var wg sync.WaitGroup
	t0 := time.Now()
	for k := 0; k < c.workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				g := rec{Game: i, Seed: c.seed + uint64(i), Winner: -1}
				playOne(br, reg, &g, dA, dB, c)
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
	// The multiplier: the SAME matchup, N distinct shuffles.
	for i := 0; i < c.multiply; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
	stats := br.Stop()

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
	fmt.Fprintf(os.Stderr, "broker: %s\n", stats)
	// The width histogram is the point: a GPU scorer needs wide batches, and
	// the mean alone hides whether widths are absent or bimodal.
	fmt.Fprintf(os.Stderr, "width histogram (bucket = width 1,2,4,...,256):\n")
	for i, n := range stats.Buckets {
		if n == 0 {
			continue
		}
		lo := 1 << i
		fmt.Fprintf(os.Stderr, "  %4d+: %9d calls (%.1f%%)\n", lo, n, 100*float64(n)/float64(stats.Served))
	}
	fmt.Fprintf(os.Stderr, "A share: %.3f\n", float64(aW)/float64(maxi(1, aW+bW)))
	return 0
}

// playOne runs one multiplied game: same decks, this game's seed, both seats
// wrapped behind the broker. This is the control: the wrapped seat still makes
// the choice, so wrapping changes only WHEN a decision is served, never WHAT.
func playOne(br *broker.Broker, reg *cards.Registry, g *rec, dA, dB []*cards.Card, c *cfg) {
	seats := []seat.Seat{
		br.Wrap(seat.NewBot(g.Seed^1), 0),
		br.Wrap(seat.NewBot(g.Seed^2), 1),
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
	g.Turns, g.Intent = int(o.Turns), int(o.Intents)
	switch {
	case err != nil:
		g.Result, g.Err = "error", err.Error()
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

// pickDecks resolves the two deck names: the flags, or the first two stems in
// the directory so the command runs with no arguments on a fresh clone.
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

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
