// Command sbv2shadow is the reverse-adapter shadow-state check: it joins
// sbv2engine's test-mode truth side channel (-truth) with sbagent's belief
// log (-belief-out) on (game_id, seat, seat_step) and prints, field by
// field, how often the agent's reconstruction disagrees with engine truth.
//
//	sbv2shadow -truth a.jsonl[,b.jsonl] -belief x.jsonl[,y.jsonl] [-json report.json]
//
// Exit status: 0 on success, 1 on a read error, 2 for bad arguments.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/spellbench/v2engine"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbv2shadow", flag.ContinueOnError)
	fs.SetOutput(stderr)
	truthFiles := fs.String("truth", "", "comma list of sbv2engine -truth files")
	beliefFiles := fs.String("belief", "", "comma list of sbagent -belief-out files")
	jsonOut := fs.String("json", "", "also write the report as JSON to this file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *truthFiles == "" || *beliefFiles == "" {
		fmt.Fprintln(stderr, "sbv2shadow: -truth and -belief are required")
		return 2
	}
	var truth []v2engine.TruthRecord
	for _, p := range strings.Split(*truthFiles, ",") {
		t, err := v2engine.ReadTruth(p)
		if err != nil {
			fmt.Fprintf(stderr, "sbv2shadow: %s: %v\n", p, err)
			return 1
		}
		truth = append(truth, t...)
	}
	var belief []v2agent.BeliefRecord
	for _, p := range strings.Split(*beliefFiles, ",") {
		b, err := v2engine.ReadBelief(p)
		if err != nil {
			fmt.Fprintf(stderr, "sbv2shadow: %s: %v\n", p, err)
			return 1
		}
		belief = append(belief, b...)
	}
	rep := v2engine.CompareShadow(truth, belief)
	v2engine.WriteShadowTable(stdout, rep)
	if *jsonOut != "" {
		raw, _ := json.MarshalIndent(rep, "", " ")
		if err := os.WriteFile(*jsonOut, append(raw, '\n'), 0o644); err != nil {
			fmt.Fprintf(stderr, "sbv2shadow: %v\n", err)
			return 1
		}
	}
	return 0
}
