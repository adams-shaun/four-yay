// searchbench is the reproducible, offline-only native Gorge search-study
// front door. It has no hosted-table integration.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge/internal/searchbench"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) < 2 {
		return usage()
	}
	if args[0] == "manifest" && args[1] == "validate" {
		return validate(args[2:], out)
	}
	if args[0] == "analyze" {
		return analyze(args[1:], out)
	}
	if args[0] == "source" && args[1] == "audit" {
		return sourceAudit(args[2:], out)
	}
	if args[0] == "source" && args[1] == "candidates" {
		return sourceCandidates(args[2:], out)
	}
	if args[0] == "source" && args[1] == "replay-audit" {
		return replayAudit(args[2:], out)
	}
	return usage()
}

func usage() error {
	return fmt.Errorf("usage: searchbench manifest validate -in <manifest.json>\n       searchbench analyze -manifest <manifest.json> -results <results.jsonl>\n       searchbench source audit -in <17lands.csv[.gz]>\n       searchbench source candidates -in <17lands.csv[.gz]>\n       searchbench source replay-audit -in <17lands.csv[.gz]>")
}

func validate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench manifest validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "sealed manifest path")
	if err := fs.Parse(args); err != nil || *in == "" || fs.NArg() != 0 {
		return usage()
	}
	m, err := searchbench.Read(*in)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "valid %s schema=%d digest=%s dev=%d test=%d\n", m.Kind, m.SchemaVersion, m.Digest, m.Selection.Dev, m.Selection.Test)
	return err
}

func analyze(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "sealed manifest path")
	results := fs.String("results", "", "one-arm JSONL results path")
	if err := fs.Parse(args); err != nil || *manifest == "" || *results == "" || fs.NArg() != 0 {
		return usage()
	}
	m, err := searchbench.Read(*manifest)
	if err != nil {
		return err
	}
	rows, err := searchbench.ReadResults(*results)
	if err != nil {
		return err
	}
	s, arm, err := searchbench.Analyze(m, rows)
	if err != nil {
		return err
	}
	macro, macroOK := s.MacroAgreement()
	balanced, balancedOK := s.BalancedAgreement()
	if !macroOK || !balancedOK {
		return fmt.Errorf("searchbench: result lacks all four types or both action classes")
	}
	_, err = fmt.Fprintf(out, "arm=%s items=%d macro_agreement=%.6f balanced_agreement=%.6f\n", arm, len(rows), macro, balanced)
	return err
}

func sourceAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	minWR := fs.Float64("min-win-rate", .60, "minimum user game win-rate bucket")
	minGames := fs.Int("min-games", 100, "minimum user games bucket")
	if err := fs.Parse(args); err != nil || *in == "" || fs.NArg() != 0 {
		return usage()
	}
	a, err := searchbench.AuditCSV(*in, searchbench.SourceFilter{MinimumGameWinRate: *minWR, MinimumGames: *minGames})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "rows=%d fdn_premier=%d eligible=%d malformed_eligibility=%d\n", a.Rows, a.FDNPremierRows, a.EligibleRows, a.MalformedEligibilityRows)
	return err
}

func sourceCandidates(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source candidates", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	max := fs.Int("max-per-game", 2, "pre-reconstruction candidates per game")
	if err := fs.Parse(args); err != nil || *in == "" || fs.NArg() != 0 {
		return usage()
	}
	_, a, err := searchbench.CandidatesCSV(*in, searchbench.DefaultSourceFilter(), *max)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "eligible_games=%d candidates=%d spell=%d hold=%d attack=%d block=%d\n", a.EligibleGames, a.Candidates, a.ByType[0], a.ByType[1], a.ByType[2], a.ByType[3])
	return err
}

func replayAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source replay-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	if err := fs.Parse(args); err != nil || *in == "" || fs.NArg() != 0 {
		return usage()
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	var deckCards, actions int
	for i := range games {
		for _, c := range games[i].Deck {
			deckCards += c.Count
		}
		for _, turn := range games[i].Turns {
			actions += len(turn.User.Lands) + len(turn.User.Creatures) + len(turn.User.NonCreatures) + len(turn.User.Instants) + len(turn.User.Abilities) + len(turn.User.Attackers) + len(turn.User.Blockers) + len(turn.Opponent.Lands) + len(turn.Opponent.Creatures) + len(turn.Opponent.NonCreatures) + len(turn.Opponent.Instants) + len(turn.Opponent.Abilities) + len(turn.Opponent.Attackers) + len(turn.Opponent.Blockers)
		}
	}
	_, err = fmt.Fprintf(out, "replay_games=%d deck_cards=%d action_ids=%d\n", len(games), deckCards, actions)
	return err
}
