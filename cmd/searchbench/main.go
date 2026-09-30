// searchbench is the reproducible, offline-only native Gorge search-study
// front door. It has no hosted-table integration.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/searchbench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
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
	if args[0] == "compare" {
		return compare(args[1:], out)
	}
	if args[0] == "baselines" {
		return baselines(args[1:], out)
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
	if args[0] == "source" && args[1] == "resolution-audit" {
		return resolutionAudit(args[2:], out)
	}
	if args[0] == "source" && args[1] == "corpus-audit" {
		return corpusAudit(args[2:], out)
	}
	if args[0] == "source" && args[1] == "genesis-audit" {
		return genesisAudit(args[2:], out)
	}
	if args[0] == "source" && args[1] == "stage-audit" {
		return stageAudit(args[2:], out)
	}
	if args[0] == "source" && args[1] == "root-audit" {
		return rootAudit(args[2:], out)
	}
	if args[0] == "source" && args[1] == "root-verify" {
		return rootVerify(args[2:], out)
	}
	if args[0] == "source" && args[1] == "root-run" {
		return rootRun(args[2:], out)
	}
	if args[0] == "source" && args[1] == "root-summarize" {
		return rootSummarize(args[2:], out)
	}
	return usage()
}

func usage() error {
	return fmt.Errorf("usage: searchbench manifest validate -in <manifest.json>\n       searchbench analyze -manifest <manifest.json> [-split test] [-boot 1000] [-seed 0] [-qscale 2] [-json <out.json>] -results <results.jsonl> [<results.jsonl>...]\n       searchbench compare -manifest <manifest.json> -a <results.jsonl> -b <results.jsonl> [-split test] [-boot 1000] [-seed 0] [-json <out.json>]\n       searchbench baselines -manifest <manifest.json> -out <dir> [-seed 1]\n       searchbench source audit -in <17lands.csv[.gz]>\n       searchbench source candidates -in <17lands.csv[.gz]>\n       searchbench source replay-audit -in <17lands.csv[.gz]>\n       searchbench source resolution-audit -in <17lands.csv[.gz]> -cards <cards.csv>\n       searchbench source corpus-audit -in <17lands.csv[.gz]> -cards <cards.csv> -corpus <.cards>\n       searchbench source genesis-audit -in <17lands.csv[.gz]> -cards <cards.csv> -corpus <.cards>\n       searchbench source stage-audit -in <17lands.csv[.gz]> -cards <cards.csv> -corpus <.cards>\n       searchbench source root-audit -in <17lands.csv[.gz]> -cards <cards.csv> -corpus <.cards>\n       searchbench source root-verify -roots <roots.jsonl> -in <17lands.csv[.gz]> -cards <cards.csv> -corpus <.cards>\n       searchbench source root-run -roots <roots.jsonl> -in <17lands.csv[.gz]> -cards <cards.csv> -corpus <.cards> -arm <clairvoyant-mcts|pimc-1|pimc-4|is-mcts>\n       searchbench source root-summarize -results <root-run.jsonl>")
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

// parseInterleaved parses flags that may follow positional arguments, so
// "-results a.jsonl b.jsonl -json out.json" works; it returns the positionals.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return rest, nil
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

type fileList []string

func (f *fileList) String() string     { return fmt.Sprint(*f) }
func (f *fileList) Set(v string) error { *f = append(*f, v); return nil }

func analysisFlags(fs *flag.FlagSet) (*string, *string, *int, *uint64) {
	manifest := fs.String("manifest", "", "sealed manifest path")
	split := fs.String("split", string(searchbench.SplitTest), "split to score: test or dev")
	boot := fs.Int("boot", searchbench.DefaultBootstrap.Resamples, "bootstrap resamples (0 disables CIs)")
	seed := fs.Uint64("seed", searchbench.DefaultBootstrap.Seed, "bootstrap seed (CPython random.Random seed)")
	return manifest, split, boot, seed
}

func bindFile(m searchbench.Manifest, split searchbench.Split, path string) (searchbench.Run, error) {
	rows, err := searchbench.ReadResults(path)
	if err != nil {
		return searchbench.Run{}, err
	}
	run, err := searchbench.BindResults(m, split, rows)
	if err != nil {
		return searchbench.Run{}, fmt.Errorf("%s: %w", path, err)
	}
	return run, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// analyze scores one or more result files over one manifest split: one row
// per file, as markdown on stdout and optionally JSON.
func analyze(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest, split, boot, seed := analysisFlags(fs)
	var results fileList
	fs.Var(&results, "results", "result JSONL (repeatable; further paths may follow)")
	jsonOut := fs.String("json", "", "write the report JSON here")
	qscale := fs.Float64("qscale", searchbench.DefaultQScale, "factor putting Q gaps on upstream's [-1,1] value scale")
	rest, err := parseInterleaved(fs, args)
	if err != nil || *manifest == "" || *boot < 0 {
		return usage()
	}
	results = append(results, rest...)
	if len(results) == 0 {
		return usage()
	}
	m, err := searchbench.Read(*manifest)
	if err != nil {
		return err
	}
	opt := searchbench.AnalyzeOptions{Split: searchbench.Split(*split), Bootstrap: searchbench.Bootstrap{Resamples: *boot, Seed: *seed}, QScale: *qscale}
	runs := make([]searchbench.Run, 0, len(results))
	for _, p := range results {
		run, err := bindFile(m, opt.Split, p)
		if err != nil {
			return err
		}
		runs = append(runs, run)
	}
	rep, err := searchbench.Analyze(m, runs, results, opt)
	if err != nil {
		return err
	}
	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, rep); err != nil {
			return err
		}
	}
	return rep.WriteMarkdown(out)
}

