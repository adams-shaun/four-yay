package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mkRow writes one spellbench-match-ledger/v1 row compactly. decks must be
// non-empty; seat names are p0/p1. Mirroring cmd/botbench's writer, winner is
// null for anything that is not a natural game (a truncated or halted row
// carries no winner), so a fixture cannot claim a winner the format does not.
func mkRow(deck, p0, p1, winner, outcome string) string {
	w := `"` + winner + `"`
	if outcome != "natural" {
		w = `null`
	}
	return `{"schema":"spellbench-match-ledger/v1","decks":[{"catalog_id":"` + deck +
		`"}],"seats":[{"name":"` + p0 + `"},{"name":"` + p1 + `"}],"winner":` +
		w + `,"outcome":"` + outcome + `"}`
}

// TestParseDeckLedgerPerDeck is the per-deck reader's precondition and count
// assertion. The fixture is built inline (Forge scripts are GPL and never
// committed) and the test first proves the raw rows are what the parser must
// see, so a silently-empty fixture fails loudly instead of passing.
func TestParseDeckLedgerPerDeck(t *testing.T) {
	// Six rows on two decks for sb-tactical vs bot, seat-swapped, plus one
	// draw. Preconditions: the focus policy appears in every row and BOTH
	// decks carry at least one win AND one loss, so a rate of 0 or 1 would
	// mean the reader collapsed categories.
	b := []byte(
		mkRow("Wildfire", "sb-tactical", "bot", "p0", "natural") + "\n" +
			mkRow("Wildfire", "bot", "sb-tactical", "p1", "natural") + "\n" +
			mkRow("Wildfire", "sb-tactical", "bot", "p1", "natural") + "\n" + // tactical loses
			mkRow("Wildfire", "bot", "sb-tactical", "p1", "natural") + "\n" + // tactical wins
			mkRow("Elves", "sb-tactical", "bot", "p0", "natural") + "\n" +
			mkRow("Elves", "bot", "sb-tactical", "p1", "truncated") + "\n") // draw: not scored
	led := parseDeckLedger("ref/test", "/does/not/matter", b, "sb-tactical")
	if led == nil {
		t.Fatal("parseDeckLedger returned nil: the reader saw no rows")
	}
	if led.Focus != "sb-tactical" {
		t.Fatalf("focus = %q, want the explicit focus", led.Focus)
	}
	// Auto-detect is a separate assertion: with the counts tied, the tie must
	// break lexically ("bot" < "sb-tactical") and be stable.
	if auto := parseDeckLedger("ref/test", "/p", b, ""); auto == nil || auto.Focus != "bot" {
		t.Fatalf("auto focus = %+v, want stable lexical tie-break \"bot\"", auto)
	}
	if len(led.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (Wildfire, Elves)", len(led.Rows))
	}
	if led.Rows[0].Deck != "Elves" || led.Rows[1].Deck != "Wildfire" {
		t.Fatalf("rows not sorted by deck: %q, %q", led.Rows[0].Deck, led.Rows[1].Deck)
	}
	// Elves: 1 win, 1 truncated (draw) -> 1-0, rate 1.0.
	el := led.Rows[0]
	if el.Wins != 1 || el.Losses != 0 || el.Draws != 1 || el.Games != 1 {
		t.Fatalf("Elves rate = %+v, want 1-0 with 1 draw", el)
	}
	if el.WinRate != 1.0 || len(el.CI) != 2 || el.CI[0] <= 0 || el.CI[1] != 1.0 {
		t.Fatalf("Elves CI = %+v, want a one-sided interval with lo>0, hi=1", el)
	}
	// Wildfire: 3 wins, 1 loss -> 3-1, rate 0.75.
	wfr := led.Rows[1]
	if wfr.Wins != 3 || wfr.Losses != 1 || wfr.Games != 4 || wfr.WinRate != 0.75 {
		t.Fatalf("Wildfire rate = %+v, want 3-1 at 0.75", wfr)
	}
	if wfr.CI[0] >= 0.75 || wfr.CI[1] <= 0.75 {
		t.Fatalf("Wildfire CI = %v, want a two-sided interval straddling 0.75", wfr.CI)
	}
}

