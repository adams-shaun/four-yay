package main

// Ported from the 2026-09-26 auto-pay audit's throwaway proof branch
// (`wt/autopay-gaps-proofs` @ 124ed89fb, file payment_plan_audit_test.go) as
// ticket ap-feedback-copy / PP-15's feedback lane: a match whose log carries
// planned payment intents is captured by the real SnapshotForFeedback path and
// replays through the real loader to the recorded head; tampering one saved
// witness diverges.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// TestPaymentPlanAuditFeedbackCaptureReplaysPlannedIntents replays a planned
// intent capture to the recorded head and refuses a tampered witness.
func TestPaymentPlanAuditFeedbackCaptureReplaysPlannedIntents(t *testing.T) {
	requireCorpus(t)
	root, err := feedback.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	reg := testutil.CorpusRegistry(t)
	load := func(name string) (host.Deck, error) {
		cards, err := testutil.LoadRepoDeck(reg, name)
		if err != nil {
			return host.Deck{}, host.ErrNotFound
		}
		return host.Deck{Name: name, Cards: cards}, nil
	}
	r, err := host.New(host.Options{
		LoadDeck: load, Tokens: reg.Tokens, NameUniverse: reg.AllCards(),
		Seats: func(_ []string, seed uint64) []seat.Seat {
			return []seat.Seat{seat.NewBot(seed ^ 1).EnableAutoPayMana(), seat.NewBot(seed ^ 2).EnableAutoPayMana()}
		},
		Sleep: func(time.Duration, <-chan struct{}) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(host.TableConfig{ID: "t1", Name: "t1", Seats: 2, Decks: []string{"ur-delver", "uw-control"},
		Seed: 11, Spectator: view.Public, AutoMana: true, BotAutoPayMana: true}); err != nil {
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
	planned := 0
	for _, in := range snap.Log.Log.Intents {
		if in.Payment != nil {
			planned++
		}
	}
	if planned == 0 {
		t.Fatal("precondition: the captured match recorded no planned intent")
	}
	write := func(dir string, match, log any) {
		for name, v := range map[string]any{"match.json": match, "log.json": log} {
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	dir := t.TempDir()
	write(dir, snap.Match, snap.Log)
	l, cfg, meta, err := feedback.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := replay.Replay(l, cfg)
	if err != nil {
		t.Fatalf("replay of a planned-intent capture: %v", err)
	}
	if e.L.Head() != meta.Head {
		t.Fatalf("replayed head %s, recorded %s", e.L.Head(), meta.Head)
	}
	t.Logf("capture with %d planned intents replayed to %s", planned, meta.Head)

	// Tamper one saved witness (its body, not its ID): the replay must refuse.
	tampered := snap.Log
	tampered.Log.Intents = append(tampered.Log.Intents[:0:0], snap.Log.Log.Intents...)
	for i := range tampered.Log.Intents {
		if tampered.Log.Intents[i].Payment != nil {
			in := tampered.Log.Intents[i]
			p := *in.Payment
			p.Plan.PoolAfter[5]++
			in.Payment = &p
			tampered.Log.Intents[i] = in
			break
		}
	}
	bad := t.TempDir()
	write(bad, snap.Match, tampered)
	l2, cfg2, _, err := feedback.Load(bad)
	if err != nil {
		t.Fatalf("load tampered: %v", err)
	}
	if e2, err := replay.Replay(l2, cfg2); err == nil && e2.L.Head() == meta.Head {
		t.Fatal("a tampered saved witness replayed to the recorded head")
	}
}
