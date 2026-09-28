// Command sbv2shadow is the reverse-adapter shadow-state check: it joins
// sbv2engine's test-mode truth side channel (-truth) with sbagent's belief
// log (-belief-out) on (game_id, seat, seat_step) and prints, field by
// field, how often the agent's reconstruction disagrees with engine truth.
//
//	sbv2shadow -truth a.jsonl[.gz][,b...] (-belief x.jsonl[,y...] | -belief-dir DIR) [-json report.json]
//
// The comparison streams: truth files are read game by game (a worker's
// side channel holds its games one after another) and each game is joined
// with the belief logs whose first record names it, so memory stays at one
// game whatever the run's size.
//
// Exit status: 0 on success, 1 on a read error, 2 for bad arguments.
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

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/spellbench/v2engine"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbv2shadow", flag.ContinueOnError)
	fs.SetOutput(stderr)
	truthFiles := fs.String("truth", "", "comma list of sbv2engine -truth files")
	beliefFiles := fs.String("belief", "", "comma list of sbagent -belief-out files")
	beliefDir := fs.String("belief-dir", "", "directory of sbagent -belief-out files (*.jsonl)")
	jsonOut := fs.String("json", "", "also write the report as JSON to this file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *truthFiles == "" || (*beliefFiles == "" && *beliefDir == "") {
		fmt.Fprintln(stderr, "sbv2shadow: -truth and -belief or -belief-dir are required")
		return 2
	}
	var paths []string
	if *beliefFiles != "" {
		paths = strings.Split(*beliefFiles, ",")
	}
	if *beliefDir != "" {
		m, _ := filepath.Glob(filepath.Join(*beliefDir, "*.jsonl"))
		sort.Strings(m)
		paths = append(paths, m...)
	}
	byGame := map[string][]string{}
	var order []string
	for _, p := range paths {
		id, err := v2engine.BeliefGameID(p)
		if err != nil {
			fmt.Fprintf(stderr, "sbv2shadow: %s: %v\n", p, err)
			return 1
		}
		if id == "" {
			continue
		}
		if _, ok := byGame[id]; !ok {
			order = append(order, id)
		}
		byGame[id] = append(byGame[id], p)
	}
	sh := v2engine.NewShadow()
	joined := map[string]bool{}
	for _, tp := range strings.Split(*truthFiles, ",") {
		var readErr error
		err := v2engine.StreamTruth(tp, func(gameID string, truth []v2engine.TruthRecord) {
			files, ok := byGame[gameID]
			if !ok || readErr != nil {
				return
			}
			var belief []v2agent.BeliefRecord
			for _, p := range files {
				b, err := v2engine.ReadBelief(p)
				if err != nil {
					readErr = fmt.Errorf("%s: %v", p, err)
					return
				}
				belief = append(belief, b...)
			}
			joined[gameID] = true
			sh.AddGame(truth, belief)
		})
		if err == nil {
			err = readErr
		}
		if err != nil {
			fmt.Fprintf(stderr, "sbv2shadow: %s: %v\n", tp, err)
			return 1
		}
	}
	for _, id := range order {
		if joined[id] {
			continue
		}
		for _, p := range byGame[id] {
			b, err := v2engine.ReadBelief(p)
			if err != nil {
				fmt.Fprintf(stderr, "sbv2shadow: %s: %v\n", p, err)
				return 1
			}
			sh.AddGame(nil, b) // every record counts as unjoined
		}
	}
	rep := sh.Report()
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
