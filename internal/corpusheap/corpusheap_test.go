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
	// Lowered at S4 (the imaged lazy registry): measured 1.4 MB scan,
	// 40.5 MB heap, 18,405 objects. S4 set the heap ceiling to 35.6 MB from
	// the design's ESTIMATE ("~35-40 MB") rather than from the measurement
	// it logged, so it landed just above the ceiling; cmd/botbench failed
	// first in the same batch run and masked it.
	//
	// Re-pinned 2026-10-07 (agent-20261007T001003Z-312ebafa) when the census
	// was made cache-independent. The census now builds its OWN gob cache in
	// t.TempDir() (compile -> Save -> LoadRegistry) instead of calling
	// cards.OpenCorpus on the shared .cards, so it measures the SAME imaged
	// registry whether .cards already holds an ir-<fingerprint>.gob.gz or is
	// read-only. That deterministic path measures 35.5 MB heap here.
	//
	// Why 35.5 and not the old cache-hit 35.6: the imaged registry embeds the
	// card-script path string on every card, so a compile run from an ABSOLUTE
	// root carries that root's length. Measured 2026-10-07 (byte-identical
	// corpora, same 34074 cards): this worktree's ~95-char .cards prefix -> 40.5
	// MB, the main checkout's ~33-char prefix -> 35.6 MB, a short /tmp copy ->
	// 35.5 MB. The old census therefore varied with WHERE the checkout lived,
	// not with the code. The census now t.Chdir()s into the corpus dir and
	// compiles the RELATIVE root "cardsfolder", so the stored paths carry a
	// fixed short prefix and the measurement is identical from .cards or any
	// copy: measured 37.19 MB (35.5 MB) on six fresh processes, two roots.
	// (CompileDir's gob BYTES still vary slightly run to run, but the decoded
	// heap is identical to <0.01 MB, which is what this ratchet reads.)
	//
	// The compile is OUTSIDE the measured window: it builds an eager *Card tree
	// that is dropped before the baseline, so the delta is the imaged registry
	// alone, exactly what a server that hits a cache holds.
	ceilingScanBytes   int64 = 1_500_000  // 1.4 MB: /gc/scan/heap:bytes
	ceilingHeapBytes   int64 = 39_200_000 // 35.5 MB measured + ~5% headroom: HeapAlloc
	ceilingHeapObjects int64 = 18_400     // /gc/heap/objects:objects
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

// TestCorpusHeapCensus pins the live heap, scannable bytes and object count
// of the imaged lazy corpus registry against the ceilings. It builds its own
// gob cache from the corpus in a private temp dir and measures what
// LoadRegistry serves from it, so the census is deterministic -- independent
// of which ir-<fingerprint>.gob.gz files .cards happens to hold, of .cards
// being writable, and of where the checkout lives. It skips when there is no
// .cards/ corpus, the same way internal/testutil.CorpusRegistry does, so a
// clean clone with no fetched corpus still passes.
func TestCorpusHeapCensus(t *testing.T) {
	dir := corpusDir(t)
	if dir == "" {
		t.Skip("corpusheap: no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}

	// Measure ONE deterministic path: the imaged lazy registry that
	// LoadRegistry serves from a gob cache. Build that cache OURSELVES in a
	// private temp dir -- compile, Save, then load -- so the result never
	// depends on whether .cards already holds an ir-<fingerprint>.gob.gz or
	// on .cards being writable. Calling cards.OpenCorpus(shared .cards) here
	// was the defect: it served the shared cache when one existed and an
	// embedded, eager compiled registry when the Save back into a read-only
	// .cards failed, and the two measured 37 MB vs 165 MB. The census measures
	// the registry itself, so it cannot use internal/testutil.CorpusRegistry's
	// shared cache either (that would reintroduce the same dependence).
	//
	// Compile from a RELATIVE root (t.Chdir into the corpus dir, then
	// CompileDir("cardsfolder")): the imaged registry stores each card's
	// script path, so an absolute root embeds the checkout's path length and
	// the measurement would vary with where the worktree lives (measured
	// 40.5 MB from this worktree's ~95-char root vs 35.5 MB from a short
	// path). The relative root makes the stored prefix fixed and short, so the
	// number is the corpus's, not the box's.
	//
	// The compile is deliberately OUTSIDE the measured window: it builds an
	// eager *Card tree that is dropped (Save writes the gob; the tree is no
	// longer referenced) before the baseline is taken, so the delta below is
	// the imaged registry alone, exactly what a cache hit holds. The tempdir
	// is removed by t.TempDir; nothing is written into .cards.
	cache := filepath.Join(t.TempDir(), "ir.gob.gz")
	t.Chdir(dir)
	compiled, _, err := cards.CompileDir("cardsfolder")
	if err != nil {
		t.Fatalf("corpusheap: CompileDir(cardsfolder): %v", err)
	}
	if compiled.Len() == 0 {
		t.Fatal("corpusheap: CompileDir returned an empty registry")
	}
	if err := compiled.Save(cache); err != nil {
		t.Fatalf("corpusheap: Save(%s): %v", cache, err)
	}
	compiled = nil

	// A pre-load baseline is subtracted from the post-load reading: the delta
	// is the corpus itself, not whatever this test binary already held. The
	// baseline also proves the setup is honest -- an empty heap must be tiny,
	// or the "after" figure measured against it means nothing.
	before := report()

	reg, err := cards.LoadRegistry(cache)
	if err != nil {
		t.Fatalf("corpusheap: LoadRegistry(%s): %v", cache, err)
	}
	if reg == nil {
		t.Fatal("corpusheap: LoadRegistry returned a nil registry")
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
	if reg.Len() == 0 {
		t.Fatalf("corpusheap: populated registry reports 0 cards -- the load did not happen")
	}
	// The measured registry must be the IMAGED (lazy) one, not the eager
	// compiled tree: an imaged registry materializes a card only when it is
	// touched, so at this point none are. An eager registry reports every card
	// materialized, and that is exactly the state the old OpenCorpus-on-a-
	// read-only-.cards path wrongly measured.
	if n := reg.MaterializedCount(); n != 0 {
		t.Fatalf("corpusheap: MaterializedCount = %d, want 0 -- measured an eager registry, not the imaged one", n)
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
