package main

// Task cli-20260922T150843Z-42f8f6dc, fix round sol3 MAJOR: the name-card
// universe is a match MODE, and a feedback snapshot has to carry it the same
// way host's own sidecar does. `gorged` wires Options.NameUniverse
// unconditionally, so EVERY live match is universe-backed and a report filed
// against a match that resolved a NameCard ask records a name intent. If
// match.json does not record the mode, feedback.config() rebuilds a Config
// without a universe, the replayed engine takes the legacy no-ask path, and
// replay.run finds no decision pending at the recorded intent: cmd/repro and
// feedback.EngineAt both fail on exactly the reports that most need
// reproducing.
//
// This is the end-to-end proof for that path — a real universe-backed host
// match, captured by the real capture code (SnapshotForFeedback), replayed
// through the real loader — and it covers both committed shapes: the live
// capture, which pins the exact offered label list, and the size-stripped
// committed fixture, which keeps only the mode bit and re-derives the list
// from the live corpus.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// nameUniverseDeckCards builds the deck the table below plays: Pithing
// Needle, whose ETBReplacement resolves `DB$ NameCard` with no ValidCards$
// (the brief's headline unrestricted card), beside a basic land to cast it
// with. Both are REAL corpus cards by design — feedback.config() rebuilds
// each seat's deck by looking its recorded card names up in the corpus, so a
// synthetic script could not survive the round trip this test is about.
// Needle costs {1}, so seat 0 lands one on turn 2 and the match reaches a
// real ETB name ask well inside its first few turns.
func nameUniverseDeckCards(t *testing.T, reg *cards.Registry) []*cards.Card {
	t.Helper()
	needle, ok := reg.Lookup("Pithing Needle")
	if !ok {
		t.Skip("corpus has no Pithing Needle")
	}
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Skip("corpus has no Forest")
	}
	deck := make([]*cards.Card, 0, 40)
	for i := 0; i < 16; i++ {
		deck = append(deck, needle)
	}
	for i := 0; i < 24; i++ {
		deck = append(deck, forest)
	}
	return deck
}

// nameChoices counts the name elections a log recorded: the Choose events
// rules/cast.go emits with Counter "name" for an ETB name ask. It is the
// precondition every assertion below rests on — with none, a replay that
// lost the universe would succeed by accident and the test would prove
// nothing.
func nameChoices(l *events.Log) int {
	n := 0
	for _, ev := range l.Events {
		if ev.Kind == events.Choose && ev.Counter == "name" {
			n++
		}
	}
	return n
}

