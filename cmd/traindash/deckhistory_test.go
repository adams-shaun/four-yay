package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCandidate(t *testing.T, root, head, spec, stamp, body string) string {
	t.Helper()
	dir := filepath.Join(root, "cand", head, spec)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "matches.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if stamp != "" {
		meta, err := json.Marshal(candidateMeta{TS: stamp, GitHead: head, Spec: spec, Key: "refkey"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDeckHistoryOrdersCandidateRunsAndGroupsDecks(t *testing.T) {
	root := t.TempDir()
	newer := "2026-09-30T02:00:00Z"
	older := "2026-09-29T02:00:00Z"
	// Each run contains both decks. The stamp order MUST be the reverse of the
	// lexical (short-head) order, so a regression to the pre-fix plain lexical
	// sort of findDeckLedgers' ids fails this test rather than passing it. The
	// lexically-first head (aaaaaaaa) therefore carries the NEWER stamp.
	a := writeCandidate(t, root, "bbbbbbbbbbbbbbbb", "bot-a", older,
		mkRow("Wildfire", "bot-a", "opponent", "p0", "natural")+"\n"+
			mkRow("Burn", "opponent", "bot-a", "p0", "natural")+"\n")
	b := writeCandidate(t, root, "aaaaaaaaaaaaaaaa", "bot-b", newer,
		mkRow("Wildfire", "bot-b", "opponent", "p1", "natural")+"\n"+
			mkRow("Burn", "bot-b", "opponent", "p0", "natural")+"\n")
	// Precondition: the fixture can actually discriminate stamp ordering from
	// lexical ordering. Assert the two orders are exact reverses, so a later
	// fixture edit cannot silently re-align them and make the test vacuous.
	lexical := []string{"aaaaaaaa/bot-b", "bbbbbbbb/bot-a"}
	byStamp := []string{"bbbbbbbb/bot-a", "aaaaaaaa/bot-b"}
	for i := range lexical {
		if lexical[i] == byStamp[i] {
			t.Fatalf("fixture precondition: stamp order %v must not equal lexical %v", byStamp, lexical)
		}
	}
	if !(older < newer) {
		t.Fatal("fixture precondition: stamps must differ and be ordered")
	}
	if a == b || len(findDeckLedgers(root)) != 2 {
		t.Fatalf("fixture precondition: candidate dirs=%q,%q ledgers=%d", a, b, len(findDeckLedgers(root)))
	}
	got := scanDeckHistory(root)
	if len(got) != 2 || got[0].Deck != "Burn" || got[1].Deck != "Wildfire" {
		t.Fatalf("decks = %+v, want both sorted decks", got)
	}
	for _, deck := range got {
		if len(deck.Points) != 2 {
			t.Fatalf("%s points = %+v, want 2", deck.Deck, deck.Points)
		}
		for i, p := range deck.Points {
			if p.Run != byStamp[i] {
				t.Fatalf("%s order[%d] = %q, want %q", deck.Deck, i, p.Run, byStamp[i])
			}
		}
	}
	// Rates survive the reduction and distinguish the runs: order[0] is the
	// older bbbbbbbb/bot-a run (Burn 0-1, Wildfire 1-0) and order[1] the newer
	// aaaaaaaa/bot-b run (Burn 1-0, Wildfire 0-1).
	if got[0].Points[0].Wins != 0 || got[0].Points[1].Wins != 1 || got[1].Points[0].Wins != 1 || got[1].Points[1].Wins != 0 {
		t.Fatalf("rates did not preserve ledger outcomes: %+v", got)
	}
	s := NewScanner(nil)
	s.DeckRoots = []string{root}
	snap := s.Scan()
	if len(snap.DeckHistory) != 2 || len(snap.DeckHistory[0].Points) != 2 {
		t.Fatalf("scan snapshot history = %+v, want two decks with two runs", snap.DeckHistory)
	}
}

func TestDeckHistoryFallsBackToMtimeAndEmptySnapshotNormalizes(t *testing.T) {
	root := t.TempDir()
	first := writeCandidate(t, root, "firsthead", "one", "", mkRow("Wildfire", "bot", "opp", "p0", "natural")+"\n")
	second := writeCandidate(t, root, "secondhead", "two", "", mkRow("Wildfire", "bot", "opp", "p1", "natural")+"\n")
	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	// The stamp-less runs fall back to mtime. As in the stamp test, the
	// lexically-first head (firsthead) MUST carry the NEWER mtime so ascending
	// mtime order is the reverse of lexical order and a lexical-sort regression
	// fails here too.
	if err := os.Chtimes(filepath.Join(second, "matches.jsonl"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(first, "matches.jsonl"), newer, newer); err != nil {
		t.Fatal(err)
	}
	if !older.Before(newer) {
		t.Fatal("mtime fixture precondition: timestamps must differ")
	}
	got := scanDeckHistory(root)
	if len(got) != 1 || len(got[0].Points) != 2 {
		t.Fatalf("history = %+v, want one deck in two runs", got)
	}
	if got[0].Points[0].Run != "secondhe/two" || !got[0].Points[0].Order.Equal(older) || got[0].Points[1].Run != "firsthea/one" || !got[0].Points[1].Order.Equal(newer) {
		t.Fatalf("mtime ordering = %+v", got[0].Points)
	}
	snap := NewScanner(nil).Scan()
	if snap.DeckHistory == nil || len(snap.DeckHistory) != 0 {
		t.Fatalf("empty scan deck history = %#v, want non-nil empty", snap.DeckHistory)
	}
}