// TestParseDeckLedgerSkipsGarbage proves a mid-write line is skipped and a
// ledger with no focus policy for the named focus returns nil (so the caller
// never plots a phantom deck).
func TestParseDeckLedgerSkipsGarbage(t *testing.T) {
	b := []byte(`{"schema":"other/v9"}` + "\n" +
		`{"schema":"spellbench-match-ledger/v1",` + "\n" + // truncated write
		mkRow("Spy", "sb-uniform", "sb-heuristic", "p0", "natural") + "\n")
	led := parseDeckLedger("ref/x", "/p", b, "sb-uniform")
	if led == nil || len(led.Rows) != 1 || led.Rows[0].Deck != "Spy" {
		t.Fatalf("garbage tolerance = %+v", led)
	}
	if got := led.Rows[0].Wins; got != 1 {
		t.Fatalf("Spy wins = %d, want 1 (the malformed line must be dropped, not counted)", got)
	}
	// A focus absent from the ledger yields no rows and nil.
	if led := parseDeckLedger("ref/x", "/p", b, "sb-tactical"); led != nil {
		t.Fatalf("absent focus returned %+v, want nil", led)
	}
}

// TestWilson95Known pins the interval against a hand-computed value so the
// dashboard's CI is the same Wilson form the rating uses.
func TestWilson95Known(t *testing.T) {
	lo, hi := wilson95(5, 10)
	// Symmetric at p=0.5: [0.2366, 0.7634].
	if lo < 0.2365 || lo > 0.2367 || hi < 0.7633 || hi > 0.7635 {
		t.Fatalf("wilson95(5,10) = [%f,%f], want ~[0.2366,0.7634]", lo, hi)
	}
	if lo, hi := wilson95(0, 0); lo != 0 || hi != 1 {
		t.Fatalf("wilson95(0,0) = [%v,%v], want [0,1]", lo, hi)
	}
}

// TestFindDeckLedgers discovers matches.jsonl under a root and skips the
// directories the scan already skips (scratch/).
func TestFindDeckLedgers(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"ref/aaa", "ref/bbb", "scratch"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ref/aaa/matches.jsonl", mkRow("Wildfire", "bot", "bot", "p0", "natural")+"\n")
	write("ref/bbb/matches.jsonl", mkRow("Elves", "bot", "bot", "p0", "natural")+"\n")
	write("scratch/matches.jsonl", mkRow("Spy", "bot", "bot", "p0", "natural")+"\n")
	got := findDeckLedgers(root)
	var ids []string
	for _, f := range got {
		ids = append(ids, f.id)
	}
	if len(ids) != 2 || ids[0] != "ref/aaa" || ids[1] != "ref/bbb" {
		t.Fatalf("findDeckLedgers = %v, want [ref/aaa ref/bbb] (scratch/ must be skipped)", ids)
	}
}

// TestScanIncludesDeckLedgers proves the wiring: a scan of a root holding a
// matches.jsonl surfaces its per-deck ledger in the snapshot payload the page
// polls.
func TestScanIncludesDeckLedgers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ref", "key")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := mkRow("Wildfire", "sb-tactical", "bot", "p0", "natural") + "\n" +
		mkRow("Wildfire", "bot", "sb-tactical", "p0", "natural") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "matches.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := NewScanner([]string{root}).Scan()
	if len(snap.DeckLedgers) != 1 {
		t.Fatalf("deck ledgers = %d, want 1", len(snap.DeckLedgers))
	}
	dl := snap.DeckLedgers[0]
	if dl.ID != "ref/key" || dl.Focus == "" || len(dl.Rows) != 1 {
		t.Fatalf("ledger = %+v", dl)
	}
	if dl.Rows[0].WinRate != 0.5 {
		t.Fatalf("reader precondition: want both a win and a loss, got %+v", dl.Rows[0])
	}
}