// compare reports every metric's paired difference a − b over the same
// resampled games, and the two runs' same-choice rate.
func compare(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench compare", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest, split, boot, seed := analysisFlags(fs)
	a := fs.String("a", "", "result JSONL a")
	b := fs.String("b", "", "result JSONL b")
	jsonOut := fs.String("json", "", "write the comparison JSON here")
	if err := fs.Parse(args); err != nil || *manifest == "" || *a == "" || *b == "" || *boot < 0 || fs.NArg() != 0 {
		return usage()
	}
	m, err := searchbench.Read(*manifest)
	if err != nil {
		return err
	}
	opt := searchbench.AnalyzeOptions{Split: searchbench.Split(*split), Bootstrap: searchbench.Bootstrap{Resamples: *boot, Seed: *seed}}
	ra, err := bindFile(m, opt.Split, *a)
	if err != nil {
		return err
	}
	rb, err := bindFile(m, opt.Split, *b)
	if err != nil {
		return err
	}
	rep, err := searchbench.Compare(m, ra, rb, opt)
	if err != nil {
		return err
	}
	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, rep); err != nil {
			return err
		}
	}
	return rep.WriteMarkdown(out)
}

// baselines writes the always-passive, always-active and uniform-random
// result files for every manifest item.
func baselines(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench baselines", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "sealed manifest path")
	dir := fs.String("out", "", "output directory")
	seed := fs.Uint64("seed", 1, "uniform-random baseline seed")
	if err := fs.Parse(args); err != nil || *manifest == "" || *dir == "" || fs.NArg() != 0 {
		return usage()
	}
	m, err := searchbench.Read(*manifest)
	if err != nil {
		return err
	}
	arms, err := searchbench.Baselines(m, *seed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	for _, arm := range searchbench.BaselineArms() {
		p := filepath.Join(*dir, arm+".jsonl")
		if err := searchbench.WriteResults(p, arms[arm]); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "wrote %s (%d rows)\n", p, len(arms[arm])); err != nil {
			return err
		}
	}
	return nil
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

func resolutionAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source resolution-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	cardsPath := fs.String("cards", "", "17lands cards CSV")
	if err := fs.Parse(args); err != nil || *in == "" || *cardsPath == "" || fs.NArg() != 0 {
		return usage()
	}
	cards, err := searchbench.LoadCardNames(*cardsPath)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	good := 0
	for _, game := range games {
		if _, err := searchbench.ResolveReplayGame(game, cards); err == nil {
			good++
		}
	}
	_, err = fmt.Fprintf(out, "games=%d card_resolved=%d refused=%d\n", len(games), good, len(games)-good)
	return err
}

func corpusAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source corpus-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	if err := fs.Parse(args); err != nil || *in == "" || fs.NArg() != 0 {
		return usage()
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	good := 0
	for _, game := range games {
		if _, err := searchbench.ResolveDeck(reg, game.Deck); err == nil {
			good++
		}
	}
	_, err = fmt.Fprintf(out, "games=%d decks_resolved=%d refused=%d\n", len(games), good, len(games)-good)
	return err
}

func genesisAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source genesis-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	cardsPath := fs.String("cards", "", "17lands cards CSV")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	if err := fs.Parse(args); err != nil || *in == "" || *cardsPath == "" || fs.NArg() != 0 {
		return usage()
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	names, err := searchbench.LoadCardNames(*cardsPath)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	good := 0
	for i, game := range games {
		opponent, e := searchbench.ResolveDeck(reg, game.Deck)
		if e == nil {
			_, e = searchbench.NewGenesis(reg, game, names, opponent, uint64(i+1))
		}
		if e == nil {
			good++
		}
	}
	_, err = fmt.Fprintf(out, "games=%d genesis=%d refused=%d\n", len(games), good, len(games)-good)
	return err
}

func stageAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source stage-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	cardsPath := fs.String("cards", "", "17lands cards CSV")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	limit := fs.Int("limit", 100, "games to stage")
	if err := fs.Parse(args); err != nil || *in == "" || *cardsPath == "" || *limit < 1 || fs.NArg() != 0 {
		return usage()
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	names, err := searchbench.LoadCardNames(*cardsPath)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	if len(games) > *limit {
		games = games[:*limit]
	}
	good, bridges := 0, 0
	firstFailure := ""
	for i, game := range games {
		base, e := searchbench.ResolveDeck(reg, game.Deck)
		if e != nil {
			if firstFailure == "" {
				firstFailure = e.Error()
			}
			continue
		}
		opponent, e := searchbench.ObservedOpponentDeck(reg, base, game, names)
		if e != nil {
			if firstFailure == "" {
				firstFailure = e.Error()
			}
			continue
		}
		engine, e := searchbench.NewGenesis(reg, game, names, opponent, uint64(i+1))
		if e != nil {
			if firstFailure == "" {
				firstFailure = e.Error()
			}
			continue
		}
		resolved, e := searchbench.ResolveReplayGame(game, names)
		if e != nil || len(resolved.Turns) == 0 {
			if firstFailure == "" && e != nil {
				firstFailure = e.Error()
			}
			continue
		}
		b, e := searchbench.StageGame(engine, [2]*seat.Bot{seat.NewBot(uint64(i + 11)), seat.NewBot(uint64(i + 12))}, resolved)
		if e == nil {
			good++
			bridges += b
		} else if firstFailure == "" {
			firstFailure = fmt.Sprintf("game=%s on_play=%t: %v", game.ID, game.OnPlay, e)
		}
	}
	_, err = fmt.Fprintf(out, "games=%d staged=%d refused=%d bridges=%d first_failure=%q\n", len(games), good, len(games)-good, bridges, firstFailure)
	return err
}

// rootAudit counts source-labelled native roots as they are reached. Unlike
// stage-audit it retains prefixes from games that later become unstaggable:
// a benchmark decision needs a sound prefix, not a reconstructed ending.
func rootAudit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source root-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	cardsPath := fs.String("cards", "", "17lands cards CSV")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	limit := fs.Int("limit", 100, "games to stage")
	output := fs.String("out", "", "optional root JSONL output")
	if err := fs.Parse(args); err != nil || *in == "" || *cardsPath == "" || *limit < 1 || fs.NArg() != 0 {
		return usage()
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	names, err := searchbench.LoadCardNames(*cardsPath)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	if len(games) > *limit {
		games = games[:*limit]
	}
	var roots, userRoots, lands, spells, completed int
	var records []searchbench.RootRecord
	firstFailure := ""
	for i, game := range games {
		base, e := searchbench.ResolveDeck(reg, game.Deck)
		if e == nil {
			base, e = searchbench.ObservedOpponentDeck(reg, base, game, names)
		}
		if e != nil {
			if firstFailure == "" {
				firstFailure = e.Error()
			}
			continue
		}
		engine, e := searchbench.NewGenesis(reg, game, names, base, uint64(i+1))
		if e != nil {
			if firstFailure == "" {
				firstFailure = e.Error()
			}
			continue
		}
		resolved, e := searchbench.ResolveReplayGame(game, names)
		if e != nil {
			if firstFailure == "" {
				firstFailure = e.Error()
			}
			continue
		}
		ordinal := 0
		_, e = searchbench.StageGameObserve(engine, [2]*seat.Bot{seat.NewBot(uint64(i + 11)), seat.NewBot(uint64(i + 12))}, resolved, func(root *rules.Engine, d *decision.Decision, action searchbench.RecordedAction) error {
			if *output != "" {
				record, err := searchbench.NewRootRecord(game.ID, uint64(i+1), ordinal, root, d, action)
				if err != nil {
					return err
				}
				records = append(records, record)
			}
			ordinal++
			roots++
			if d.Player == 0 {
				userRoots++
			}
			if action.Intent.Payment != nil {
				spells++
				return nil
			}
			for _, option := range d.Options {
				if len(action.Intent.Choices) == 1 && option.Index == action.Intent.Choices[0] {
					switch option.Kind {
					case "play_land":
						lands++
					case "cast":
						spells++
					}
				}
			}
			return nil
		})
		if e == nil {
			completed++
		} else if firstFailure == "" {
			firstFailure = e.Error()
		}
	}
	if *output != "" {
		f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if e != nil {
			return e
		}
		w := bufio.NewWriter(f)
		enc := json.NewEncoder(w)
		for _, record := range records {
			if e := enc.Encode(record); e != nil {
				_ = f.Close()
				return e
			}
		}
		if e := w.Flush(); e != nil {
			_ = f.Close()
			return e
		}
		if e := f.Close(); e != nil {
			return e
		}
	}
	_, err = fmt.Fprintf(out, "games=%d roots=%d user_roots=%d lands=%d spells=%d completed=%d first_failure=%q\n", len(games), roots, userRoots, lands, spells, completed, firstFailure)
	return err
}

