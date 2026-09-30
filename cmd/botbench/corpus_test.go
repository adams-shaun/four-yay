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
// run on the subset registry spellbenchExit opens by default and on the whole
// corpus (-corpus-full), and requires byte-identical matches.jsonl and
// games.jsonl (wall_ms dropped). It also requires the default run to have
// actually used a subset, so the comparison is not full-vs-full.
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
	subM, subG, subLog := run(false)
	fullM, fullG, _ := run(true)
	if !strings.Contains(subLog, "-card subset") {
		t.Fatalf("the default FDN run did not open a subset registry:\n%s", subLog)
	}
	if subM != fullM {
		t.Error("matches.jsonl differs between the subset and the full corpus")
	}
	if subG != fullG {
		t.Errorf("games.jsonl digest: subset %s, full %s", subG, fullG)
	}
}
