package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchprobe"
)

func TestRejectInvalidBudgetsBeforeLoadingCorpus(t *testing.T) {
	for _, args := range [][]string{{"-games", "0"}, {"-workers", "0"}, {"-worlds", "3"}, {"-attempts", "0"}, {"-max-submits", "0"}} {
		output := filepath.Join(t.TempDir(), "must-not-exist.json")
		args = append(args, "-out", output, "-cards", "missing-corpus")
		if err := run(args, io.Discard); err == nil || !strings.Contains(err.Error(), "require games/workers/attempts") {
			t.Fatalf("did not reject invalid budget before loading corpus: %v: %v", args, err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("invalid budget created output: %v", err)
		}
	}
}

func TestSummarizeResultsSeparatesEligibleRootsAndNoRootGames(t *testing.T) {
	results := []searchprobe.ExperimentResult{
		{RootAt: 10, FourWorld: searchprobe.SearchResult{Replays: 4}},
		{RootAt: 20, FourWorld: searchprobe.SearchResult{Fallback: "insufficient sampled worlds/ESS"}},
		{RootAt: -1, NoRootReason: "no eligible turn>=5 cast/ability/pass root"},
		{RootAt: -1, Error: "baseline stalled"},
	}
	got := summarizeResults(results)
	if got.Covered != 1 || got.Errors != 1 || got.EligibleRoots != 2 || got.NoRootGames != 1 {
		t.Fatalf("summary = %+v", got)
	}
}

// TestRunWritesReadableProfiles catches either profiling flag being ignored,
// written in a format go tool pprof cannot consume, or contaminating the JSON
// report with profiler configuration instead of the ordinary run result.
func TestRunWritesReadableProfiles(t *testing.T) {
	// The corpus need only be fetched; searchprobe loads this build's own
	// fingerprint-keyed IR via cards.OpenCorpus (compiling if necessary).
	if _, err := os.Stat("../../.cards/cardsfolder"); err != nil {
		t.Skipf("compiled corpus unavailable: %v", err)
	}
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "report.json")
	cpuPath := filepath.Join(tmp, "cpu.pprof")
	heapPath := filepath.Join(tmp, "heap.pprof")
	err := run([]string{
		"-games", "1", "-workers", "1", "-seed", "10307",
		"-attempts", "1", "-worlds", "4", "-max-submits", "5000",
		"-cards", "../../.cards", "-out", outPath,
		"-cpuprofile", cpuPath, "-memprofile", heapPath,
	}, io.Discard)
	if err != nil {
		t.Fatalf("profiled run: %v", err)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open report: %v", err)
	}
	defer f.Close()
	var report struct {
		Games int
	}
	if err := json.NewDecoder(f).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Games != 1 {
		t.Fatalf("report games = %d, want 1", report.Games)
	}

	for _, tc := range []struct{ name, path string }{
		{"cpuprofile", cpuPath},
		{"memprofile", heapPath},
	} {
		pf, err := os.Open(tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		zr, err := gzip.NewReader(pf)
		if err != nil {
			pf.Close()
			t.Fatalf("%s: not a gzip pprof stream: %v", tc.name, err)
		}
		n, copyErr := io.Copy(io.Discard, zr)
		closeErr := zr.Close()
		fileCloseErr := pf.Close()
		if copyErr != nil || closeErr != nil || fileCloseErr != nil || n == 0 {
			t.Fatalf("%s: unreadable or empty profile: bytes=%d copy=%v gzip-close=%v file-close=%v", tc.name, n, copyErr, closeErr, fileCloseErr)
		}
	}
}