// rootVerify independently reconstructs indexed roots. It is deliberately
// bounded by -limit so operators can smoke-test a large index, or set zero to
// audit the complete immutable selection.
func rootVerify(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source root-verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	cardsPath := fs.String("cards", "", "17lands cards CSV")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	rootsPath := fs.String("roots", "", "root JSONL index")
	limit := fs.Int("limit", 1, "roots to independently replay; zero means all")
	worlds := fs.Int("worlds", 0, "sampled hidden worlds to validate per root")
	if err := fs.Parse(args); err != nil || *in == "" || *cardsPath == "" || *rootsPath == "" || *limit < 0 || *worlds < 0 || fs.NArg() != 0 {
		return usage()
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	names, err := searchbench.LoadCardNames(*cardsPath)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	byID := make(map[string]searchbench.ReplayGame, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	roots, err := searchbench.ReadRootRecords(*rootsPath)
	if err != nil {
		return err
	}
	n := len(roots)
	if *limit != 0 && *limit < n {
		n = *limit
	}
	for i := 0; i < n; i++ {
		game, ok := byID[roots[i].GameID]
		if !ok {
			return fmt.Errorf("searchbench: root %d names source game %q not present in eligible input", i, roots[i].GameID)
		}
		if _, err := searchbench.ReplayRoot(reg, names, game, roots[i]); err != nil {
			return fmt.Errorf("searchbench: root %d: %w", i, err)
		}
		for world := 0; world < *worlds; world++ {
			// This is an audit seed, not a benchmark world schedule. A future
			// sealed manifest owns the actual per-item world seeds.
			seed := uint64(i+1)<<32 | uint64(world+1)
			sampled, err := searchbench.ReplayRootWorld(reg, names, game, roots[i], seed)
			if err != nil {
				return fmt.Errorf("searchbench: root %d world %d: %w", i, world, err)
			}
			if err := searchbench.ValidateRootWorld(roots[i], sampled); err != nil {
				return fmt.Errorf("searchbench: root %d world %d: %w", i, world, err)
			}
		}
	}
	_, err = fmt.Fprintf(out, "verified=%d sampled_worlds=%d indexed=%d\n", n, n*(*worlds), len(roots))
	return err
}

// rootRun is the native four-arm search driver over independently verified
// roots. Its JSONL is intentionally diagnostic-only for now: manifest sealing
// remains the scoring boundary, so this command cannot accidentally present a
// partial replay population as the published benchmark result.
func rootRun(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source root-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	in := fs.String("in", "", "17lands FDN replay CSV")
	cardsPath := fs.String("cards", "", "17lands cards CSV")
	corpus := fs.String("corpus", ".cards", "compiled Forge corpus")
	rootsPath := fs.String("roots", "", "root JSONL index")
	armText := fs.String("arm", "", "clairvoyant-mcts, pimc-1, pimc-4, or is-mcts")
	limit := fs.Int("limit", 1, "roots to run")
	includeOpponent := fs.Bool("include-opponent", false, "also run opponent-seat roots (diagnostic only)")
	sourceKind := fs.String("source-kind", "", "restrict to source action kind: land or spell")
	sims := fs.Int("sims", 100, "simulations per root")
	seed := fs.Uint64("seed", 1, "search policy seed")
	if err := fs.Parse(args); err != nil || *in == "" || *cardsPath == "" || *rootsPath == "" || *limit < 1 || *sims < 1 || *seed == 0 || (*sourceKind != "" && *sourceKind != "land" && *sourceKind != "spell" && *sourceKind != "attack") || fs.NArg() != 0 {
		return usage()
	}
	arm, err := searchbench.ParseArm(*armText)
	if err != nil {
		return err
	}
	if arm == searchbench.ArmClairvoyant {
		// The measurement command's explicit opt-in (azmcts/clairvoyant).
		clairvoyant.AllowClairvoyant()
	}
	reg, err := cards.OpenCorpus(*corpus)
	if err != nil {
		return err
	}
	names, err := searchbench.LoadCardNames(*cardsPath)
	if err != nil {
		return err
	}
	games, err := searchbench.ReplayGamesCSV(*in, searchbench.DefaultSourceFilter())
	if err != nil {
		return err
	}
	byID := make(map[string]searchbench.ReplayGame, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	roots, err := searchbench.ReadRootRecords(*rootsPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	selected := make([]int, 0, *limit)
	for i := range roots {
		if !*includeOpponent && roots[i].Seat != 0 {
			continue
		}
		if *sourceKind != "" && roots[i].SourceKind != *sourceKind {
			continue
		}
		selected = append(selected, i)
		if len(selected) == *limit {
			break
		}
	}
	for outputOrdinal, i := range selected {
		game, ok := byID[roots[i].GameID]
		if !ok {
			return fmt.Errorf("searchbench: root %d source game %q absent", i, roots[i].GameID)
		}
		root, err := searchbench.ReplayRoot(reg, names, game, roots[i])
		if err != nil {
			return fmt.Errorf("searchbench: root %d: %w", i, err)
		}
		worldCount := 1
		switch arm {
		case searchbench.ArmPIMC4:
			worldCount = searchbench.PIMCWorlds
		case searchbench.ArmISMCTS:
			worldCount = searchbench.WorldCount
		}
		worlds := make([]searchbench.ReplayedRoot, 0, worldCount)
		worldSeeds := searchbench.SBV1WorldSeeds()
		for w := 0; w < worldCount; w++ {
			world, err := searchbench.ReplayRootWorld(reg, names, game, roots[i], worldSeeds[w])
			if err != nil {
				return fmt.Errorf("searchbench: root %d world %d: %w", i, w, err)
			}
			if err := searchbench.ValidateRootWorld(roots[i], world); err != nil {
				return fmt.Errorf("searchbench: root %d world %d: %w", i, w, err)
			}
			worlds = append(worlds, world)
		}
		in := searchbench.ArmInput{Arm: arm, Options: searchbench.BenchOptions(*sims), Seed: *seed + uint64(outputOrdinal), Real: root.Engine}
		for _, w := range worlds {
			in.Worlds = append(in.Worlds, w.Engine)
		}
		result, err := searchbench.RunArm(context.Background(), in)
		if err != nil {
			return fmt.Errorf("searchbench: root %d %s: %w", i, arm, err)
		}
		if err := enc.Encode(searchbench.NativeRunResult{GameID: roots[i].GameID, SourceKind: roots[i].SourceKind, SourceCard: roots[i].SourceCard, Ordinal: roots[i].Ordinal, Seat: int(roots[i].Seat), Arm: arm, Choice: result.Choice, Intent: result.Intent, Recorded: roots[i].Recorded, MatchRecorded: searchbench.SameRecordedAction(result.Intent, roots[i].Recorded), Sims: result.Stats.Simulations, Completed: result.Stats.Completed, Skipped: result.Stats.Skipped}); err != nil {
			return err
		}
	}
	return nil
}

func rootSummarize(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("searchbench source root-summarize", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	results := fs.String("results", "", "root-run JSONL")
	if err := fs.Parse(args); err != nil || *results == "" || fs.NArg() != 0 {
		return usage()
	}
	rows, err := searchbench.ReadNativeRunResults(*results)
	if err != nil {
		return err
	}
	summary, arm, err := searchbench.SummarizeNativeRuns(rows)
	if err != nil {
		return err
	}
	agreement, ok := summary.Agreement()
	if !ok {
		_, err = fmt.Fprintf(out, "arm=%s rows=%d searched=0 skipped=%d sims=%d completed=%d agreement=unavailable\n", arm, summary.Rows, summary.Skipped, summary.Simulations, summary.Completed)
		return err
	}
	_, err = fmt.Fprintf(out, "arm=%s rows=%d searched=%d skipped=%d sims=%d completed=%d matches=%d agreement=%.6f\n", arm, summary.Rows, summary.Searched, summary.Skipped, summary.Simulations, summary.Completed, summary.Matched, agreement)
	return err
}
