package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultWallLimitS  = 60
	defaultRSSLimitKiB = 2 * 1024 * 1024
	// A pinned exception that now measures under staleFraction of the limit
	// is fixed, and its row must be deleted. The margin keeps a test sitting
	// on the limit from flapping the batch red.
	staleFraction = 0.8
	// A pinned wall exception may wobble; it fails only past this multiple.
	regressFactor = 1.25
	unseededMark  = "!unseeded"
)

// exceptions is a shrink-only inventory: package<TAB>test<TAB>measured rows.
// While unseeded is set the inventory has never been populated from a
// whole-suite run, so check reports offenders without failing.
type exceptions struct {
	unseeded bool
	rows     map[string]int64
}

func parseExceptions(data string) (exceptions, error) {
	e := exceptions{rows: map[string]int64{}}
	for _, line := range strings.Split(data, "\n") {
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case line == unseededMark:
			e.unseeded = true
		default:
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				return e, fmt.Errorf("row %q: want package<TAB>test<TAB>measured", line)
			}
			n, err := strconv.ParseInt(f[2], 10, 64)
			if err != nil {
				return e, fmt.Errorf("row %q: %v", line, err)
			}
			e.rows[f[0]+"\t"+f[1]] = n
		}
	}
	return e, nil
}

func (e exceptions) hasPackage(pkg string) bool {
	for k := range e.rows {
		if strings.HasPrefix(k, pkg+"\t") {
			return true
		}
	}
	return false
}

// finding is one budget verdict. fail findings turn the exit code red.
type finding struct {
	fail bool
	text string
	row  string // a ready-to-paste exception row, when the finding is an offence
}

// checkWall judges every top-level test against the per-test wall limit.
func checkWall(m *measured, exc exceptions, limitS float64) []finding {
	var out []finding
	keys := make([]string, 0, len(m.tests))
	for k := range m.tests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := m.tests[k]
		pinned, isPinned := exc.rows[k]
		heavy := ""
		if t.sub != "" {
			heavy = fmt.Sprintf(" (heaviest subtest %s %.1fs)", t.sub, t.subWall)
		}
		switch {
		case t.wall > limitS && !isPinned:
			out = append(out, finding{true, fmt.Sprintf("OVER-BUDGET wall   %s %s  %.1fs > %.0fs%s: split it into independent chunk tests",
				t.pkg, t.name, t.wall, limitS, heavy), fmt.Sprintf("%s\t%s\t%d", t.pkg, t.name, int64(math.Ceil(t.wall)))})
		case t.wall > limitS && t.wall > float64(pinned)*regressFactor:
			out = append(out, finding{true, fmt.Sprintf("REGRESSED     wall   %s %s  %.1fs, pinned at %ds (allowed up to %.0fs)%s",
				t.pkg, t.name, t.wall, pinned, float64(pinned)*regressFactor, heavy), ""})
		case t.wall > limitS:
			out = append(out, finding{false, fmt.Sprintf("known-over    wall   %s %s  %.1fs (pinned %ds)%s", t.pkg, t.name, t.wall, pinned, heavy), ""})
		case isPinned && t.wall < limitS*staleFraction:
			out = append(out, finding{true, fmt.Sprintf("STALE-ROW     wall   %s %s  now %.1fs: delete its row from the wall exceptions", t.pkg, t.name, t.wall), ""})
		}
	}
	return out
}

// checkRSS judges each test binary's peak RSS. A binary is one process for all
// its tests, so its peak bounds every test in it; a package over the limit is
// covered only if the RSS exception inventory already names a test in it.
func checkRSS(peaks map[string]int64, exc exceptions, limitKiB int64) []finding {
	var out []finding
	pkgs := make([]string, 0, len(peaks))
	for p := range peaks {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		kib := peaks[p]
		has := exc.hasPackage(p)
		switch {
		case kib > limitKiB && !has:
			out = append(out, finding{true, fmt.Sprintf("OVER-BUDGET rss    %s  test binary peaked at %d KiB > %d KiB: find the test with a capped `-run '^TestX$'` run and shrink it",
				p, kib, limitKiB), fmt.Sprintf("%s\t<test>\t%d", p, kib)})
		case kib > limitKiB:
			out = append(out, finding{false, fmt.Sprintf("known-over    rss    %s  test binary peaked at %d KiB (inventory names its test)", p, kib), ""})
		case has && float64(kib) < float64(limitKiB)*staleFraction:
			out = append(out, finding{true, fmt.Sprintf("STALE-ROW     rss    %s  test binary now peaks at %d KiB: delete its rows from the RSS exceptions", p, kib), ""})
		}
	}
	return out
}

