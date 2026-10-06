package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// event is the subset of a `go test -json` record the budget reads.
type event struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package,omitempty"`
	Test    string  `json:"Test,omitempty"`
	Output  string  `json:"Output,omitempty"`
	Elapsed float64 `json:"Elapsed,omitempty"`
}

// testWall is one top-level test's measured wall time.
type testWall struct {
	pkg, name string  // pkg is the module-relative directory
	wall      float64 // seconds
	sub       string  // the heaviest subtest, "" when none ran
	subWall   float64
}

func (t testWall) key() string { return t.pkg + "\t" + t.name }

// measured is everything a run's events say.
type measured struct {
	tests map[string]*testWall // key() -> test
	pkgs  map[string]bool      // package directories that reported a result
}

// pkgDir turns an import path into a module-relative directory.
func pkgDir(module, importPath string) string {
	if importPath == module {
		return "."
	}
	return strings.TrimPrefix(importPath, module+"/")
}

// parseEvents reads `go test -json` records (or tee's compact form). A line
// that is not JSON is skipped; skipped tests are not measured.
func parseEvents(r io.Reader, module string) (*measured, error) {
	m := &measured{tests: map[string]*testWall{}, pkgs: map[string]bool{}}
	skipped := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Package == "" {
			continue
		}
		if ev.Action != "pass" && ev.Action != "fail" && ev.Action != "skip" {
			continue
		}
		dir := pkgDir(module, ev.Package)
		if ev.Test == "" {
			m.pkgs[dir] = true
			continue
		}
		top, sub, isSub := strings.Cut(ev.Test, "/")
		k := dir + "\t" + top
		t := m.tests[k]
		if t == nil {
			t = &testWall{pkg: dir, name: top}
			m.tests[k] = t
		}
		switch {
		case ev.Action == "skip" && !isSub:
			skipped[k] = true
		case isSub:
			if ev.Elapsed > t.subWall {
				t.sub, t.subWall = sub, ev.Elapsed
			}
		case ev.Elapsed > t.wall:
			t.wall = ev.Elapsed
		}
	}
	for k := range skipped {
		delete(m.tests, k)
	}
	return m, sc.Err()
}

// noise is plain-text output a passing run does not print without -v.
var noise = regexp.MustCompile(`^\s*(=== (RUN|PAUSE|CONT|NAME)\b|--- (PASS|SKIP)\b|PASS\s*$)`)

// runTee reprints the plain `go test` text from a -json stream on out and
// writes the compact per-test events to the -events file. A passing test's
// output is dropped (plain `go test` drops it too); a failing test's, and any
// output of a test with no terminal event (a panic or timeout kills the
// binary mid-test), is printed. Package-level output (build errors, the
// "FAIL\tpkg" and "ok  \tpkg" lines) is always printed.
func runTee(args []string, in io.Reader, out io.Writer) int {
	var eventsPath string
	for i := 0; i < len(args); i++ {
		if args[i] == "-events" && i+1 < len(args) {
			eventsPath = args[i+1]
			i++
		}
	}
	if eventsPath == "" {
		fmt.Fprintln(os.Stderr, "testbudget tee: -events FILE is required")
		return 2
	}
	f, err := os.Create(eventsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testbudget tee: %v\n", err)
		return 2
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	pending := map[string][]string{} // pkg\ttest -> output lines awaiting its verdict
	var order []string               // first-seen order, so a flush is deterministic
	flush := func(k string) {
		for _, l := range pending[k] {
			fmt.Fprint(out, l)
		}
		delete(pending, k)
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			fmt.Fprintln(out, sc.Text())
			continue
		}
		k := ev.Package + "\t" + ev.Test
		switch ev.Action {
		case "output":
			switch {
			case ev.Test == "":
				if !noise.MatchString(ev.Output) {
					fmt.Fprint(out, ev.Output)
				}
			case !noise.MatchString(ev.Output):
				if _, ok := pending[k]; !ok {
					order = append(order, k)
				}
				pending[k] = append(pending[k], ev.Output)
			}
		case "pass", "fail", "skip":
			if ev.Test != "" && ev.Action == "fail" {
				flush(k)
			}
			delete(pending, k)
			if ev.Test == "" && ev.Action == "fail" {
				for _, o := range order {
					if strings.HasPrefix(o, ev.Package+"\t") {
						flush(o)
					}
				}
			}
			if ev.Elapsed > 0 || ev.Test == "" {
				_ = enc.Encode(event{Action: ev.Action, Package: ev.Package, Test: ev.Test, Elapsed: ev.Elapsed})
			}
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "testbudget tee: %v\n", err)
		return 2
	}
	return 0
}
