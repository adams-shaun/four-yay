// Command gentypes writes web/src/protocol.ts from package protocol's
// structs. `make gentypes` regenerates; `make lint` runs it with -check and
// fails when the committed file is stale, so the client's types can never
// drift from the server's.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/types"
	"os"
	"path/filepath"

	"github.com/adams-shaun/gorge/internal/tsgen"
	"github.com/adams-shaun/gorge/protocol"
)

const header = "// TypeScript twins of package protocol (github.com/adams-shaun/gorge/protocol).\n" +
	"// Regenerate with `make gentypes`; `make lint` fails when this file is stale.\n"

// Render is the whole generation, shared by main and the freshness test.
func Render() (string, error) {
	const protoPkg, decisionPkg = "github.com/adams-shaun/gorge/protocol", "github.com/adams-shaun/gorge/decision"
	rootNames := []struct{ pkg, name string }{
		{protoPkg, "Frame"}, {protoPkg, "Hello"}, {protoPkg, "TableInfo"},
		{protoPkg, "Widget"}, {protoPkg, "SeatInfo"}, {protoPkg, "MatchStart"},
		{protoPkg, "Snapshot"}, {protoPkg, "Event"}, {protoPkg, "EventBody"},
		{protoPkg, "DecisionBody"}, {protoPkg, "MatchEnd"}, {protoPkg, "TableHaltedBody"},
		{protoPkg, "Overflow"}, {protoPkg, "ErrorBody"}, {protoPkg, "MatchInfo"},
		{protoPkg, "Subscribe"}, {protoPkg, "Unsubscribe"},
		{protoPkg, "BotPolicyList"},
		// decision.Intent is the client's ANSWER — the one wire type a human
		// needs. Every other decision type reaches the client through View,
		// but nothing reaches it from the server's offered decisions, so
		// without this root the wire-drift gate could never see drift on the
		// request shape (M2e-4, R-E4-3).
		{decisionPkg, "Intent"},
	}
	// The structs are read from SOURCE (go/types), not by reflection.
	loader := tsgen.NewLoader()
	roots := make([]types.Type, 0, len(rootNames))
	for _, r := range rootNames {
		t, err := loader.Lookup(r.pkg, r.name)
		if err != nil {
			return "", err
		}
		roots = append(roots, t)
	}
	unions := map[string][]string{
		"FrameType":  {"hello", "widget", "match_start", "snapshot", "event", "decision", "match_end", "table_halted", "overflow", "error", "rewind"},
		"Mode":       {protocol.ModeOverview, protocol.ModeFocus},
		"TableState": {protocol.TableIdle, protocol.TableLive, protocol.TableCooldown, protocol.TableHalted},
		"MatchState": {protocol.MatchLive, protocol.MatchFinished, protocol.MatchAborted, protocol.MatchCrashed},
		"Visibility": {"seat", "public", "omniscient"},
		"StackKind":  {"spell", "ability", "trigger"},
		"Phase":      {"beginning", "main1", "combat", "main2", "ending", ""},
		"BotTier":    {"production", "experimental"},
	}
	return tsgen.Generate(tsgen.Options{Roots: roots, Unions: unions, Header: header})
}

func main() {
	out := flag.String("o", "web/src/protocol.ts", "output path")
	check := flag.Bool("check", false, "exit 1 if the output file is stale instead of writing it")
	flag.Parse()
	src, err := Render()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gentypes:", err)
		os.Exit(1)
	}
	if *check {
		cur, err := os.ReadFile(*out)
		if err != nil || !bytes.Equal(cur, []byte(src)) {
			fmt.Fprintf(os.Stderr, "gentypes: %s is stale; run make gentypes\n", *out)
			os.Exit(1)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gentypes:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(src), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gentypes:", err)
		os.Exit(1)
	}
}
