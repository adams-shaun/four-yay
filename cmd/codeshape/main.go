// Command codeshape prints the rules engine's code-shape metrics
// (internal/codeshape): functions over 300 lines in rules/ and effects/,
// Engine method count, effects.Host width, effects.Ctx and resumePoint field
// counts, string-literal Params reads and case literals, and the
// effects.Ctx / SpecContext / TriggerContext composite literals outside the
// context-constructor files.
//
//	go run ./cmd/codeshape            # JSON (the steward axis reads this)
//	go run ./cmd/codeshape -table     # human table, long functions listed
//	go run ./cmd/codeshape -root DIR  # measure another checkout
//
// Without -root it walks up from the working directory to the nearest go.mod.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/adams-shaun/gorge/internal/codeshape"
)

func main() {
	root := flag.String("root", "", "module root to measure (default: nearest go.mod above the working directory)")
	table := flag.Bool("table", false, "print a human-readable table instead of JSON")
	top := flag.Int("top", 0, "with -table, list at most this many long functions (0 = all)")
	flag.Parse()
	if err := run(os.Stdout, *root, *table, *top); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(w io.Writer, root string, table bool, top int) error {
	if root == "" {
		var err error
		if root, err = moduleRoot(); err != nil {
			return err
		}
	}
	m, err := codeshape.Measure(root)
	if err != nil {
		return err
	}
	if !table {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	rows := []struct {
		name string
		v    int
	}{
		{"funcs over 300 lines (rules+effects)", m.FuncsOver300},
		{"Engine methods (rules)", m.EngineMethods},
		{"effects.Host methods", m.HostMethods},
		{"effects.Host embeds", m.HostEmbeds},
		{"effects.Host direct methods", m.HostDirectMethods},
		{"effects.Host largest role", m.HostRoleMaxMethods},
		{"effects optional-host assertions", m.HostOptionalAssertions},
		{"effects.Ctx named fields", m.CtxFields},
		{"effects.Ctx embeds", m.CtxEmbeds},
		{"resumePoint fields", m.ResumePointFields},
		{"Params[\"lit\"] reads", m.StringParamReads},
		{"Params distinct literal keys", m.StringParamKeys},
		{"case \"lit\" literals", m.StringCaseLiterals},
		{"effects.Ctx literals (outside constructors)", m.CtxLiterals},
		{"effects.SpecContext literals (outside constructors)", m.SpecContextLiterals},
		{"effects.TriggerContext literals (outside constructors)", m.TriggerContextLiterals},
		{"trigmatch.Board methods", m.TrigmatchBoardMethods},
		{"files parsed", m.Files},
	}
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\n", r.name, r.v)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nfunctions over %d lines:\n", codeshape.LongFuncLines)
	tw = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for i, f := range m.LongFuncs {
		if top > 0 && i >= top {
			fmt.Fprintf(tw, "...\t%d more\n", len(m.LongFuncs)-top)
			break
		}
		fmt.Fprintf(tw, "%d\t%s\t%s:%d\n", f.Lines, f.Name, f.File, f.Line)
	}
	return tw.Flush()
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("codeshape: no go.mod above the working directory; pass -root")
		}
		dir = parent
	}
}
