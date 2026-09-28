// Command sbv1agent is a SpellBench protocol v1 agent (the agent role of
// spec/SPELLBENCH_PROTOCOL_V1.md, Section 10) for any Go policy in
// internal/spellbench/v1agent. It answers hello, game_start, choose and
// game_over as NDJSON over stdin/stdout until stdin closes.
//
//	sbv1agent -policy uniform|heuristic|first|tactical [-seed N] [-name NAME] [-version V] [-quiet]
//
// uniform, heuristic and first are the python arena builtins, choice for
// choice (uniform with the same -seed as the builtin's "seed" makes the
// same picks). tactical reads the decision's x_kernel_v5 board when the
// engine attaches one (mtg-kernel's bridge does) and falls back to the
// candidate references otherwise.
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
	p, err := v1agent.NewPolicyWith(*policyName, *seed, topts)
	if err != nil {
		fmt.Fprintf(stderr, "sbv1agent: %v\n", err)
		return 2
	}
	if *name == "" {
		*name = "sbv1-" + *policyName
	}
	opts := v1agent.Options{Name: *name, Version: *version, Log: stderr, ExtensionsAccepted: []string{"x_kernel_v5"}}
	if *quiet {
		opts.Log = nil
	}
	agent := v1agent.New(p, opts)
	w := bufio.NewWriter(stdout)
	err = agent.Serve(stdin, w)
	if *stats != "" {
		rec, _ := json.Marshal(map[string]any{
			"game_id": agent.LastGame, "seat": agent.LastSeat, "bot": *name,
			"decisions": agent.Stats.Decisions, "fallbacks": agent.Stats.Fallbacks,
			"kernel_missing": agent.Stats.KernelMissing, "wire_errors": agent.Stats.WireErrors,
			"retries": agent.Stats.RetriesServed,
		})
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
