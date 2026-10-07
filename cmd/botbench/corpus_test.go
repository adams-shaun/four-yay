package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpellbenchSubsetCorpusMatchesFull plays the same small FDN spellbench
// run on the corpus spellbenchExit opens by default and on the whole corpus
// (-corpus-full), and requires byte-identical matches.jsonl and games.jsonl
// (wall_ms dropped).
//
// The corpus open this compares used to be OpenCorpusFor's subset loader, and
// the test also required the default run to have actually taken the subset so
// it was not a full-vs-full comparison. S4 (perf(cards), a3b5123a3) made
// OpenCorpusFor a wrapper over SharedCorpus: the imaged, lazy registry costs
// the GC what a subset did, so the subset is no longer on this path and the
// default open IS the full one. The subset loader itself is unchanged and is
// still driven directly by cards/subset_test.go; S8 deletes it.
//
// The equivalence the test exists for still holds and is still worth pinning:
// -corpus-full must not change a game. It now asserts the default open is the
// full registry (the property that made the old subset requirement obsolete)
// rather than a subset, so a future return of a subset open to this path
// re-arms the subset comparison loudly instead of silently comparing
// full-vs-full.
func TestSpellbenchSubsetCorpusMatchesFull(t *testing.T) {
	dir := corpusDirForSpellbench(t)
	run := func(full bool) (string, string, string) {
		t.Helper()
		prev := *corpusFull
		*corpusFull = full
		defer func() { *corpusFull = prev }()
		o := sbOpts{bots: "bot,sb-heuristic", pairs: 1, decks: "FDN01-UBG,FDN06-RG",
			out: t.TempDir(), baseSeed: 20260926, catalog: "fdn"}
		var errOut bytes.Buffer
		if code := spellbenchExit(o, dir, 2, 40, 8000, "", io.Discard, &errOut); code != 0 {
			t.Fatalf("spellbenchExit(full=%v) = %d: %s", full, code, errOut.String())
		}
		m, err := os.ReadFile(filepath.Join(o.out, "matches.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		return string(m), digestGamesJSON(t, filepath.Join(o.out, "games.jsonl")), errOut.String()
	}
	defM, defG, defLog := run(false)
	fullM, fullG, _ := run(true)
	// S4: the default open is the shared full registry, so the two runs are
	// the same registry. The subset log line would mean OpenCorpusFor took the
	// subset route again, and then this must compare subset against full.
	if !strings.Contains(defLog, "corpus: full,") {
		t.Fatalf("the default FDN run did not open the full registry:\n%s", defLog)
	}
	if defM != fullM {
		t.Error("matches.jsonl differs between the default and the full corpus")
	}
	if defG != fullG {
		t.Errorf("games.jsonl digest: default %s, full %s", defG, fullG)
	}
}
