package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mkRow writes one spellbench-match-ledger/v1 row compactly. decks must be
// non-empty; seat names are p0/p1, and the seat ordinal travels with each
// seats entry exactly as cmd/botbench's writer emits it. Mirroring that
// writer, winner is null for anything that is not a natural game (a truncated
// or halted row carries no winner), so a fixture cannot claim a winner the
// format does not.
func mkRow(deck, p0, p1, winner, outcome string) string {
	return mkRowDecks(deck, deck, p0, p1, winner, outcome)
}

// mkRowDecks is mkRow for a non-mirror game: d0 is the deck in seat p0 and d1
// the deck in seat p1, matching the decks[s] seat indexing the reader relies
// on.
func mkRowDecks(d0, d1, p0, p1, winner, outcome string) string {
	w := `"` + winner + `"`
	if outcome != "natural" {
		w = `null`
	}
	return `{"schema":"spellbench-match-ledger/v1","decks":[{"catalog_id":"` + d0 +
		`"},{"catalog_id":"` + d1 + `"}],"seats":[{"name":"` + p0 +
		`","seat":"p0"},{"name":"` + p1 + `","seat":"p1"}],"winner":` +
		w + `,"outcome":"` + outcome + `"}`
}

// mkRowReordered is mkRowDecks with the seats array emitted p1 first, so a
// reader that trusts the array position instead of the seat ordinal
// mis-attributes both the deck and the win side.
func mkRowReordered(d0, d1, p0, p1, winner, outcome string) string {
	w := `"` + winner + `"`
	if outcome != "natural" {
		w = `null`
	}
	return `{"schema":"spellbench-match-ledger/v1","decks":[{"catalog_id":"` + d0 +
		`"},{"catalog_id":"` + d1 + `"}],"seats":[{"name":"` + p1 +
		`","seat":"p1"},{"name":"` + p0 + `","seat":"p0"}],"winner":` +
		w + `,"outcome":"` + outcome + `"}`
}