func moduleName(gomod string) (string, error) {
	b, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if m, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			return strings.TrimSpace(m), nil
		}
	}
	return "", fmt.Errorf("%s: no module line", gomod)
}

func runCheck(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(errw)
	events := fs.String("events", "", "per-test events file written by `testbudget tee` (or `go test -json` output)")
	rssDir := fs.String("rss-dir", "", "directory the `testbudget exec` wrapper recorded peak RSS in")
	wallFile := fs.String("wall-exceptions", "internal/testutil/testdata/wall_exceptions.txt", "shrink-only wall inventory")
	rssFile := fs.String("rss-exceptions", "internal/testutil/testdata/rss_exceptions.txt", "shrink-only RSS inventory")
	gomod := fs.String("gomod", "go.mod", "go.mod naming the module")
	wallLimit := fs.Float64("wall-limit-s", defaultWallLimitS, "per-test wall limit, seconds")
	rssLimit := fs.Int64("rss-limit-kib", defaultRSSLimitKiB, "per-binary peak RSS limit, KiB")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *events == "" {
		fmt.Fprintln(errw, "testbudget check: -events is required")
		return 2
	}
	fail := func(err error) int { fmt.Fprintf(errw, "testbudget check: %v\n", err); return 2 }
	module, err := moduleName(*gomod)
	if err != nil {
		return fail(err)
	}
	f, err := os.Open(*events)
	if err != nil {
		return fail(err)
	}
	m, err := parseEvents(f, module)
	f.Close()
	if err != nil {
		return fail(err)
	}
	if len(m.tests) == 0 {
		fmt.Fprintf(errw, "testbudget check: %s holds no test results; a budget check over nothing proves nothing\n", *events)
		return 1
	}
	read := func(path string) (exceptions, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return exceptions{}, err
		}
		e, err := parseExceptions(string(b))
		if err != nil {
			return e, fmt.Errorf("%s: %v", path, err)
		}
		return e, nil
	}
	wallExc, err := read(*wallFile)
	if err != nil {
		return fail(err)
	}
	rssExc, err := read(*rssFile)
	if err != nil {
		return fail(err)
	}
	findings := checkWall(m, wallExc, *wallLimit)
	rssNote := "no -rss-dir: peak RSS not checked"
	if *rssDir != "" {
		peaks, err := readRSSDir(*rssDir)
		if err != nil {
			return fail(err)
		}
		findings = append(findings, checkRSS(peaks, rssExc, *rssLimit)...)
		rssNote = fmt.Sprintf("%d test binaries probed for RSS (a package served from the test cache is not re-probed)", len(peaks))
	}
	failed := 0
	var rows []string
	for _, fd := range findings {
		fmt.Fprintln(out, fd.text)
		if fd.fail {
			failed++
		}
		if fd.row != "" {
			rows = append(rows, fd.row)
		}
	}
	fmt.Fprintf(out, "testbudget: %d tests in %d packages checked against %.0fs wall; %s\n", len(m.tests), len(m.pkgs), *wallLimit, rssNote)
	if wallExc.unseeded && failed > 0 {
		fmt.Fprintf(out, "testbudget: UNSEEDED: %s has never been populated from a whole-suite run, so these %d findings do not fail this run.\n", *wallFile, failed)
		fmt.Fprintln(out, "testbudget: seed the wall and RSS exceptions with the rows below (and their pins in internal/testutil), then delete the !unseeded line:")
		for _, r := range rows {
			fmt.Fprintln(out, "  "+strings.ReplaceAll(r, "\t", "<TAB>"))
		}
		return 0
	}
	if failed > 0 {
		fmt.Fprintf(out, "testbudget: %d violation(s). New offenders are fixed, not pinned; the inventories only shrink.\n", failed)
		return 1
	}
	return 0
}
