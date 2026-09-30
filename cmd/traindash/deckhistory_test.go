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
	// Each run contains both decks, and candidate stamps deliberately order
	// opposite the lexically-sorted short heads.
	a := writeCandidate(t, root, "bbbbbbbbbbbbbbbb", "bot-a", newer,
		mkRow("Wildfire", "bot-a", "opponent", "p0", "natural")+"\n"+
			mkRow("Burn", "opponent", "bot-a", "p0", "natural")+"\n")
	b := writeCandidate(t, root, "aaaaaaaaaaaaaaaa", "bot-b", older,
		mkRow("Wildfire", "bot-b", "opponent", "p1", "natural")+"\n"+
			mkRow("Burn", "bot-b", "opponent", "p0", "natural")+"\n")
	if a == b || len(findDeckLedgers(root)) != 2 {
		t.Fatalf("fixture precondition: candidate dirs=%q,%q ledgers=%d", a, b, len(findDeckLedgers(root)))
	}
	got := scanDeckHistory(root)
	if len(got) != 2 || got[0].Deck != "Burn" || got[1].Deck != "Wildfire" {
		t.Fatalf("decks = %+v, want both sorted decks", got)
	}
	wantOrder := []string{"aaaaaaaa/bot-b", "bbbbbbbb/bot-a"}
	for _, deck := range got {
		if len(deck.Points) != 2 {
			t.Fatalf("%s points = %+v, want 2", deck.Deck, deck.Points)
		}
		for i, p := range deck.Points {
			if p.Run != wantOrder[i] {
				t.Fatalf("%s order[%d] = %q, want %q", deck.Deck, i, p.Run, wantOrder[i])
			}
		}
	}
	if got[0].Points[0].Wins != 1 || got[0].Points[1].Wins != 0 || got[1].Points[0].Wins != 0 || got[1].Points[1].Wins != 1 {
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
	if err := os.Chtimes(filepath.Join(first, "matches.jsonl"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(second, "matches.jsonl"), newer, newer); err != nil {
		t.Fatal(err)
	}
	if !older.Before(newer) {
		t.Fatal("mtime fixture precondition: timestamps must differ")
	}
	got := scanDeckHistory(root)
	if len(got) != 1 || len(got[0].Points) != 2 {
		t.Fatalf("history = %+v, want one deck in two runs", got)
	}
	if got[0].Points[0].Run != "firsthea/one" || !got[0].Points[0].Order.Equal(older) || !got[0].Points[1].Order.Equal(newer) {
		t.Fatalf("mtime ordering = %+v", got[0].Points)
	}
	snap := NewScanner(nil).Scan()
	if snap.DeckHistory == nil || len(snap.DeckHistory) != 0 {
		t.Fatalf("empty scan deck history = %#v, want non-nil empty", snap.DeckHistory)
	}
}
