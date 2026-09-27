package host

import (
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
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

// decksHas reports whether name appears anywhere in decks. Match 1 rotates
// TableConfig.Decks by one (host/match.go's (i+k)%len), so seat 0 plays
// decks[1]: the authored dual-land deck must be found in either position.
func decksHas(decks []string, name string) bool {
	for _, d := range decks {
		if d == name {
			return true
		}
	}
	return false
}

// ppDualLandLoader serves "pp-dual", an authored mana base whose ONLY land is
// a multi-intrinsic dual. Every planned payment for the deck's red or blue
// spells must tap that Volcanic Island, so the hosted lane exercises the exact
// alternative the witness names (spec §6): a dual land's {U} and {R}
// abilities share the {intrinsic, basic_land} PaymentAbility identity, and the
// executor must activate the one the step's Produces names, never the first
// identity match. The repo-deck planner ranks basics ahead of duals, so no
// real ur-delver/uw-control game ever plans a dual; without this deck the lane
// passed even with the dual-land executor fix reverted.
func ppDualLandLoader(t *testing.T) func(string) (Deck, error) {
	t.Helper()
	repo := repoDeckLoader(t)
	reg := testutil.CorpusRegistry(t)
	card := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus lacks %q for the dual-land PP-20 lane", name)
		}
		return c
	}
	var cs []*cards.Card
	add := func(name string, n int) {
		c := card(name)
		for i := 0; i < n; i++ {
			cs = append(cs, c)
		}
	}
	add("Volcanic Island", 16)
	add("Lightning Bolt", 8)
	add("Ponder", 8)
	add("Delver of Secrets", 8)
	add("Monastery Swiftspear", 8)
	return func(name string) (Deck, error) {
		if name == "pp-dual" {
			return Deck{Name: name, Cards: cs}, nil
		}
		return repo(name)
	}
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
	switch {
	case decksHas(decks, "pp-dual"):
		o.LoadDeck = ppDualLandLoader(t)
	case len(decks) == 2 && decks[0] == "ur-delver" && decks[1] == "uw-control":
		o.LoadDeck = repoDeckLoader(t)
	}
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
	plannedCards := make([]state.ObjID, 0, 2)
	plannedIDs := make(map[string]bool, 2)
	checkedPlans := 0
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
		// A selected witness that returns to the ordinary manual flow did
		// not succeed as planned: the engine only attaches PaymentFallback to
		// a manual window after an accepted plan stops. The human submits each
		// witness with no intervening board change, so any fallback naming a
		// plan this lane selected is a real failed planned cast (the P0 class
		// 5308ef397 fixed, and the class this lane exists to catch).
		if d.PaymentFallback != nil && plannedIDs[d.PaymentFallback.PlanID] {
			t.Fatalf("planned cast fell back to manual payment: seq=%d reason=%s plan=%s", d.Seq, d.PaymentFallback.Reason, d.PaymentFallback.PlanID)
		}
		if d.Kind == decision.KPriority && checkedPlans < len(plannedCards) {
			r.mu.RLock()
			tb := r.tables["t1"]
			r.mu.RUnlock()
			tb.mu.RLock()
			fm := tb.cur
			tb.mu.RUnlock()
			if fm == nil {
				t.Fatal("live payment-plan match disappeared before checking cast")
			}
			fm.mu.RLock()
			for _, id := range plannedCards[checkedPlans:] {
				obj := fm.e.G.Obj(id)
				if obj == nil || obj.Zone == state.ZHand {
					fm.mu.RUnlock()
					t.Fatalf("planned card %d is back in hand when seat next holds priority: %#v", id, obj)
				}
			}
			fm.mu.RUnlock()
			checkedPlans = len(plannedCards)
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
			if len(d.PaymentActions) == 0 {
				t.Fatal("planned intent had no published PaymentAction")
			}
			plannedCards = append(plannedCards, d.PaymentActions[0].Cast.Object)
			plannedIDs[d.PaymentActions[0].Plans[0].ID] = true
		}
		last = d.Seq
	}
	if selected < 2 {
		t.Fatalf("external human seat selected %d payment actions, want two", selected)
	}
	if checkedPlans != len(plannedCards) {
		t.Fatalf("checked %d of %d planned casts before match completion", checkedPlans, len(plannedCards))
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
	t.Run("dual_land_two_seats", func(t *testing.T) {
		runPaymentPlanHumanSeat(t, 2, []string{"ur-delver", "uw-control"}, 20260926)
	})
	t.Run("dual_only_two_seats", func(t *testing.T) {
		// Match 1 rotates Decks, so seat 0 (the human) plays decks[1]: put the
		// authored dual-only mana base there.
		runPaymentPlanHumanSeat(t, 2, []string{"mono-red-goblins", "pp-dual"}, 20260927)
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
// capability is enforced at the host boundary. Neither the human nor the
// plain bot consumer builds the lazy extension on an off table, and the human
// receives only ordinary options through the legacy pre-payment-plan path.
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
		m := liveMatch(t, r, "t1")
		m.mu.RLock()
		pending := m.e.Pending()
		built := pending != nil && pending.Seq == d.Seq && pending.PaymentActionsBuilt
		m.mu.RUnlock()
		if built {
			t.Fatalf("non-consumer seat %d built payment actions on AutoMana-off table", d.Player)
		}
		if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
			t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
		}
		last = d.Seq
		answered++
	}
}
