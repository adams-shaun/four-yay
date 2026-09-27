package host

import (
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// paymentPlanIntent is deliberately a View/Decision-only external-seat
// policy.  It knows no rules internals: it plays a land when it can, chooses
// an offered payment witness when one exists, and otherwise makes the same
// legacy first-Min answer used by the ordinary HumanSeat integration tests.
// That makes this the host boundary exercised by an embedder rather than a
// shortcut around Decision.Validate or Engine.Submit.
func paymentPlanIntent(d *decision.Decision) (decision.Intent, bool) {
	if d.Kind == decision.KPriority {
		if len(d.PaymentActions) != 0 && len(d.PaymentActions[0].Plans) != 0 {
			a := d.PaymentActions[0]
			return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{
				ActionID: a.ID,
				Plan:     a.Plans[0],
			}}, true
		}
		for _, o := range d.Options {
			if o.Kind == "play_land" {
				return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}, false
			}
		}
	}
	return legalIntent(d), false
}

// runPaymentPlanHumanSeat is PP-20's external-seat lane. Seat zero sees only
// the decision published by the host and submits two offered witnesses through
// Registry.SubmitIntent; it then answers an ordinary priority decision with
// legacy Choices. The sample decks provide a basic land and fixed single-pip
// spells, so the selected witness is an actual untapped-source payment, not a
// pool-only or synthetic offer.
func runPaymentPlanHumanSeat(t *testing.T, seats int, decks []string, seed uint64) {
	var human *HumanSeat
	o := testOptions(t)
	o.Seats = humanFirstSeat(&human)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{
		ID: "t1", Name: "payment-plan-human", Seats: seats, Decks: decks,
		Seed: seed, Pace: 0, Spectator: view.Omniscient, AutoMana: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	var last uint64 = ^uint64(0)
	selected := 0
	legacyAfterPlan := false
	for {
		if time.Now().After(deadline) {
			t.Fatalf("payment-plan host flow did not reach planned then legacy answers; selected=%d legacy=%t", selected, legacyAfterPlan)
		}
		matches, err := r.Matches("t1")
		if err != nil {
			t.Fatalf("Matches: %v", err)
		}
		if len(matches) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		if len(matches) != 1 {
			t.Fatalf("matches = %+v, want one live match", matches)
		}
		if matches[0].State == protocol.MatchCrashed {
			t.Fatalf("payment-plan host game crashed: %+v", matches[0])
		}
		if matches[0].State == protocol.MatchFinished {
			break
		}
		d, err := r.Pending("t1", 1, 0)
		if err != nil || d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		in, planned := paymentPlanIntent(d)
		if selected >= 2 && d.Kind == decision.KPriority {
			in, planned = legalIntent(d), false
			legacyAfterPlan = true
		}
		if err := r.SubmitIntent("t1", 1, state.PlayerID(0), in); err != nil {
			t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
		}
		if planned {
			selected++
		}
		last = d.Seq
	}
	if selected < 2 {
		t.Fatalf("external human seat selected %d payment actions, want two", selected)
	}
	if got := human.caretakerCount(); got != 0 {
		t.Fatalf("caretaker answered %d decisions", got)
	}

	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.RLock()
	fm := tb.cur
	if fm == nil && len(tb.history) != 0 {
		fm = tb.history[len(tb.history)-1]
	}
	tb.mu.RUnlock()
	if fm == nil {
		t.Fatal("completed match was not retained")
	}
	fm.mu.RLock()
	log, cfg := fm.e.L.Clone(), fm.cfg
	fm.mu.RUnlock()
	replayed, err := replay.Replay(log, cfg)
	if err != nil {
		t.Fatalf("replay selected-plan host game: %v", err)
	}
	if got, want := replayed.L.Head(), log.Head(); got != want {
		t.Fatalf("replay head = %s, want %s", got, want)
	}
	t.Logf("selected two host payment plans then legacy priority; completed %d events; head %s", len(log.Events), log.Head())
}

func TestPaymentPlanHumanSeatSelectsAnOfferedPlanAndReplays(t *testing.T) {
	t.Run("two_seats", func(t *testing.T) {
		runPaymentPlanHumanSeat(t, 2, []string{"a", "b"}, 20260924)
	})
	t.Run("four_seats", func(t *testing.T) {
		runPaymentPlanHumanSeat(t, 4, []string{"a", "b", "c", "d"}, 20260925)
	})
}

// TestSubmitIntentPreflightsPaymentPlanBeforeAccepting proves the host does
// more than validate the wire selector.  The injected witness is internally
// consistent with the parked Decision (including its Seq-bound ID), but is
// deliberately unable to settle the spell's mana cost.  It must be rejected
// while the seat remains parked, rather than acknowledged and later handed to
// Engine.Submit where a host match would crash.
func TestSubmitIntentPreflightsPaymentPlanBeforeAccepting(t *testing.T) {
	var human *HumanSeat
	o := testOptions(t)
	o.Seats = humanFirstSeat(&human)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{ID: "t1", Name: "payment-plan-preflight", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260924, Spectator: view.Omniscient, AutoMana: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(30 * time.Second)
	last := ^uint64(0)
	for {
		if time.Now().After(deadline) {
			t.Fatal("did not reach a payment plan with a mana activation")
		}
		d, err := r.Pending("t1", 1, 0)
		if err != nil {
			time.Sleep(time.Millisecond)
			continue
		}
		if d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		if d.Kind == decision.KPriority {
			for ai := range d.PaymentActions {
				a := d.PaymentActions[ai]
				if len(a.Plans) == 0 || len(a.Plans[0].Activations) == 0 {
					continue
				}
				bad := decision.ClonePaymentPlan(a.Plans[0])
				bad.Activations = nil // valid V1 shape, but cannot pay this offered spell.
				bad.ID, err = decision.PaymentPlanID(d.Seq, d.Player, a.Cast, bad)
				if err != nil {
					t.Fatal(err)
				}

				// Model an inconsistent published payment offer. A client cannot
				// manufacture this (Decision.Validate checks exact membership),
				// but the host must still reject it before acknowledging it if a
				// future planner or restore path ever does.
				human.mu.Lock()
				if human.slot == nil || human.slot.dec.Seq != d.Seq {
					human.mu.Unlock()
					t.Fatal("payment decision disappeared before fixture injection")
				}
				human.slot.dec.PaymentActions[ai].Plans[0] = bad
				human.mu.Unlock()

				in := decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: bad}}
				if err := r.SubmitIntent("t1", 1, 0, in); err == nil {
					t.Fatal("SubmitIntent accepted a payment witness the engine rejects")
				}
				again, err := r.Pending("t1", 1, 0)
				if err != nil || again.Seq != d.Seq {
					t.Fatalf("rejected payment unparked or moved the game: pending=%+v err=%v", again, err)
				}
				return
			}
		}
		in, _ := paymentPlanIntent(d)
		if err := r.SubmitIntent("t1", 1, 0, in); err != nil {
			t.Fatalf("advance to payment offer seq %d: %v", d.Seq, err)
		}
		last = d.Seq
	}
}

// TestAutoManaDisabledKeepsHumanPriorityOnTheLegacyWire proves the table
// capability is enforced at the host boundary. The engine may retain its
// replay extension for bots, but a human client on an off table receives only
// ordinary options and can continue through the pre-payment-plan path.
func TestAutoManaDisabledKeepsHumanPriorityOnTheLegacyWire(t *testing.T) {
	var human *HumanSeat
	o := testOptions(t)
	o.Seats = humanFirstSeat(&human)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{ID: "t1", Name: "manual", Seats: 2, Decks: []string{"a", "b"}, Seed: 20260924, Spectator: view.Omniscient}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var last uint64 = ^uint64(0)
	answered := 0
	for answered < 12 {
		if time.Now().After(deadline) {
			t.Fatalf("only answered %d decisions", answered)
		}
		d, err := r.Pending("t1", 1, 0)
		if err != nil || d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		if len(d.PaymentActions) != 0 {
			t.Fatalf("disabled table published payment actions: %#v", d.PaymentActions)
		}
		if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
			t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
		}
		last = d.Seq
		answered++
	}
}
