// Command sbv2local plays SpellBench protocol v2 games in-process: the gorge
// v2 environment (internal/spellbench/v2engine) against two v2 agents
// (internal/spellbench/v2agent), every message the canonical line either
// side would send over a pipe, no python host. It is the fast development
// loop for the v2 agents; the rated measurement is the reference host
// (cmd/sbv2engine/scripts/sbv2_reverse.py).
//
//	sbv2local -dir .cards -a shadow-search-lite-atk -b heuristic [-decks all] [-pairs N]
//	          [-seed S] [-workers K] [-out games.jsonl] [-trace DIR] [-mana manual|autopay]
//
// The schedule is the benchmark's shape: per deck, -pairs seat-swapped game
// pairs (a mirror, the deck in both seats). Game (deck d, pair p, swap s)
// uses secret S*1000003 + d*1009 + p*2 + s and starts with seat p%2.
// Policies: heuristic, random, first (the python builtins' ports) and
// shadow-<name> (internal/spellbench/v2shadow.Names).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/spellbench/v2engine"
	"github.com/adams-shaun/gorge/internal/spellbench/v2shadow"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type job struct {
	idx      int
	deck     string
	pair     int
	swap     int
	secret   uint64
	start    string
	pol      [2]string // p0, p1 policy names
	aIsSeat0 bool
}

type row struct {
	Index    int             `json:"index"`
	Deck     string          `json:"deck"`
	P0       string          `json:"p0"`
	P1       string          `json:"p1"`
	Start    string          `json:"start"`
	Outcome  string          `json:"outcome"`
	Class    string          `json:"classification"`
	Winner   string          `json:"winner"`
	Reason   string          `json:"reason"`
	Dec      [2]int          `json:"decisions"`
	WallS    float64         `json:"wall_s"`
	Err      string          `json:"error,omitempty"`
	Shadow   [2]*shadowStats `json:"shadow,omitempty"`
	MSMax    [2]float64      `json:"ms_max"`
	MSTotal  [2]float64      `json:"ms_total"`
	Over1s   [2]int          `json:"over_1s"`
	Fallback [2]int          `json:"agent_fallbacks"`
}

type shadowStats struct {
	Stats    v2shadow.Stats `json:"stats"`
	Tactical any            `json:"tactical,omitempty"`
}

// timed wraps a policy and measures each Choose (reported only).
type timed struct {
	p           v2agent.Policy
	total, max  float64
	over1s      int
	decisionsMS []float64
}

func (t *timed) GameStart(g *v2agent.GameStart) { t.p.GameStart(g) }
func (t *timed) GameOver(g *v2agent.GameOver)   { t.p.GameOver(g) }
func (t *timed) Choose(d *v2agent.Decision) (int, error) {
	t0 := time.Now()
	k, err := t.p.Choose(d)
	ms := float64(time.Since(t0).Microseconds()) / 1000
	t.total += ms
	if ms > t.max {
		t.max = ms
	}
	if ms > 1000 {
		t.over1s++
	}
	return k, err
}