// TestParseDeckLedgerPerDeck is the per-deck reader's precondition and count
// assertion. The fixture is built inline (Forge scripts are GPL and never
// committed) and the test first proves the raw rows are what the parser must
// see, so a silently-empty fixture fails loudly instead of passing.
func TestParseDeckLedgerPerDeck(t *testing.T) {
	// Seven rows on two decks for sb-tactical vs bot, seat-swapped, plus one
	// draw. Preconditions: the focus policy appears in every row; the focus
	// sits in BOTH seat ordinals with a win AND a loss in each, so a reader
	// that collapses either the p0-win or the p1-win arm by winner alone
	// fails; and BOTH decks carry at least one win and one loss, so a rate
	// of 0 or 1 would mean the reader collapsed categories.
	b := []byte(
		mkRow("Wildfire", "sb-tactical", "bot", "p0", "natural") + "\n" + // focus p0 wins (p0 winner)
			mkRow("Wildfire", "bot", "sb-tactical", "p1", "natural") + "\n" + // focus p1 wins (p1 winner)
			mkRow("Wildfire", "sb-tactical", "bot", "p1", "natural") + "\n" + // focus p0 loses (p1 winner)
			mkRow("Wildfire", "bot", "sb-tactical", "p0", "natural") + "\n" + // focus p1 loses (p0 winner) -- the p0-win-in-p1 branch
			mkRow("Elves", "sb-tactical", "bot", "p0", "natural") + "\n" +
			mkRow("Elves", "bot", "sb-tactical", "p1", "truncated") + "\n" + // draw: not scored
			mkRowReordered("Spy", "Spy", "bot", "sb-tactical", "p0", "natural") + "\n") // focus p1 loses; seats listed p1 first
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
	if len(led.Rows) != 3 {
		t.Fatalf("rows = %d, want 3 (Elves, Spy, Wildfire)", len(led.Rows))
	}
	if led.Rows[0].Deck != "Elves" || led.Rows[1].Deck != "Spy" || led.Rows[2].Deck != "Wildfire" {
		t.Fatalf("rows not sorted by deck: %q, %q, %q", led.Rows[0].Deck, led.Rows[1].Deck, led.Rows[2].Deck)
	}
	// Elves: 1 win, 1 truncated (draw) -> 1-0, rate 1.0.
	el := led.Rows[0]
	if el.Wins != 1 || el.Losses != 0 || el.Draws != 1 || el.Games != 1 {
		t.Fatalf("Elves rate = %+v, want 1-0 with 1 draw", el)
	}
	if el.WinRate != 1.0 || len(el.CI) != 2 || el.CI[0] <= 0 || el.CI[1] != 1.0 {
		t.Fatalf("Elves CI = %+v, want a one-sided interval with lo>0, hi=1", el)
	}
	// Wildfire: 2 wins (one in each seat), 2 losses (one in each seat) -> 2-2.
	// Collapsing either win arm by winner alone moves this to 3-1 or 1-3.
	wfr := led.Rows[2]
	if wfr.Wins != 2 || wfr.Losses != 2 || wfr.Games != 4 || wfr.WinRate != 0.5 {
		t.Fatalf("Wildfire rate = %+v, want 2-2 at 0.5", wfr)
	}
	if wfr.CI[0] >= 0.5 || wfr.CI[1] <= 0.5 {
		t.Fatalf("Wildfire CI = %v, want a two-sided interval straddling 0.5", wfr.CI)
	}
	// Spy: one loss with the seats array listed p1-first, so a reader that
	// uses the array position instead of the seat ordinal reads a win.
	spy := led.Rows[1]
	if spy.Losses != 1 || spy.Wins != 0 {
		t.Fatalf("Spy rate = %+v, want 0-1 (p0 won against the p1 focus; order must not matter)", spy)
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
	s := NewScanner([]string{root})
	s.DeckRoots = []string{root}
	snap := s.Scan()
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

// TestScanDeckRootsAreSeparate pins the fix for the unbounded default-root
// walk: a matches.jsonl under the TRAINING root must not become a deck ledger
// unless that directory is named as a deck root. The default training root
// holds hundreds of scratch ledgers; walking them all both costs seconds on a
// cold scan and floods the page with unrelated charts.
func TestScanDeckRootsAreSeparate(t *testing.T) {
	trainingRoot := t.TempDir()
	deckRoot := t.TempDir()
	writeLedger := func(root, rel string) {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := mkRow("Wildfire", "sb-tactical", "bot", "p0", "natural") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "matches.jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeLedger(trainingRoot, "spellbench-work/w1")
	writeLedger(deckRoot, "ref/key")

	s := NewScanner([]string{trainingRoot})
	s.DeckRoots = []string{deckRoot}
	snap := s.Scan()
	if len(snap.DeckLedgers) != 1 {
		t.Fatalf("deck ledgers = %d, want 1 (only the deck root)", len(snap.DeckLedgers))
	}
	if got := snap.DeckLedgers[0].ID; got != "ref/key" {
		t.Fatalf("deck ledger id = %q, want ref/key (training-root ledger must not leak in)", got)
	}

	// With no deck root configured the panel is empty even though the
	// training root holds a matches.jsonl.
	if got := NewScanner([]string{trainingRoot}).Scan().DeckLedgers; len(got) != 0 {
		t.Fatalf("deck ledgers = %d with no deck root, want 0", len(got))
	}
}

// TestParseDeckLedgerSeatsNonMirror pins the seat/deck attribution: a
// non-mirror row's focus deck is the deck at the focus seat, and a win by the
// p0 seat against a focus policy in p1 is a LOSS, not a win.
func TestParseDeckLedgerSeatsNonMirror(t *testing.T) {
	b := []byte(
		mkRowDecks("Burn", "Elves", "bot", "sb-tactical", "p1", "natural") + "\n" + // focus p1, Elves, wins
			mkRowDecks("Burn", "Elves", "bot", "sb-tactical", "p0", "natural") + "\n") // focus p1, Elves, loses
	led := parseDeckLedger("ref/nm", "/p", b, "sb-tactical")
	if led == nil || len(led.Rows) != 1 {
		t.Fatalf("ledger = %+v, want one deck row", led)
	}
	row := led.Rows[0]
	if row.Deck != "Elves" {
		t.Fatalf("focus deck = %q, want Elves (the seat the focus policy sat in), not the first deck", row.Deck)
	}
	if row.Wins != 1 || row.Losses != 1 || row.Games != 2 || row.WinRate != 0.5 {
		t.Fatalf("focus rate = %+v, want 1-1 at 0.5 (a p0 win against p1 focus is a loss)", row)
	}
}
