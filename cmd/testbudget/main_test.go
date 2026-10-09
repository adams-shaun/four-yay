package main

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModule = "example.com/gorge"

// events is a go test -json stream: a heavy test with a heavy subtest, a fast
// one, a skipped one and a package line, plus a non-JSON line.
const events = `not json at all
{"Action":"pass","Package":"example.com/gorge/rules","Test":"TestHeavy/chunkA","Elapsed":70.5}
{"Action":"pass","Package":"example.com/gorge/rules","Test":"TestHeavy/chunkB","Elapsed":3}
{"Action":"pass","Package":"example.com/gorge/rules","Test":"TestHeavy","Elapsed":75}
{"Action":"pass","Package":"example.com/gorge/rules","Test":"TestFast","Elapsed":0.5}
{"Action":"skip","Package":"example.com/gorge/rules","Test":"TestSkipped","Elapsed":99}
{"Action":"pass","Package":"example.com/gorge/rules","Elapsed":80}
`

func parse(t *testing.T, s string) *measured {
	t.Helper()
	m, err := parseEvents(strings.NewReader(s), testModule)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseEventsMeasuresTopLevelTestsOnly(t *testing.T) {
	m := parse(t, events)
	heavy := m.tests["rules\tTestHeavy"]
	if heavy == nil {
		t.Fatalf("TestHeavy not measured: %v", m.tests)
	}
	if heavy.wall != 75 || heavy.sub != "chunkA" || heavy.subWall != 70.5 {
		t.Errorf("TestHeavy = %+v, want wall 75 with heaviest subtest chunkA 70.5", *heavy)
	}
	if _, ok := m.tests["rules\tTestSkipped"]; ok {
		t.Error("a skipped test was measured; its Elapsed is not a run")
	}
	if len(m.tests) != 2 || !m.pkgs["rules"] {
		t.Errorf("tests=%d pkgs=%v, want 2 tests and package rules seen", len(m.tests), m.pkgs)
	}
}

func TestCheckWallNamesOffendersAndKeepsInventoryHonest(t *testing.T) {
	m := parse(t, events)
	if m.tests["rules\tTestHeavy"].wall <= 60 {
		t.Fatal("precondition: TestHeavy must exceed the limit")
	}
	// Unlisted offender: a failing finding with a paste-ready row.
	fs := checkWall(m, exceptions{rows: map[string]int64{}}, 60)
	if len(fs) != 1 || !fs[0].fail || !strings.Contains(fs[0].text, "OVER-BUDGET wall   rules TestHeavy  75.0s") ||
		!strings.Contains(fs[0].text, "heaviest subtest chunkA 70.5s") || fs[0].row != "rules\tTestHeavy\t75" {
		t.Fatalf("unlisted offender: %+v", fs)
	}
	// Listed and not worse: reported but not a failure.
	fs = checkWall(m, exceptions{rows: map[string]int64{"rules\tTestHeavy": 75}}, 60)
	if len(fs) != 1 || fs[0].fail {
		t.Fatalf("listed offender must not fail: %+v", fs)
	}
	// Listed but regressed far past its pin.
	fs = checkWall(m, exceptions{rows: map[string]int64{"rules\tTestHeavy": 50}}, 60)
	if len(fs) != 1 || !fs[0].fail || !strings.Contains(fs[0].text, "REGRESSED") {
		t.Fatalf("regression must fail: %+v", fs)
	}
	// Listed but now fast: the row is stale and must be deleted.
	fs = checkWall(m, exceptions{rows: map[string]int64{"rules\tTestFast": 70}}, 60)
	if len(fs) != 2 || !strings.Contains(fs[0].text, "STALE-ROW") {
		t.Fatalf("stale pin must fail: %+v", fs)
	}
}

func TestCheckRSS(t *testing.T) {
	over := int64(defaultRSSLimitKiB + 1000)
	exc := exceptions{rows: map[string]int64{"host\tTestA": over}}
	fs := checkRSS(map[string]int64{"rules": over, "host": over, "view": 100}, exc, defaultRSSLimitKiB)
	if len(fs) != 2 || fs[0].text[:12] != "known-over  " || !fs[1].fail || !strings.Contains(fs[1].text, "OVER-BUDGET rss    rules") {
		t.Fatalf("unexpected findings: %+v", fs)
	}
	fs = checkRSS(map[string]int64{"host": 1000}, exc, defaultRSSLimitKiB)
	if len(fs) != 1 || !fs[0].fail || !strings.Contains(fs[0].text, "STALE-ROW") {
		t.Fatalf("a package well under budget must flag its exception rows stale: %+v", fs)
	}
}

func setup(t *testing.T, eventsBody, wallExc string) (args []string, rssDir string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rssDir = filepath.Join(dir, "rss")
	if err := os.MkdirAll(rssDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args = []string{
		"-events", write("events.json", eventsBody),
		"-gomod", write("go.mod", "module "+testModule+"\n"),
		"-wall-exceptions", write("wall.txt", wallExc),
		"-rss-exceptions", write("rss.txt", ""),
		"-rss-dir", rssDir,
	}
	return args, rssDir
}

func TestRunCheckExitCodes(t *testing.T) {
	var out, errb bytes.Buffer
	args, rssDir := setup(t, events, "# seeded\n")
	if code := runCheck(args, &out, &errb); code != 1 || !strings.Contains(out.String(), "OVER-BUDGET wall   rules TestHeavy") {
		t.Fatalf("seeded, over budget: code %d\n%s%s", code, out.String(), errb.String())
	}
	// An RSS-only offender fails too.
	if err := os.WriteFile(filepath.Join(rssDir, url.PathEscape("view")), []byte("5000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	args2, rss2 := setup(t, strings.ReplaceAll(events, "75", "5"), "")
	if err := os.WriteFile(filepath.Join(rss2, "view"), []byte("5000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runCheck(args2, &out, &errb); code != 1 || !strings.Contains(out.String(), "OVER-BUDGET rss    view") {
		t.Fatalf("rss over budget: code %d\n%s%s", code, out.String(), errb.String())
	}
	// Unseeded: same offender is named, with a paste-ready row, exit 0.
	out.Reset()
	args3, _ := setup(t, events, "!unseeded\n")
	if code := runCheck(args3, &out, &errb); code != 0 || !strings.Contains(out.String(), "UNSEEDED") ||
		!strings.Contains(out.String(), "rules<TAB>TestHeavy<TAB>75") {
		t.Fatalf("unseeded: code %d\n%s", code, out.String())
	}
	// A run with no test results proves nothing.
	args4, _ := setup(t, "garbage\n", "")
	if code := runCheck(args4, &out, &errb); code != 1 {
		t.Fatalf("empty events: code %d, want 1", code)
	}
}

func TestTeeReprintsPlainOutputAndWritesEvents(t *testing.T) {
	stream := `{"Action":"run","Package":"p/a","Test":"TestOK"}
{"Action":"output","Package":"p/a","Test":"TestOK","Output":"=== RUN   TestOK\n"}
{"Action":"output","Package":"p/a","Test":"TestOK","Output":"quiet log\n"}
{"Action":"pass","Package":"p/a","Test":"TestOK","Elapsed":1.5}
{"Action":"output","Package":"p/a","Test":"TestBad","Output":"bad.go:1: boom\n"}
{"Action":"output","Package":"p/a","Test":"TestBad","Output":"--- FAIL: TestBad (0.01s)\n"}
{"Action":"fail","Package":"p/a","Test":"TestBad","Elapsed":0.01}
{"Action":"output","Package":"p/a","Output":"FAIL\n"}
{"Action":"output","Package":"p/a","Output":"FAIL\tp/a\t0.5s\n"}
{"Action":"fail","Package":"p/a","Elapsed":0.5}
{"Action":"output","Package":"p/b","Test":"TestPanics","Output":"panic: runtime error\n"}
{"Action":"output","Package":"p/b","Output":"FAIL\tp/b\t0.2s\n"}
{"Action":"fail","Package":"p/b","Elapsed":0.2}
{"Action":"output","Package":"p/c","Output":"PASS\n"}
{"Action":"output","Package":"p/c","Output":"ok  \tp/c\t0.1s\n"}
{"Action":"pass","Package":"p/c","Elapsed":0.1}
`
	path := filepath.Join(t.TempDir(), "ev.json")
	var out bytes.Buffer
	if code := runTee([]string{"-events", path}, strings.NewReader(stream), &out); code != 0 {
		t.Fatalf("tee exit %d", code)
	}
	got := out.String()
	for _, want := range []string{"--- FAIL: TestBad (0.01s)\n", "bad.go:1: boom\n", "FAIL\tp/a\t0.5s\n", "panic: runtime error\n", "ok  \tp/c\t0.1s\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("tee dropped %q; got:\n%s", want, got)
		}
	}
	for _, drop := range []string{"quiet log", "=== RUN", "PASS\n"} {
		if strings.Contains(got, drop) {
			t.Errorf("tee printed passing-run noise %q; got:\n%s", drop, got)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseEvents(bytes.NewReader(b), "p")
	if err != nil {
		t.Fatal(err)
	}
	if w := m.tests["a\tTestOK"]; w == nil || w.wall != 1.5 || !m.pkgs["c"] {
		t.Errorf("events file lost the measurement: %+v pkgs=%v\n%s", w, m.pkgs, b)
	}
}

func TestExecRecordsPeakRSSAndPassesExitCode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(rssDirEnv, dir)
	if code := runExec([]string{"/bin/sh", "-c", "exit 3"}); code != 3 {
		t.Fatalf("exit code %d, want the binary's 3", code)
	}
	peaks, err := readRSSDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// go test runs this test with the package directory as cwd.
	if kib := peaks["cmd/testbudget"]; kib <= 0 {
		t.Fatalf("no peak RSS recorded for cmd/testbudget: %v", peaks)
	}
}
