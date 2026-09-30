// searchbench is the reproducible, offline-only native Gorge search-study
// front door. It has no hosted-table integration.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	if args[0] == "compare" {
		return compare(args[1:], out)
	}
	if args[0] == "baselines" {
		return baselines(args[1:], out)
	}
	if args[0] == "source" && args[1] == "audit" {
		return sourceAudit(args[2:], out)
	}
	return usage()
}

func usage() error {
	return fmt.Errorf("usage: searchbench manifest validate -in <manifest.json>\n       searchbench analyze -manifest <manifest.json> [-split test] [-boot 1000] [-seed 0] [-qscale 2] [-json <out.json>] -results <results.jsonl> [<results.jsonl>...]\n       searchbench compare -manifest <manifest.json> -a <results.jsonl> -b <results.jsonl> [-split test] [-boot 1000] [-seed 0] [-json <out.json>]\n       searchbench baselines -manifest <manifest.json> -out <dir> [-seed 1]\n       searchbench source audit -in <17lands.csv[.gz]>")
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
