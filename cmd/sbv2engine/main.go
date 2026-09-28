// Command sbv2engine serves the ENVIRONMENT role of SpellBench protocol v2
// over the real gorge rules engine (internal/spellbench/v2engine): NDJSON
// hello / reset / step / validate_deck on stdin/stdout until stdin closes.
// The reference python host drives it like any v2 engine, which is how
// SpellBench agents -- cmd/sbagent and the python builtins -- are run "in
// reverse" on gorge (design spec, "v2 on gorge (reverse adapter)").
//
//	sbv2engine [-dir .cards] [-mana manual|autopay] [-truth FILE] [-stats FILE] [-version V] [-quiet]
//
// -truth enables the TEST-MODE truth side channel (gzip-compressed when the
// name ends in .gz; each engine process appends one gzip member): one JSON line per posed
// decision with engine truth for the acting seat (gorge's own seat view plus
// the hidden library and hand contents), for the offline shadow-state check.
// It is written to a local file only; never use it in a rated run.
//
// -stats writes the server's counters (posed decisions by translator,
// enumerated fallbacks, dropped options, gorge refusals, protocol errors,
// halts) as JSON when stdin closes; they also go to stderr unless -quiet.
//
// Exit status: 0 at stdin EOF, 1 on an I/O error, 2 for bad arguments.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/adams-shaun/gorge/internal/spellbench/v2engine"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbv2engine", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".cards", "corpus directory (ir.gob.gz / cardsfolder)")
	mana := fs.String("mana", "manual", "mana surface: manual (mana abilities are candidates) or autopay (engine_autopay)")
	truth := fs.String("truth", "", "TEST MODE: write the engine-truth side channel to this file (JSON lines)")
	statsOut := fs.String("stats", "", "write the server counters to this file (JSON) at EOF")
	version := fs.String("version", "dev", "engine version reported in hello_ok and provenance")
	quiet := fs.Bool("quiet", false, "no diagnostics on stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reg, err := testutil.OpenCorpusRegistry(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "sbv2engine: opening corpus %s: %v\n", *dir, err)
		return 2
	}
	opts := v2engine.Options{Registry: reg, Mana: *mana, Version: *version}
	if !*quiet {
		opts.Log = stderr
	}
	if *truth != "" {
		f, err := os.OpenFile(*truth, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(stderr, "sbv2engine: %v\n", err)
			return 2
		}
		defer f.Close()
		opts.Truth = f
		if strings.HasSuffix(*truth, ".gz") {
			// one gzip member per process: readers concatenate members
			zw := gzip.NewWriter(f)
			defer zw.Close()
			opts.Truth = zw
		}
	}
	srv, err := v2engine.New(opts)
	if err != nil {
		fmt.Fprintf(stderr, "sbv2engine: %v\n", err)
		return 2
	}
	serveErr := srv.Serve(stdin, stdout)
	if !*quiet {
		srv.DumpStats(stderr)
	}
	if *statsOut != "" {
		raw, _ := json.MarshalIndent(srv.Stats().Snapshot(), "", " ")
		if err := os.WriteFile(*statsOut, append(raw, '\n'), 0o644); err != nil {
			fmt.Fprintf(stderr, "sbv2engine: %v\n", err)
		}
	}
	if serveErr != nil {
		fmt.Fprintf(stderr, "sbv2engine: %v\n", serveErr)
		return 1
	}
	return 0
}