func makePolicy(name string, reg *cards.Registry, trace io.Writer) (v2agent.Policy, *v2shadow.Policy, error) {
	if rest, ok := strings.CutPrefix(name, "shadow-"); ok {
		cfg, err := v2shadow.NamedConfig(rest, reg)
		if err != nil {
			return nil, nil, err
		}
		cfg.Trace = trace
		start := time.Now()
		cfg.Clock = func() float64 { return float64(time.Since(start).Microseconds()) / 1000 }
		cfg.BudgetMS = 20000
		p, err := v2shadow.New(cfg)
		return p, p, err
	}
	p, err := v2agent.NewPolicy(name, 11)
	return p, nil, err
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbv2local", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".cards", "corpus directory")
	a := fs.String("a", "shadow-tactical", "policy A")
	b := fs.String("b", "heuristic", "policy B")
	decksFlag := fs.String("decks", "all", "comma-separated catalog decks, or all (the benchmark pool)")
	pairs := fs.Int("pairs", 1, "seat-swapped pairs per deck")
	seed := fs.Uint64("seed", 1000, "base seed")
	workers := fs.Int("workers", 4, "parallel games")
	out := fs.String("out", "", "append one JSON row per game here")
	traceDir := fs.String("trace", "", "write per-game decision traces of shadow policies here")
	mana := fs.String("mana", "manual", "engine mana surface: manual or autopay")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reg, err := testutil.OpenCorpusRegistry(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "sbv2local: %v\n", err)
		return 2
	}
	decks := spellbench.BenchmarkPool
	if *decksFlag != "all" {
		decks = strings.Split(*decksFlag, ",")
	}
	var jobs []job
	for di, deck := range decks {
		for p := 0; p < *pairs; p++ {
			for s := 0; s < 2; s++ {
				j := job{idx: len(jobs), deck: deck, pair: p, swap: s,
					secret: *seed*1000003 + uint64(di)*1009 + uint64(p)*2 + uint64(s),
					start:  fmt.Sprintf("p%d", p%2), aIsSeat0: s == 0}
				if s == 0 {
					j.pol = [2]string{*a, *b}
				} else {
					j.pol = [2]string{*b, *a}
				}
				jobs = append(jobs, j)
			}
		}
	}
	var outF *os.File
	if *out != "" {
		if outF, err = os.OpenFile(*out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			fmt.Fprintf(stderr, "sbv2local: %v\n", err)
			return 2
		}
		defer outF.Close()
	}
	if *traceDir != "" {
		_ = os.MkdirAll(*traceDir, 0o755)
	}
	var mu sync.Mutex
	rows := make([]row, len(jobs))
	next := 0
	var wg sync.WaitGroup
	t0 := time.Now()
	done := 0
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv, err := v2engine.New(v2engine.Options{Registry: reg, Mana: *mana, Version: "local"})
			if err != nil {
				fmt.Fprintf(stderr, "sbv2local: %v\n", err)
				return
			}
			for {
				mu.Lock()
				if next >= len(jobs) {
					mu.Unlock()
					return
				}
				j := jobs[next]
				next++
				mu.Unlock()
				r := playOne(srv, reg, j, *traceDir)
				mu.Lock()
				rows[j.idx] = r
				done++
				if outF != nil {
					raw, _ := json.Marshal(r)
					outF.Write(append(raw, '\n'))
				}
				fmt.Fprintf(stderr, "[%d/%d %.0fs] g%d %-9s %s vs %s: %s %s %s dec=%v ms=%.0f/%.0f\n", done, len(jobs),
					time.Since(t0).Seconds(), j.idx, j.deck, j.pol[0], j.pol[1], r.Class, r.Winner, r.Err, r.Dec, r.MSTotal[0], r.MSTotal[1])
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	summarize(stdout, rows, *a, *b)
	return 0
}

func playOne(srv *v2engine.Server, reg *cards.Registry, j job, traceDir string) row {
	r := row{Index: j.idx, Deck: j.deck, P0: j.pol[0], P1: j.pol[1], Start: j.start}
	var agents [2]*v2agent.Agent
	var shadows [2]*v2shadow.Policy
	var timers [2]*timed
	for i := 0; i < 2; i++ {
		var tw io.Writer
		if traceDir != "" && strings.HasPrefix(j.pol[i], "shadow-") {
			f, err := os.Create(filepath.Join(traceDir, fmt.Sprintf("g%d-p%d.txt", j.idx, i)))
			if err == nil {
				defer f.Close()
				tw = f
			}
		}
		p, sh, err := makePolicy(j.pol[i], reg, tw)
		if err != nil {
			r.Err = err.Error()
			return r
		}
		timers[i] = &timed{p: p}
		shadows[i] = sh
		ag, err := v2agent.New(timers[i], v2agent.Options{Name: j.pol[i], Version: "local", SkipStrictCheck: true})
		if err != nil {
			r.Err = err.Error()
			return r
		}
		agents[i] = ag
	}
	t0 := time.Now()
	res, err := srv.PlayLocal(v2engine.LocalGame{GameID: fmt.Sprintf("local-%d-%d", j.secret, j.idx), Deck: j.deck,
		Secret: j.secret, Start: j.start, AgentSeeds: [2]uint64{j.secret*2 + 1, j.secret*2 + 2}}, agents)
	r.WallS = time.Since(t0).Seconds()
	if err != nil {
		r.Err = err.Error()
	}
	r.Outcome, r.Class, r.Winner, r.Reason, r.Dec = res.Outcome, res.Classification, res.Winner, res.Reason, res.Decisions
	for i := 0; i < 2; i++ {
		r.MSMax[i], r.MSTotal[i], r.Over1s[i] = timers[i].max, timers[i].total, timers[i].over1s
		r.Fallback[i] = agents[i].Stats.PolicyFallbacks
		if shadows[i] != nil {
			sm := shadows[i].Summary()
			r.Shadow[i] = &shadowStats{Stats: shadows[i].Stats, Tactical: sm["tactical"]}
		}
	}
	return r
}

