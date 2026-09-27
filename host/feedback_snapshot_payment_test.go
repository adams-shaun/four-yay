package host

// The feedback snapshot's payment-extension boundary (spec §7, ap-feedback-copy):
// SnapshotForFeedback copies the engine's pending decision for the reporting
// seat's view. That copy must (a) deep-copy the payment-plan extension —
// PaymentActions and the pending PaymentFallback — so nothing written through
// the snapshot can reach the live pending decision, and (b) gate the extension
// exactly the way ViewAtSeat gates it: a table without AutoMana publishes no
// payment actions to the seat, so the seat's feedback view.json must not carry
// payment_actions the live client never received.

import (
	"reflect"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// capturePlannedPending drives a two-seat table whose seat 0 is a human, until
// a pending priority decision carries a non-empty payment-plan extension, and
// captures SnapshotForFeedback for seat 0 while that decision is still parked.
// The human never answers, so the captured pending decision stays live behind
// the snapshot. It fails the test when no planned decision ever parks (the
// fixture precondition), on a crash, or when the match finishes first.
func capturePlannedPending(t *testing.T, autoMana bool) (FeedbackSnapshot, *Registry) {
	t.Helper()
	var human *HumanSeat
	o := testOptions(t)
	o.Seats = humanFirstSeat(&human)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(TableConfig{ID: "t1", Name: "feedback-payment", Seats: 2, Decks: []string{"a", "b"},
		Seed: 20260924, Pace: 0, Spectator: view.Omniscient, AutoMana: autoMana}); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		r.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	var last uint64 = ^uint64(0)
	for {
		if time.Now().After(deadline) {
			r.Close()
			t.Fatal("no pending priority decision carried a payment plan (fixture precondition)")
		}
		matches, err := r.Matches("t1")
		if err != nil {
			r.Close()
			t.Fatalf("Matches: %v", err)
		}
		if len(matches) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		if matches[0].State == protocol.MatchCrashed {
			r.Close()
			t.Fatalf("fixture game crashed: %+v", matches[0])
		}
		if matches[0].State == protocol.MatchFinished {
			r.Close()
			t.Fatal("match finished before a planned priority decision parked (fixture precondition)")
		}
		d, err := r.Pending("t1", 1, 0)
		if err != nil || d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		// The extension is read on the engine's own pending decision: on an
		// AutoMana-off table the published copy is stripped (match.go), so a
		// planned priority would otherwise be invisible to this loop exactly
		// in the configuration the gating test must observe. Seat 0 is parked
		// whenever Pending returned, so the engine decision below is the same
		// parked one, not a bot's transient ask.
		m := liveMatch(t, r, "t1")
		m.mu.RLock()
		ep := m.e.Pending()
		planned := ep != nil && ep.Kind == decision.KPriority && ep.Seq == d.Seq &&
			len(ep.PaymentActions) > 0 && len(ep.PaymentActions[0].Plans) > 0
		m.mu.RUnlock()
		if planned {
			seat := state.PlayerID(0)
			snap, err := r.SnapshotForFeedback("t1", &seat)
			if err != nil {
				r.Close()
				t.Fatalf("SnapshotForFeedback: %v", err)
			}
			return snap, r
		}
		// Not a planned priority: answer with the ordinary legacy first-Min
		// options so the game keeps moving toward one. A payment witness is
		// never submitted — the snapshot is captured while the planned
		// decision is still parked.
		if err := r.SubmitIntent("t1", 1, state.PlayerID(0), legalIntent(d)); err != nil {
			r.Close()
			t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
		}
		last = d.Seq
	}
}

// TestFeedbackSnapshotPaymentGatedByAutoMana: a seat on a table with AutoMana
// off sees no payment actions in the live seat view (the pending publication
// strips them), so the seat's feedback snapshot view must not carry them
// either. The fixture helper itself proves the table produces planned
// decisions at all — the engine's parked decision carries the extension
// regardless of the table setting, so an ungated snapshot would publish it.
func TestFeedbackSnapshotPaymentGatedByAutoMana(t *testing.T) {
	snap, r := capturePlannedPending(t, false)
	defer r.Close()
	if snap.View == nil {
		t.Fatal("feedback snapshot for the reporting seat carried no view")
	}
	if snap.View.Decision == nil {
		t.Fatal("feedback snapshot view did not attach the pending decision")
	}
	if n := len(snap.View.Decision.PaymentActions); n != 0 {
		t.Fatalf("AutoMana-off feedback view carries %d payment actions; the live seat view publishes none", n)
	}
}

// TestFeedbackSnapshotPaymentActionsAreDeepCopies: on a table with AutoMana
// on, the snapshot's view carries the payment extension (the AutoMana-on
// control the gating test's absence would otherwise be), and mutating it —
// including the PaymentFallback pointer, the field a bare `cp := *p` copy
// aliases outright — leaves the engine's live pending decision untouched.
func TestFeedbackSnapshotPaymentActionsAreDeepCopies(t *testing.T) {
	snap, r := capturePlannedPending(t, true)
	defer r.Close()
	if snap.View == nil || snap.View.Decision == nil {
		t.Fatal("feedback snapshot view did not attach the pending decision")
	}
	if len(snap.View.Decision.PaymentActions) == 0 {
		t.Fatal("precondition: the AutoMana-on feedback view carries no payment actions")
	}

	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	p := m.e.Pending()
	if p == nil {
		m.mu.RUnlock()
		t.Fatal("live pending decision vanished before the snapshot's copy could be checked")
	}
	before := p.CloneValue()
	want := m.table.cfg.AutoMana
	m.mu.RUnlock()
	if !want {
		t.Fatal("precondition: fixture table built with AutoMana off")
	}

	// Mutate every part of the extension through the snapshot's copy: the
	// action slice, its plan slice, and the fallback pointer the shallow
	// struct copy would share with the engine's pending decision.
	vd := snap.View.Decision
	vd.PaymentActions[0].Label = "mutated by test"
	vd.PaymentActions[0].Plans[0].ID = "mutated by test"
	if vd.PaymentFallback != nil {
		vd.PaymentFallback.Reason = "mutated by test"
	}

	m.mu.RLock()
	p = m.e.Pending()
	if p == nil {
		m.mu.RUnlock()
		t.Fatal("live pending decision vanished after the snapshot mutation")
	}
	live := p.CloneValue()
	m.mu.RUnlock()
	if !reflect.DeepEqual(before.PaymentActions, live.PaymentActions) {
		t.Fatalf("mutation through the snapshot reached the live pending decision:\n got %#v\nwant %#v",
			live.PaymentActions, before.PaymentActions)
	}
	if vd.PaymentFallback != nil && !reflect.DeepEqual(before.PaymentFallback, live.PaymentFallback) {
		t.Fatalf("fallback mutation through the snapshot reached the live pending decision:\n got %#v\nwant %#v",
			live.PaymentFallback, before.PaymentFallback)
	}
}
