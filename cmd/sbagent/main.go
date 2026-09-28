// Command sbagent is a SpellBench protocol v2 agent (the agent role of
// spec/SPELLBENCH_PROTOCOL_V2.md, Section 10): it answers hello, game_start,
// choose and game_over as NDJSON over stdin/stdout until stdin closes.
//
//	sbagent -policy random|heuristic|first [-name NAME] [-version V] [-seed N] [-no-echo] [-quiet]
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
// The hello_ok bot name defaults to "sbagent-<policy>" and the version to
// Version; an arena that checks hello against its config entry needs them to
// match that entry. Diagnostics (unknown observation fields, mistyped
// fields) go to stderr, never stdout; -quiet drops them.
//
// Exit status: 0 at stdin EOF, 1 on an I/O error, 2 for bad arguments.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "sbagent: unexpected arguments %q\n", fs.Args())
		return 2
	}
	policy, err := v2agent.NewPolicy(*policyName, *seed)
	if err != nil {
		fmt.Fprintf(stderr, "sbagent: %v\n", err)
		return 2
	}
	if *name == "" {
		*name = "sbagent-" + *policyName
	}
	opts := v2agent.Options{Name: *name, Version: *version, NoEcho: *noEcho, Log: stderr}
	if *quiet {
		opts.Log = nil
	}
	agent, err := v2agent.New(policy, opts)
	if err != nil {
		fmt.Fprintf(stderr, "sbagent: %v\n", err)
		return 2
	}
	if err := agent.Serve(stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "sbagent: %v\n", err)
		return 1
	}
	return 0
}
