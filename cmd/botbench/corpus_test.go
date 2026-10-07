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
// run with the default corpus handling and with -corpus-full, and requires
// byte-identical matches.jsonl and games.jsonl (wall_ms dropped).
//
// Until S4 of the pointer-free corpus design this compared a subset registry
// against the whole corpus, because the default run decoded only its decks'
// cards plus tokens. S4 made cards.OpenCorpusFor a wrapper over SharedCorpus
// (the imaged, lazy registry), so both paths now open the same shared full
// registry and the run-level subset assertion is retired. The invariant that
// survives -- a deck-seated run's games do not depend on how its registry was
// opened -- is still worth pinning: the default must be the shared full
// registry, and -corpus-full must not change a single byte.
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
	if !strings.Contains(defLog, "corpus: full") {
		t.Fatalf("the default FDN run did not open the shared full registry (S4: the subset loader is retired):\n%s", defLog)
	}
	if strings.Contains(defLog, "-card subset") {
		t.Errorf("the default FDN run opened a subset registry, which S4 retired:\n%s", defLog)
	}
	if defM != fullM {
		t.Error("matches.jsonl differs between the default and -corpus-full runs")
	}
	if defG != fullG {
		t.Errorf("games.jsonl digest: default %s, -corpus-full %s", defG, fullG)
	}
}
