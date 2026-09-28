package host

import (
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// liveEngineCardZone reads one object's zone from the live match under its
// lock, for the external-seat lane's post-window checks.
func liveEngineCardZone(t *testing.T, r *Registry, id state.ObjID) state.Zone {
	t.Helper()
	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.RLock()
	m := tb.cur
	tb.mu.RUnlock()
	if m == nil {
		t.Fatal("live match disappeared")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	o := m.e.G.Obj(id)
	if o == nil {
		t.Fatalf("object %d vanished", id)
	}
	return o.Zone
}

// TestAnnouncePayHumanSeatThroughHost is the announce-then-pay host lane
// (docs/superpowers/specs/2026-09-27-announce-then-pay.md): a human seat on
// an Auto Mana table sees only published decisions, announces plan-payable
// casts through Registry.SubmitIntent, cancels the first (the card returns
// to hand), undoes a tap when one is offered, pays the rest in the select
// mana window, and the finished match replays to the same head.
func TestAnnouncePayHumanSeatThroughHost(t *testing.T) {
	var human *HumanSeat
	o := testOptions(t)
	o.LoadDeck = repoDeckLoader(t)
	o.Seats = humanFirstSeat(&human)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{ID: "t1", Name: "announce-pay", Seats: 2, Decks: []string{"ur-delver", "uw-control"},
		Seed: 20260927, Pace: 0, Spectator: view.Omniscient, AutoMana: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	last := ^uint64(0)
	announced, windows, taps, undone, cancelled, completed := 0, 0, 0, 0, 0, 0
	var cancelledCard, pendingCard state.ObjID
	tappedThisCast := false
	for {
		if time.Now().After(deadline) {
			t.Fatalf("announce lane did not finish: announced=%d windows=%d cancelled=%d completed=%d", announced, windows, cancelled, completed)
		}
		ms, err := r.Matches("t1")
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		if ms[0].State == protocol.MatchCrashed {
			t.Fatalf("match crashed: %+v", ms[0])
		}
		if ms[0].State == protocol.MatchFinished {
			break
		}
		d, err := r.Pending("t1", 1, 0)
		if err != nil || d.Seq == last {
			time.Sleep(time.Millisecond)
			continue
		}
		last = d.Seq
		var in decision.Intent
		switch {
		case d.ManaPayment != nil:
			windows++
			pendingCard = d.ManaPayment.Card
			pick := func(kind string) (decision.Option, bool) {
				for _, o := range d.Options {
					if o.Kind == kind {
						return o, true
					}
				}
				return decision.Option{}, false
			}
			var o decision.Option
			var ok bool
			switch {
			case cancelled == 0:
				o, ok = pick(decision.OptCancelCast)
				cancelled++
				cancelledCard = d.ManaPayment.Card
			case undone == 0 && hasKind(d, decision.OptUndoTap):
				o, ok = pick(decision.OptUndoTap)
				undone++
			case !tappedThisCast:
				// One manual tap per cast, on a source Auto-fill would use.
				for _, c := range d.Options {
					if c.Kind == "mana" && containsObj(d.ManaPayment.AutoFill, c.Obj) && paysOwed(d.ManaPayment.Owed, c.Label) {
						o, ok = c, true
						break
					}
				}
				if ok {
					taps++
					tappedThisCast = true
					break
				}
				fallthrough
			default:
				if o, ok = pick(decision.OptAutoFill); !ok {
					// A wrong-colour manual tap can leave nothing to plan:
					// cancel, and expect the card back in hand.
					o, ok = pick(decision.OptCancelCast)
					cancelledCard = d.ManaPayment.Card
				}
			}
			if !ok {
				t.Fatalf("announced window offers nothing usable: %#v", d.Options)
			}
			in = decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
		case d.Kind == decision.KPriority:
			if cancelledCard != 0 {
				if z := liveEngineCardZone(t, r, cancelledCard); z != state.ZHand {
					t.Fatalf("cancelled card %d is in %s, want hand", cancelledCard, z)
				}
				cancelledCard, pendingCard = 0, 0
			}
			tappedThisCast = false
			if pendingCard != 0 {
				if z := liveEngineCardZone(t, r, pendingCard); z == state.ZHand {
					t.Fatalf("announced card %d is back in hand after paying", pendingCard)
				}
				completed++
				pendingCard = 0
			}
			in = legalIntent(d)
			for _, a := range d.PaymentActions {
				if announced < 4 && len(a.Plans) > 0 && a.BaseOptionIndex == nil {
					in = decision.Intent{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID}}
					announced++
					break
				}
			}
			if in.Announce == nil {
				for _, o := range d.Options {
					if o.Kind == "play_land" {
						in = decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
						break
					}
				}
			}
		default:
			in = legalIntent(d)
		}
		if err := r.SubmitIntent("t1", 1, 0, in); err != nil {
			t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
		}
	}
	if announced < 3 || cancelled != 1 || completed < 2 || taps < 1 || undone != 1 {
		t.Fatalf("announced=%d windows=%d taps=%d undone=%d cancelled=%d completed=%d", announced, windows, taps, undone, cancelled, completed)
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
	fm.mu.RLock()
	log, cfg := fm.e.L.Clone(), fm.cfg
	fm.mu.RUnlock()
	replayed, err := replay.Replay(log, cfg)
	if err != nil {
		t.Fatalf("replay announced host game: %v", err)
	}
	if got, want := replayed.L.Head(), log.Head(); got != want {
		t.Fatalf("replay head = %s, want %s", got, want)
	}
	t.Logf("announced=%d windows=%d taps=%d undone=%d cancelled=%d completed=%d events=%d", announced, windows, taps, undone, cancelled, completed, len(log.Events))
}

// An announce the parked decision does not offer is refused before the seat
// is woken, and the match does not move.
func TestAnnouncePaySubmitIntentRejectsUnofferedAnnounce(t *testing.T) {
	var human *HumanSeat
	o := testOptions(t)
	o.Seats = humanFirstSeat(&human)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{ID: "t1", Name: "announce-reject", Seats: 2, Decks: []string{"a", "b"},
		Seed: 20260927, Spectator: view.Omniscient, AutoMana: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	d := waitPending(t, r, "t1", 1, 0)
	bad := decision.Intent{Seq: d.Seq, Player: 0, Announce: &decision.AnnounceSelection{ActionID: "not-offered"}}
	if err := r.SubmitIntent("t1", 1, 0, bad); err == nil {
		t.Fatal("unoffered announce accepted")
	}
	again, err := r.Pending("t1", 1, 0)
	if err != nil || again.Seq != d.Seq {
		t.Fatalf("rejected announce moved the game: %+v %v", again, err)
	}
}

func hasKind(d *decision.Decision, kind string) bool {
	for _, o := range d.Options {
		if o.Kind == kind {
			return true
		}
	}
	return false
}

func containsObj(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// paysOwed reports whether a "mana" option's production (its label's last
// pip letter) covers an owed coloured pip, or generic when one is owed.
func paysOwed(owed decision.PaymentCost, label string) bool {
	if label == "" {
		return false
	}
	i := strings.IndexByte("WUBRGC", label[len(label)-1])
	return i >= 0 && (owed.Mana[i] > 0 || owed.Generic > 0)
}
