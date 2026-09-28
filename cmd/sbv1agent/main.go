// Command sbv1agent is a SpellBench protocol v1 agent (the agent role of
// spec/SPELLBENCH_PROTOCOL_V1.md, Section 10) for any Go policy in
// internal/spellbench/v1agent. It answers hello, game_start, choose and
// game_over as NDJSON over stdin/stdout until stdin closes.
//
//	sbv1agent -policy uniform|heuristic|first|tactical [-seed N] [-name NAME] [-version V] [-quiet]
//	sbv1agent -policy shadow-tactical|shadow-az|shadow-aztac -cards DIR [-sims N] [-worlds K] ...
//
// uniform, heuristic and first are the python arena builtins, choice for
// choice (uniform with the same -seed as the builtin's "seed" makes the
// same picks). tactical reads the decision's x_kernel_v5 board when the
// engine attaches one (mtg-kernel's bridge does) and falls back to the
// candidate references otherwise.
//
// The shadow-* policies (internal/spellbench/kshadow) rebuild a gorge engine
// from every decision's x_kernel_v5 observation and answer with a gorge
// policy (sb-tactical, or azmcts over redealt worlds); every decision they
// cannot stage or map is answered by tactical and counted in the -stats
// line, with per-decision latency (the only clock read: it reaches no
// answer).
//
// A policy failure never becomes a wire error (that forfeits the game): it
// is answered by the heuristic and counted; the counts go to stderr at EOF.
//
// Exit status: 0 at stdin EOF, 1 on an I/O error, 2 for bad arguments.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/kshadow"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
)

// Version is the default hello_ok bot version.
const Version = "0.1.0"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbv1agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	policyName := fs.String("policy", "tactical", "policy: uniform, heuristic, first or tactical")
	name := fs.String("name", "", `hello_ok bot name (default "sbv1-<policy>")`)
	version := fs.String("version", Version, "hello_ok bot version")
	seed := fs.Uint64("seed", 0, "uniform: the builtin's seed")
	quiet := fs.Bool("quiet", false, "no diagnostics on stderr")
	trace := fs.String("trace", "", "tactical: append a per-decision trace to this file")
	stats := fs.String("stats", "", "append one JSON line of agent counters (decisions, fallbacks, missing x_kernel_v5, wire errors, retries) at exit")
	cardsDir := fs.String("cards", ".cards", "shadow policies: gorge card corpus directory, or a pool registry .gob.gz (kshadowcheck -save-pool)")
	sims := fs.Int("sims", 25, "shadow-az*: simulations per searched decision")
	worlds := fs.Int("worlds", 0, "shadow-az*: dealt worlds per decision (0: one per simulation)")
	rollW := fs.Int("roll-worlds", 0, "shadow-roll: worlds per decision (0: default)")
	rollH := fs.Int("roll-horizon", -1, "shadow-roll: rollout turns beyond the current one (-1: default)")
	rollM := fs.Float64("roll-margin", -1, "shadow-roll: override margin (-1: default)")
	rollK := fs.Int("roll-topk", 0, "shadow-roll: priority candidates kept (0: default)")
	rollP := fs.String("roll-policy", "", "shadow-roll: rollout policy bot|tactical (default bot)")
	kinds := fs.String("kinds", "priority,attackers,blockers,target", "shadow-az*: searched decision kinds")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "sbv1agent: unexpected arguments %q\n", fs.Args())
		return 2
	}
	var topts v1agent.TacticalOptions
	if *trace != "" {
		f, err := os.OpenFile(*trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(stderr, "sbv1agent: %v\n", err)
			return 2
		}
		defer f.Close()
		topts.Trace = f
	}
	var p v1agent.Policy
	var shadow *kshadow.Policy
	var timed *timedPolicy
	if mode, ok := strings.CutPrefix(*policyName, "shadow-"); ok {
		reg, err := kshadow.OpenRegistry(*cardsDir)
		if err != nil {
			fmt.Fprintf(stderr, "sbv1agent: opening corpus %s: %v\n", *cardsDir, err)
			return 2
		}
		k, err := azmcts.ParseKinds(*kinds)
		if err != nil {
			fmt.Fprintf(stderr, "sbv1agent: %v\n", err)
			return 2
		}
		roll := kshadow.DefaultRoll()
		if *rollW > 0 {
			roll.Worlds = *rollW
		}
		if *rollH >= 0 {
			roll.Horizon = *rollH
		}
		if *rollM >= 0 {
			roll.Margin = *rollM
		}
		if *rollK > 0 {
			roll.TopK = *rollK
		}
		if *rollP != "" {
			roll.Rollout = *rollP
		}
		cfg := kshadow.Config{Reg: reg, Mode: mode, Sims: *sims, Worlds: *worlds, Kinds: k, Seed: *seed, Roll: roll}
		if topts.Trace != nil {
			cfg.Trace = topts.Trace
		}
		shadow, err = kshadow.New(cfg)
		if err != nil {
			fmt.Fprintf(stderr, "sbv1agent: %v\n", err)
			return 2
		}
		timed = &timedPolicy{p: shadow}
		p = timed
	} else {
		var err error
		p, err = v1agent.NewPolicyWith(*policyName, *seed, topts)
		if err != nil {
			fmt.Fprintf(stderr, "sbv1agent: %v\n", err)
			return 2
		}
	}
	if *name == "" {
		*name = "sbv1-" + *policyName
	}
	opts := v1agent.Options{Name: *name, Version: *version, Log: stderr, ExtensionsAccepted: []string{"x_kernel_v5"}}
	if shadow != nil {
		// A shadow policy that fails falls back to tactical, never to the
		// weaker heuristic.
		opts.Fallback = v1agent.NewTactical(v1agent.TacticalOptions{})
	}
	if *quiet {
		opts.Log = nil
	}
	agent := v1agent.New(p, opts)
	w := bufio.NewWriter(stdout)
	err := agent.Serve(stdin, w)
	if *stats != "" {
		m := map[string]any{
			"game_id": agent.LastGame, "seat": agent.LastSeat, "bot": *name,
			"decisions": agent.Stats.Decisions, "fallbacks": agent.Stats.Fallbacks,
			"kernel_missing": agent.Stats.KernelMissing, "wire_errors": agent.Stats.WireErrors,
			"retries": agent.Stats.RetriesServed,
		}
		if shadow != nil {
			for k, v := range shadow.Summary() {
				m[k] = v
			}
			m["ms_total"], m["ms_max"] = timed.total, timed.max
			m["ms_over_1s"], m["ms_over_10s"] = timed.over1s, timed.over10s
		}
		rec, _ := json.Marshal(m)
		if f, ferr := os.OpenFile(*stats, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
			f.Write(append(rec, '\n'))
			f.Close()
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "sbv1agent: %v\n", err)
		return 1
	}
	return 0
}

// timedPolicy measures each Choose's wall time (reported in -stats only).
type timedPolicy struct {
	p               *kshadow.Policy
	total, max      float64
	over1s, over10s int
}

func (t *timedPolicy) GameStart(g *v1agent.GameStart) { t.p.GameStart(g) }
func (t *timedPolicy) GameOver(g *v1agent.Terminal)   { t.p.GameOver(g) }
func (t *timedPolicy) Choose(d *v1agent.Decision) int {
	t0 := time.Now()
	defer func() {
		ms := float64(time.Since(t0).Microseconds()) / 1000
		t.total += ms
		if ms > t.max {
			t.max = ms
		}
		if ms > 1000 {
			t.over1s++
		}
		if ms > 10000 {
			t.over10s++
		}
	}()
	return t.p.Choose(d)
}