// TestFeedbackSnapshotReplaysAUniverseBackedMatch plays a universe-backed
// match to its end, captures it exactly as the server's feedback capture
// does, and replays the capture through feedback.Load — the path cmd/repro
// and feedback.EngineAt both take. The recorded head must come back.
//
// Reverting the fix (dropping the `if m.NameUniverse` restore in
// internal/testutil/feedback/feedback.go's config, or the two fields in
// host.FeedbackMatch / feedbackMatch) makes the replayed engine pose no name
// ask, and replay refuses the recorded intent.
func TestFeedbackSnapshotReplaysAUniverseBackedMatch(t *testing.T) {
	// The capture reads each token card's script back off a path relative to
	// the repo root, as every live server runs; the fixture generator in this
	// package chdirs for the same reason.
	root, err := feedback.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	reg := testutil.CorpusRegistry(t)
	if reg.Len() == 0 {
		t.Fatal("precondition: corpus universe is empty")
	}
	deck := nameUniverseDeckCards(t, reg)
	load := func(name string) (host.Deck, error) {
		if name != "needles" {
			return host.Deck{}, host.ErrNotFound
		}
		return host.Deck{Name: "needles", Cards: deck}, nil
	}
	r, err := host.New(host.Options{
		LoadDeck:     load,
		Tokens:       reg.Tokens,
		NameUniverse: reg.Universe(),
		Seats: func(seatNames []string, seed uint64) []seat.Seat {
			return []seat.Seat{seat.NewBot(seed ^ 1), seat.NewBot(seed ^ 2)}
		},
		Sleep: func(time.Duration, <-chan struct{}) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(host.TableConfig{ID: "t1", Name: "Table t1", Seats: 2,
		Decks: []string{"needles", "needles"}, Seed: 7, Spectator: view.Public}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	snap, err := r.SnapshotForFeedback("t1", nil)
	if err != nil {
		t.Fatalf("SnapshotForFeedback: %v", err)
	}
	if !snap.Match.NameUniverse {
		t.Fatal("capture lost the name-universe mode: a universe-backed match must record name_universe true")
	}
	if len(snap.Match.NameUniverseNames) < 1000 {
		t.Fatalf("capture recorded only %d universe names; the pinned label list is missing", len(snap.Match.NameUniverseNames))
	}
	if n := nameChoices(&snap.Log.Log); n == 0 {
		t.Fatal("precondition: the captured match resolved no name election, so this replay proves nothing")
	}

	dir := t.TempDir()
	matchRaw, err := json.MarshalIndent(snap.Match, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	logRaw, err := json.MarshalIndent(snap.Log, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"match.json": matchRaw, "log.json": logRaw} {
		if err := os.WriteFile(filepath.Join(dir, name), append(body, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The live-capture shape: the pinned label list is present, so the
	// replay is immune to a corpus move as well.
	l, cfg, meta, err := feedback.Load(dir)
	if err != nil {
		t.Fatalf("load universe-backed capture: %v", err)
	}
	if cfg.NameUniverse == nil {
		t.Fatal("feedback.config dropped the universe the capture recorded")
	}
	if len(cfg.NameUniverseNames) != len(snap.Match.NameUniverseNames) {
		t.Fatalf("restored %d pinned names, capture recorded %d", len(cfg.NameUniverseNames), len(snap.Match.NameUniverseNames))
	}
	e, err := replay.Replay(l, cfg)
	if err != nil {
		t.Fatalf("replay universe-backed capture: %v", err)
	}
	if got := e.L.Head(); got != meta.Head {
		t.Fatalf("replayed head %q, recorded %q", got, meta.Head)
	}

	// The committed-fixture shape: writeCommittedMatch strips the half-megabyte
	// label list (and the GPL token text) but keeps the mode bit, so the
	// loader re-derives the list from the live corpus. Against the corpus the
	// capture was taken on — this one — that derivation is the same list, so
	// the head must still come back. A corpus pin move is what can break it,
	// and that surfaces as the documented DIVERGED.
	stripped := t.TempDir()
	const syncID = "nameuniverse-roundtrip"
	// writeCommittedMatch syncs the stripped token text into the gitignored
	// token directory keyed by this id, as it does for a real fixture. This
	// is a throwaway id, so remove the directory again rather than leaving a
	// stale one beside the committed fixture's.
	if syncDir, err := feedback.TokenSyncDir(syncID); err == nil {
		t.Cleanup(func() { os.RemoveAll(syncDir) })
	}
	committed, err := writeCommittedMatch(matchRaw, syncID)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(committed, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["name_universe_names"]; ok {
		t.Error("committed match.json still carries the full name_universe_names list")
	}
	if _, ok := doc["name_universe"]; !ok {
		t.Fatal("committed match.json dropped the name_universe MODE bit; the fixture can no longer replay")
	}
	if err := os.WriteFile(filepath.Join(stripped, "match.json"), committed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stripped, "log.json"), append(logRaw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	l2, cfg2, meta2, err := feedback.Load(stripped)
	if err != nil {
		t.Fatalf("load stripped capture: %v", err)
	}
	if cfg2.NameUniverse == nil {
		t.Fatal("stripped capture replayed without a universe; the mode bit is not load-bearing")
	}
	e2, err := replay.Replay(l2, cfg2)
	if err != nil {
		t.Fatalf("replay stripped capture: %v", err)
	}
	if got := e2.L.Head(); got != meta2.Head {
		t.Fatalf("stripped capture replayed head %q, recorded %q", got, meta2.Head)
	}
}
