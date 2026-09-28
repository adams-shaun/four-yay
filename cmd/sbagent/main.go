// Command sbagent is a SpellBench protocol v2 agent (the agent role of
// spec/SPELLBENCH_PROTOCOL_V2.md, Section 10): it answers hello, game_start,
// choose and game_over as NDJSON over stdin/stdout until stdin closes.
//
//	sbagent -policy random|heuristic|first [-name NAME] [-version V] [-seed N] [-no-echo] [-quiet] [-stats] [-belief-out FILE]
//	sbagent -policy shadow-<name> -cards POOL.gob.gz|CORPUS_DIR [-budget-ms N] [-trace FILE] ...
//	sbagent -save-pool POOL.gob.gz -cards CORPUS_DIR
//
// Policies (internal/spellbench/v2agent):
//
//   - first: always candidate 0 (pass whenever passing is legal); the python
//     v2 "first" builtin, choice for choice.
//   - heuristic: the python v2 "heuristic" builtin's fixed kind preference,
//     choice for choice.
//   - random: a uniformly random candidate from a SplitMix64 stream seeded
//     at game_start with agent_seed XOR -seed (spec 10.2 delivers agent_seed
//     in game_start; spec 11.6 derives it from the run secret, game index
//     and seat). This is the python v2 "uniform" builtin's derivation
//     (spellbench-arena-uniform-v2), so with the same -seed as that bot's
//     --seed it makes the same picks.
//
// The shadow-* policies (internal/spellbench/v2shadow) rebuild a gorge
// engine from every decision's observation and answer with a gorge policy
// on it: shadow-tactical is sb-tactical, shadow-search-* is sb-search (see
// v2shadow.Names for the budgets). Every decision the shadow cannot stage
// or map is answered by the heuristic and counted in the -stats line, with
// per-decision wall latency (the only clock read besides the search's
// clock guard, -budget-ms, which stops dealing worlds once a decision has
// used that much time and is counted when it fires). -cards names the card
// registry: a pool registry written by -save-pool (fast: the catalog decks
// and the token scripts only) or a corpus directory.
//
// The hello_ok bot name defaults to "sbagent-<policy>" and the version to
// Version; an arena that checks hello against its config entry needs them to
// match that entry. Diagnostics (unknown observation fields, mistyped
// fields) go to stderr, never stdout; -quiet drops them.
//
// The agent never forfeits a recoverable error (v2agent.Stats): a policy
// failure is answered by the fallback policy, a choose for a game it is not
// serving starts that game, a game_start over a stale game replaces it, and
// candidates without ids are answered by position. -stats writes those
// counts as one "sbagent-stats: {json}" line to stderr at exit (even with
// -quiet).
//
// -belief-out appends one JSON line per choose with the seat's
// reconstruction (v2agent.Belief) for the reverse-adapter shadow check
// (cmd/sbv2shadow); diagnostics only.
//
// Exit status: 0 at stdin EOF, 1 on an I/O error, 2 for bad arguments.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/spellbench/v2shadow"
)

