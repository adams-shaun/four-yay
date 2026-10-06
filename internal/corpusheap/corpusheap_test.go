// Package corpusheap is a test-only package that ratchets the resident card
// corpus' heap footprint. It lives in its own package, rather than joining an
// existing one, so the census runs in a fresh process with nothing else loaded
// and in parallel with every other package's tests.
//
// The point is the pointer-free corpus work (S0 of
// docs/superpowers/specs/2026-10-06-pointer-free-corpus-design.md): the whole
// corpus is 163 MB live, 102 MB of it scannable, and every GC cycle re-marks
// it. This test pins the measured values so a regression that grows the heap,
// or a later step that shrinks it, cannot pass unnoticed: a measurement above
// its ceiling fails, and one far below its ceiling fails too as a stale
// ceiling that no longer ratchets.
package corpusheap_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"

	// Register the effects' ParamCoders exactly as a real process does, so the
	// census reflects what a server holds, not a bare parser.
	_ "github.com/adams-shaun/gorge/rules"
)

// The ceilings, in absolute bytes/objects. They are the values the spec's §1.1
// probe measured on main at 7b37cc0eb (102.2 MB scan, 1,685,473 objects,
// 163.1 MB heap) rounded to a stable value; the census logs the live numbers so
// a later step can lower them.
const (
	ceilingScanBytes   int64 = 107_164_467 // 102.2 MB: /gc/scan/heap:bytes
	ceilingHeapBytes   int64 = 170_917_888 // 163.0 MB: HeapAlloc
	ceilingHeapObjects int64 = 1_685_000   // /gc/heap/objects:objects
)

// tolerance is the noise band: a measurement within this fraction of its
// ceiling (either side) passes. Anything above it is a regression; anything
// more than staleFraction below it is a ceiling that must be lowered.
const (
	tolerance     = 0.03 // ±3%
	staleFraction = 0.10 // >10% below the ceiling is stale
)

// heapSample is one census reading.
type heapSample struct {
	scan    uint64
	objects uint64
	heap    uint64
}

// report forces two GCs and reads the post-load heap. Two GCs, not one: the
// first collection may leave the previous cycle's floating garbage, and the
// second settles the mark set the ceiling is about.
func report() heapSample {
	runtime.GC()
	runtime.GC()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	samples := []metrics.Sample{
		{Name: "/gc/scan/heap:bytes"},
		{Name: "/gc/heap/objects:objects"},
	}
	metrics.Read(samples)

	return heapSample{
		scan:    samples[0].Value.Uint64(),
		objects: samples[1].Value.Uint64(),
		heap:    ms.HeapAlloc,
	}
}

// TestCorpusHeapCensus opens the corpus and pins its live heap, scannable
// bytes and object count against the ceilings. It skips when there is no
// .cards/ corpus, the same way internal/testutil.CorpusRegistry does, so a
// clean clone with no fetched corpus still passes.
func TestCorpusHeapCensus(t *testing.T) {
	dir := corpusDir(t)
	if dir == "" {
		t.Skip("corpusheap: no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}

	// A pre-load baseline is subtracted from the post-load reading: the delta
	// is the corpus itself, not whatever this test binary already held. The
	// baseline also proves the setup is honest -- an empty heap must be tiny,
	// or the "after" figure measured against it means nothing.
	before := report()

	reg, err := cards.OpenCorpus(dir)
	if err != nil {
		t.Fatalf("corpusheap: OpenCorpus(%s): %v", dir, err)
	}
	if reg == nil {
		t.Fatal("corpusheap: OpenCorpus returned a nil registry")
	}
	// Keep the registry reachable through the measurement; a liveness barrier,
	// not just documentation.
	runtime.KeepAlive(reg)

	after := report()

	scan := int64(after.scan) - int64(before.scan)
	objects := int64(after.objects) - int64(before.objects)
	heap := int64(after.heap) - int64(before.heap)

	// The preconditions the real assertions depend on: the baseline is an
	// empty heap (not already carrying a corpus), and the load actually
	// changed all three figures. Without these a "nothing loaded" run would
	// pass every ceiling trivially.
	if before.heap > 64<<20 {
		t.Fatalf("corpusheap: pre-load baseline HeapAlloc = %s, expected an empty heap", mb(before.heap))
	}
	if len(reg.Cards) == 0 {
		t.Fatalf("corpusheap: populated registry reports 0 cards -- the load did not happen")
	}
	for name, v := range map[string]int64{
		"scan": scan, "objects": objects, "heap": heap,
	} {
		if v <= 0 {
			t.Fatalf("corpusheap: %s delta = %d, expected the load to grow it", name, v)
		}
	}

	// Log the live numbers the ceilings must be lowered to after any change
	// that legitimately shrinks the corpus. This line is the authority.
	t.Logf("corpus heap: scan=%s heap=%s objects=%d", mb(uint64(scan)), mb(uint64(heap)), objects)
	t.Logf("ceilings:    scan=%s heap=%s objects=%d", mb(uint64(ceilingScanBytes)), mb(uint64(ceilingHeapBytes)), ceilingHeapObjects)

	check(t, "scan", "/gc/scan/heap:bytes", scan, ceilingScanBytes)
	check(t, "heap", "HeapAlloc", heap, ceilingHeapBytes)
	check(t, "objects", "/gc/heap/objects:objects", objects, ceilingHeapObjects)
}

// check applies the bidirectional ratchet to one measurement.
func check(t *testing.T, label, metric string, got int64, ceiling int64) {
	t.Helper()
	gotF := float64(got)
	ceilF := float64(ceiling)
	switch {
	case gotF > ceilF*(1+tolerance):
		t.Errorf("%s = %.1f (%s) is above its ceiling %s by more than %.0f%% -- the corpus grew; if the growth is intended, raise the ceiling in this file, otherwise fix the regression",
			metric, gotF, mb(uint64(got)), mb(uint64(ceiling)), tolerance*100)
	case gotF < ceilF*(1-staleFraction):
		t.Errorf("%s = %.1f (%s) is more than %.0f%% below its ceiling %s -- stale ceiling, lower it to the measured value",
			metric, gotF, mb(uint64(got)), staleFraction*100, mb(uint64(ceiling)))
	}
	_ = label
}

// corpusDir finds the repository root the way internal/testutil/decks.go:211
// and cards/boundary_test.go do -- `git rev-parse --show-toplevel`, not a
// hard-coded relative path. A test binary's working directory is its own
// package directory, and under a hook GIT_DIR can point somewhere wrong, so the
// child git runs with cards.GitEnv() (every inherited GIT_* stripped), exactly
// as that discovery does. It returns "" when there is no .cards/ directory.
func corpusDir(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), ".cards")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

// mb renders a byte count as a human-readable MB figure.
func mb(b uint64) string {
	return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
}
