package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/view"
)

func TestPaymentPlanUndoRepostsAndReacceptsAPlannedIntent(t *testing.T) {
	o := testOptions(t)
	rewound := make(chan int, 1)
	o.OnRewind = func(_ TableID, _ int, n int, _ uint64) error { rewound <- n; return nil }
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{ID: "t1", Name: "undo-plan", Seats: 2, Decks: []string{"a", "b"},
		Seed: 20260924, Pace: 0, Spectator: view.Omniscient, Humans: []int{0}, AutoMana: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	var planned *decision.Decision
	var sel decision.Intent
	last := ^uint64(0)
	for planned == nil {
		if time.Now().After(deadline) {
			t.Fatal("no planned action with an activation was offered")
		}
		d, err := r.Pending("t1", 1, 0)
		if err != nil || d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		in, _ := paymentPlanIntent(d)
		if in.Payment != nil && len(in.Payment.Plan.Activations) == 0 {
			in = legalIntent(d)
		}
		if err := r.SubmitIntent("t1", 1, 0, in); err != nil {
			t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
		}
		if in.Payment != nil {
			planned, sel = d, in
		}
		last = d.Seq
	}
	// Wait until the human is asked something after the planned intent.
	for {
		d, err := r.Pending("t1", 1, 0)
		if err == nil && d.Seq != planned.Seq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("game did not advance past the planned intent")
		}
		time.Sleep(time.Millisecond)
	}
	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	n := waitRewind(t, rewound)
	d := waitPendingSeq(t, r, "t1", planned.Seq)
	if !reflect.DeepEqual(d.PaymentActions, planned.PaymentActions) {
		t.Fatalf("rewound offer differs from the original:\n got %#v\nwant %#v", d.PaymentActions, planned.PaymentActions)
	}
	if err := r.SubmitIntent("t1", 1, 0, sel); err != nil {
		t.Fatalf("re-submitting the identical planned witness after undo: %v", err)
	}
	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	l, cfg := m.e.L.Clone(), m.cfg
	m.mu.RUnlock()
	if _, err := replay.ReplayTo(l, cfg, n); err != nil {
		t.Fatalf("ReplayTo(%d) after planned undo: %v", n, err)
	}
}

func TestPaymentPlanRestartReplaysPersistedPlannedIntents(t *testing.T) {
	dir := t.TempDir()
	cfg := TableConfig{ID: "t1", Name: "restart-plan", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260924,
		Spectator: view.Omniscient, AutoMana: true, BotAutoPayMana: true}
	{
		r, err := New(diskOptions(t, dir))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.AddTable(cfg); err != nil {
			t.Fatal(err)
		}
		_ = r.Start("t1")
		r.Wait("t1")
		r.Close()
	}
	raw, err := os.ReadFile(filepath.Join(dir, "t1", "1.intents"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"payment"`) {
		t.Fatal("precondition: the persisted match recorded no planned intent")
	}
	r, err := New(diskOptions(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := r.Matches("t1")
	if err != nil || len(ms) != 1 {
		t.Fatalf("matches after restart: %+v %v", ms, err)
	}
	v, err := r.ViewAt("t1", 1, uint64(ms[0].Events-1))
	if err != nil || !v.Over {
		t.Fatalf("archived planned match did not replay after restart: over=%v err=%v", v.Over, err)
	}
	r.Close()

	// Tamper with the first persisted witness (not its ID) and restart.
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	tampered := false
	for i, line := range lines {
		if !strings.Contains(line, `"payment"`) {
			continue
		}
		var in decision.Intent
		if err := json.Unmarshal([]byte(line), &in); err != nil || in.Payment == nil {
			continue
		}
		in.Payment.Plan.PoolAfter[5]++
		b, _ := json.Marshal(in)
		lines[i] = string(b)
		tampered = true
		break
	}
	if !tampered {
		t.Skip("intents file line format not a bare Intent; tamper probe not applicable")
	}
	if err := os.WriteFile(filepath.Join(dir, "t1", "1.intents"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r2, err := New(diskOptions(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if v, err := r2.ViewAt("t1", 1, uint64(ms[0].Events-1)); err == nil && v.Over {
		t.Fatal("a tampered persisted witness was replayed and served as the recorded game")
	}
}