func summarize(w io.Writer, rows []row, a, b string) {
	wins := map[string]int{}
	perDeck := map[string][2]int{}
	var errs, halts int
	agg := map[string]map[string]int{} // policy -> counter
	reasons := map[string]map[string]int{}
	lossy := map[string]map[string]int{}
	var msTot, decTot = map[string]float64{}, map[string]int{}
	msMax := map[string]float64{}
	for _, r := range rows {
		if r.Err != "" {
			errs++
		}
		if r.Class == "halted" {
			halts++
		}
		pols := [2]string{r.P0, r.P1}
		if r.Winner == "p0" || r.Winner == "p1" {
			wp := pols[0]
			if r.Winner == "p1" {
				wp = pols[1]
			}
			wins[wp]++
			d := perDeck[r.Deck]
			if wp == a {
				d[0]++
			} else {
				d[1]++
			}
			perDeck[r.Deck] = d
		} else {
			wins["draw"]++
		}
		for i := 0; i < 2; i++ {
			msTot[pols[i]] += r.MSTotal[i]
			decTot[pols[i]] += r.Dec[i]
			if r.MSMax[i] > msMax[pols[i]] {
				msMax[pols[i]] = r.MSMax[i]
			}
			s := r.Shadow[i]
			if s == nil {
				continue
			}
			pol := pols[i]
			if agg[pol] == nil {
				agg[pol], reasons[pol], lossy[pol] = map[string]int{}, map[string]int{}, map[string]int{}
			}
			agg[pol]["decisions"] += s.Stats.Decisions
			agg[pol]["builds"] += s.Stats.Builds
			agg[pol]["refusals"] += s.Stats.Refusals
			agg[pol]["panics"] += s.Stats.Panics
			agg[pol]["lowerings"] += s.Stats.Lowerings
			agg[pol]["lowered_casts"] += s.Stats.LoweredCasts
			agg[pol]["searched"] += s.Stats.Searched
			agg[pol]["clock_stops"] += s.Stats.ClockStops
			agg[pol]["proposal_roots"] += s.Stats.ProposalRoots
			agg[pol]["proposal_wins"] += s.Stats.ProposalWins
			agg[pol]["agent_fallbacks"] += r.Fallback[i]
			for cls, m := range s.Stats.Answered {
				for src, n := range m {
					agg[pol]["answered "+cls+"/"+src] += n
				}
			}
			for k, n := range s.Stats.Reasons {
				reasons[pol][k] += n
			}
			for k, n := range s.Stats.LoweringAborts {
				reasons[pol]["lowering abort: "+k] += n
			}
			for k, n := range s.Stats.Lossy {
				lossy[pol][k] += n
			}
		}
	}
	fmt.Fprintf(w, "games %d  %s %d - %d %s  draws %d  errors %d  halted %d\n", len(rows), a, wins[a], wins[b], b, wins["draw"], errs, halts)
	var ds []string
	for d := range perDeck {
		ds = append(ds, d)
	}
	sort.Strings(ds)
	for _, d := range ds {
		fmt.Fprintf(w, "  %-10s %d-%d\n", d, perDeck[d][0], perDeck[d][1])
	}
	for _, pol := range []string{a, b} {
		if decTot[pol] > 0 {
			fmt.Fprintf(w, "%s: %d decisions, mean %.2f ms/decision, max %.0f ms\n", pol, decTot[pol], msTot[pol]/float64(decTot[pol]), msMax[pol])
		}
		dump := func(title string, m map[string]int) {
			if len(m) == 0 {
				return
			}
			var ks []string
			for k := range m {
				ks = append(ks, k)
			}
			sort.Slice(ks, func(i, j int) bool { return m[ks[i]] > m[ks[j]] || m[ks[i]] == m[ks[j]] && ks[i] < ks[j] })
			fmt.Fprintf(w, "  %s:\n", title)
			for i, k := range ks {
				if i >= 40 {
					break
				}
				fmt.Fprintf(w, "    %7d %s\n", m[k], k)
			}
		}
		dump("counters", agg[pol])
		dump("fallback reasons", reasons[pol])
		dump("lossy (builds)", lossy[pol])
	}
}