// Version is sbagent's hello_ok bot version.
const Version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbagent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	policyName := fs.String("policy", "random", "policy: random, heuristic or first")
	name := fs.String("name", "", `hello_ok bot name (default "sbagent-<policy>")`)
	version := fs.String("version", Version, "hello_ok bot version")
	seed := fs.Uint64("seed", 0, "random policy: XORed into each game's agent_seed (the python uniform bot's --seed)")
	noEcho := fs.Bool("no-echo", false, "omit the optional seat_step and semantic_echo from each choice")
	quiet := fs.Bool("quiet", false, "write no diagnostics to stderr")
	stats := fs.Bool("stats", false, `write the recovery counts as one "sbagent-stats: {json}" line to stderr at exit`)
	beliefOut := fs.String("belief-out", "", "append the per-decision belief (shadow check) to this file as JSON lines")
	cardsPath := fs.String("cards", ".cards", "shadow policies: a pool registry (.gob.gz, -save-pool) or a corpus directory")
	budget := fs.Float64("budget-ms", 10000, "shadow-search: per-decision clock guard in ms (0 disables)")
	trace := fs.String("trace", "", "shadow policies: append a per-decision trace to this file")
	savePool := fs.String("save-pool", "", "write the pool registry (catalog decks + tokens) of -cards to this .gob.gz and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "sbagent: unexpected arguments %q\n", fs.Args())
		return 2
	}
	if *savePool != "" {
		full, err := v2shadow.OpenRegistry(*cardsPath)
		if err != nil {
			fmt.Fprintf(stderr, "sbagent: %v\n", err)
			return 2
		}
		pool, err := v2shadow.PoolRegistry(full)
		if err == nil {
			err = pool.Save(*savePool)
		}
		if err != nil {
			fmt.Fprintf(stderr, "sbagent: %v\n", err)
			return 1
		}
		return 0
	}
	var policy v2agent.Policy
	var shadow *v2shadow.Policy
	var timer *timedPolicy
	if rest, ok := strings.CutPrefix(*policyName, "shadow-"); ok {
		reg, err := v2shadow.OpenRegistry(*cardsPath)
		if err != nil {
			fmt.Fprintf(stderr, "sbagent: opening cards %s: %v\n", *cardsPath, err)
			return 2
		}
		cfg, err := v2shadow.NamedConfig(rest, reg)
		if err != nil {
			fmt.Fprintf(stderr, "sbagent: %v\n", err)
			return 2
		}
		cfg.Seed = *seed
		start := time.Now()
		cfg.Clock = func() float64 { return float64(time.Since(start).Microseconds()) / 1000 }
		cfg.BudgetMS = *budget
		if *trace != "" {
			f, err := os.OpenFile(*trace, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				fmt.Fprintf(stderr, "sbagent: %v\n", err)
				return 2
			}
			defer f.Close()
			cfg.Trace = f
		}
		if shadow, err = v2shadow.New(cfg); err != nil {
			fmt.Fprintf(stderr, "sbagent: %v\n", err)
			return 2
		}
		timer = &timedPolicy{p: shadow}
		policy = timer
	} else {
		var err error
		if policy, err = v2agent.NewPolicy(*policyName, *seed); err != nil {
			fmt.Fprintf(stderr, "sbagent: %v\n", err)
			return 2
		}
	}
	if *name == "" {
		*name = "sbagent-" + *policyName
	}
	opts := v2agent.Options{Name: *name, Version: *version, NoEcho: *noEcho, Log: stderr}
	if *quiet {
		opts.Log = nil
	}
	if *beliefOut != "" {
		f, err := os.OpenFile(*beliefOut, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(stderr, "sbagent: %v\n", err)
			return 2
		}
		defer f.Close()
		opts.BeliefLog = f
	}
	agent, err := v2agent.New(policy, opts)
	if err != nil {
		fmt.Fprintf(stderr, "sbagent: %v\n", err)
		return 2
	}
	err = agent.Serve(stdin, stdout)
	if *stats {
		var rec any = agent.Stats
		if shadow != nil {
			m := map[string]any{}
			if raw, jerr := json.Marshal(agent.Stats); jerr == nil {
				_ = json.Unmarshal(raw, &m)
			}
			m["Bot"] = *name
			m["Shadow"] = shadow.Summary()
			m["Latency"] = timer.summary()
			m["ShadowFallbacks"] = shadow.Stats.Fallbacks()
			rec = m
		}
		if raw, jerr := json.Marshal(rec); jerr == nil {
			fmt.Fprintf(stderr, "sbagent-stats: %s\n", raw)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "sbagent: %v\n", err)
		return 1
	}
	return 0
}

// timedPolicy measures each Choose's wall time (reported in -stats only;
// it reaches no answer).
type timedPolicy struct {
	p  v2agent.Policy
	ms []float64
}

func (t *timedPolicy) GameStart(g *v2agent.GameStart) { t.p.GameStart(g) }
func (t *timedPolicy) GameOver(g *v2agent.GameOver)   { t.p.GameOver(g) }
func (t *timedPolicy) Choose(d *v2agent.Decision) (int, error) {
	t0 := time.Now()
	k, err := t.p.Choose(d)
	t.ms = append(t.ms, float64(time.Since(t0).Microseconds())/1000)
	return k, err
}

// summary is the per-decision latency distribution (ms).
func (t *timedPolicy) summary() map[string]any {
	n := len(t.ms)
	out := map[string]any{"decisions": n}
	if n == 0 {
		return out
	}
	s := append([]float64(nil), t.ms...)
	sort.Float64s(s)
	q := func(p float64) float64 { return s[min(n-1, int(p*float64(n)))] }
	total := 0.0
	over1s, over10s := 0, 0
	for _, x := range s {
		total += x
		if x > 1000 {
			over1s++
		}
		if x > 10000 {
			over10s++
		}
	}
	out["total_ms"], out["mean_ms"], out["p50_ms"], out["p90_ms"], out["p99_ms"], out["max_ms"] = total, total/float64(n), q(0.5), q(0.9), q(0.99), s[n-1]
	out["over_1s"], out["over_10s"] = over1s, over10s
	return out
}
