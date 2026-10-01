package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// autoPayProbe is a priority decision offering both a manual mana activation
// and a payment plan for a castable creature: an auto-pay seat answers it with
// the offered witness, a manual seat taps the source (the tap gate wants mana
// for the creature it cannot yet pay).
func autoPayProbe() (botpolicy.Board, decision.Decision) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "activate", Obj: 7}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{{ID: "action", Cast: decision.PlannedCast{Object: 9, Origin: "hand"},
			Plans: []decision.PaymentPlan{{ID: "plan", Version: decision.PaymentPlanV1}}}},
	}
	b := botpolicy.Board{IsMain: true, Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		7: {OnBattlefield: true},
		9: {Creature: true, Power: 3, CMC: 3, Castable: true},
	})}
	return b, d
}

func paysByPlan(t *testing.T, s seat.Seat) bool {
	t.Helper()
	bs, ok := s.(seat.BoardSeat)
	if !ok {
		t.Fatalf("seat %T is not a BoardSeat", s)
	}
	b, d := autoPayProbe()
	in, err := bs.DecideBoard(context.Background(), b, d)
	if err != nil {
		t.Fatal(err)
	}
	return in.Payment != nil && in.Payment.ActionID == "action" && in.Payment.Plan.ID == "plan"
}

// TestAutoPayFlagReachesFittedSidesAndBaseline pins -auto-pay's wiring: with
// it off, neither the fitted-side constructor nor the BenchVsBot baseline
// pays by plan (the historical manual fit); with it on, BOTH do, so a fitted
// auto-pay profile is measured against the auto-pay default it would replace
// rather than against the manual-tapping bot.
func TestAutoPayFlagReachesFittedSidesAndBaseline(t *testing.T) {
	d := &devSuite{ctor: func(seed uint64, w Weights) seat.Seat { return seat.NewCastProfileBotWithWeights(seed, w) }}
	w := botpolicy.DefaultCastWeights
	if paysByPlan(t, d.ctor(3, w)) || paysByPlan(t, d.baselineCtor()(3)) {
		t.Fatal("without -auto-pay a seat paid by plan; the manual fit must stay manual")
	}
	d.setAutoPay(true)
	if !paysByPlan(t, d.ctor(3, w)) {
		t.Fatal("-auto-pay: the fitted side did not pay by the offered plan")
	}
	if !paysByPlan(t, d.baselineCtor()(3)) {
		t.Fatal("-auto-pay: the BenchVsBot baseline did not pay by the offered plan")
	}
}

// TestAutoPayFlagParses runs the real flag path end to end on one pair and one
// iteration, so a typo in the flag wiring fails here rather than in a fit.
func TestAutoPayFlagParses(t *testing.T) {
	dir := corpusDir(t)
	var out, errb bytes.Buffer
	code := mainExit([]string{"-dir", dir, "-pairs", "mono-black-aggro:mono-white-equipment",
		"-games", "1", "-iters", "1", "-seed", "7", "-workers", "2", "-fit", "CreatureBase", "-auto-pay", "-quiet"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
}
