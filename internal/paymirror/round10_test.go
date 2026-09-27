package paymirror

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestRoundTenFindingsMirror replays the round-10 paymirror finding games
// (testdata/round10.jsonl: repo deck names or random card-name lists, no
// script text) end to end: no cast is a mismatch, no planned cast falls back
// or misses its witness, the live-vs-clone control is equivalent, and each
// named cast gets its root-caused verdict.
//
//   - 12468 seq 7211 (Homarid Spawning Bed), random4 state_differs on
//     G.Objs[*].Remembered[*].Obj: Veiled Crocodile's CR 603.8 state trigger
//     remembered the Island tapped in run A's CR 601.2g window but itself on
//     the float route. A rules fix: a state trigger remembers its source
//     (stateTriggerRemembered).
//   - 11828 seq 4243 (Vorinclex, Voice of Hunger), random2
//     a_fallback=source_changed: the plan's Fanatic of Rhonas step ("Activate
//     only if you control a creature with power 4 or greater") held only
//     through Syr Elenora (power = cards in hand) while Vorinclex was still
//     in hand; CR 601.2a moved it to the stack first, the step fell back and
//     the cast reversed. A rules fix: the offer proves every planned step
//     with the card on the stack (paymentPlanHoldsOnStack). The plan is no
//     longer offered there, so the game moves; the pin is that no planned
//     cast in it falls back or mismatches. (The report's control route was
//     equivalent: the source_changed was run A's fallback, not the clone.)
//   - 12603 seq 2300 (Mana Vault), commander4 a_witness:unexecuted_activation:
//     Treasonous Ogre's "Pay 3 life: Add {R}" logged life -3, an opponent's
//     inline speed gain, then R; the witness required the mana to follow the
//     payment immediately (harness gap, lifePaymentConsequences).
//   - 10877 seq 2873 (Infernal Plunge), commander4-r9 a_witness:wrong_production:
//     the plan's Ogre paid life for R and was then the creature sacrificed
//     for the spell's additional cost (CR 601.2h); the witness read that
//     sacrifice as its activation (harness gap, activationProduction).
//
// fb-20260927T163321Z-69285807 moved the commander seeds here (12468, 11828,
// 12603, 10877) with the command-zone payment-plan fix: a commander in the
// command zone now gets a plan, the auto-pay bots cast it through one, and
// those games move. 10877 keeps the same Infernal Plunge pin at its new seq;
// the other three already carry empty or unchanged pins.
func TestRoundTenFindingsMirror(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/round10.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var specs []GameSpec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var s GameSpec
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		specs = append(specs, s)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("testdata holds %d specs, want 4", len(specs))
	}
	want := map[uint64]map[uint64]string{ // seed -> seq -> verdict key ("" = equivalent)
		12468: {7211: ""},
		11828: {},
		12603: {2300: ""},
		10877: {2834: ""},
	}
	for _, spec := range specs {
		reports := round6Game(t, d, spec)
		seen := map[uint64]bool{}
		for _, r := range reports {
			st, key := r.Verdict()
			if st == Mismatch || (st == Unmirrorable && !r.ExpectedUnmirrorable()) {
				t.Errorf("seed %d seq %d %q: %s %s", spec.Seed, r.Seq, r.Card, st, key)
			}
			if r.AFallback != "" || r.AWitness != "" {
				t.Errorf("seed %d seq %d %q: fallback %q witness %q", spec.Seed, r.Seq, r.Card, r.AFallback, r.AWitness)
			}
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", spec.Seed, r.Seq, r.Card, r.Control)
			}
			w, ok := want[spec.Seed][r.Seq]
			if !ok {
				continue
			}
			seen[r.Seq] = true
			if key != w {
				t.Errorf("seed %d seq %d %q: verdict %s %q, want %q", spec.Seed, r.Seq, r.Card, st, key, w)
			}
		}
		for seq := range want[spec.Seed] {
			if !seen[seq] {
				t.Errorf("seed %d: no planned cast at seq %d (the game no longer reaches the finding)", spec.Seed, seq)
			}
		}
		if len(reports) == 0 {
			t.Errorf("seed %d: no planned cast at all", spec.Seed)
		}
	}
}

// TestWitnessReadsLifePaymentPastSpeedGain pins lifePaymentConsequences: an
// opponent's speed gain the life payment folds inline sits between a
// pay-life-only source's payment and its mana, and is passed over; any other
// event there still breaks the start.
func TestWitnessReadsLifePaymentPastSpeedGain(t *testing.T) {
	const payer state.PlayerID = 1
	ogre := decision.PaymentActivation{Source: 107, Produces: decision.ManaAmount{0, 0, 0, 1, 0, 0},
		Consequence: &decision.PaymentConsequence{Life: 3}}
	plan := decision.PaymentPlan{Activations: []decision.PaymentActivation{ogre}}
	life := events.Event{Kind: events.LifeChange, Player: payer, Amount: -3}
	speed := events.Event{Kind: events.SpeedChange, Player: 3, Amount: 1, Text: "speed"}
	red := events.Event{Kind: events.ManaAdd, Player: payer, Amount: 1, Counter: "R"}
	evs := []events.Event{life, speed, red}
	if s := lifeOnlyStarts(evs, plan, payer); s[0] != 0 {
		t.Fatalf("starts = %v, want [0] past the speed gain", s)
	}
	if got := manaRunAfter(evs, 0); got != ogre.Produces {
		t.Fatalf("mana after the payment = %v, want R", got)
	}
	if v := witnessOver(t, evs, plan, payer); v != "" {
		t.Fatalf("witness %q, want none", v)
	}
	broken := []events.Event{life, {Kind: events.Draw, Player: payer}, red}
	if s := lifeOnlyStarts(broken, plan, payer); s[0] != -1 {
		t.Fatalf("a draw between payment and mana read as a start: %v", s)
	}
}

// TestWitnessPassesSourceSacrificedForTheSpell pins activationProduction:
// a pay-life-only source sacrificed afterwards for the spell's own
// additional cost (CR 601.2h) is still read through its life payment, while
// a source that tapped and made nothing is still wrong_production.
func TestWitnessPassesSourceSacrificedForTheSpell(t *testing.T) {
	const payer state.PlayerID = 1
	ogre := decision.PaymentActivation{Source: 107, Produces: decision.ManaAmount{0, 0, 0, 1, 0, 0},
		Consequence: &decision.PaymentConsequence{Life: 3}}
	plan := decision.PaymentPlan{Activations: []decision.PaymentActivation{ogre}}
	evs := []events.Event{
		{Kind: events.PutOnStack, Obj: 109, Player: payer},
		{Kind: events.LifeChange, Player: payer, Amount: -3},
		{Kind: events.ManaAdd, Player: payer, Amount: 1, Counter: "R"},
		{Kind: events.ManaAdd, Player: payer, Amount: -1, Counter: "R"},
		{Kind: events.MoveZone, Obj: 107, From: state.ZBattlefield, To: state.ZGraveyard, Text: "sacrificed"},
		{Kind: events.TriggerPush, Obj: 138, Player: payer},
	}
	if v := witnessOver(t, evs, plan, payer); v != "" {
		t.Fatalf("witness %q, want none", v)
	}
	tapped := []events.Event{
		{Kind: events.Tap, Obj: 107},
		{Kind: events.LifeChange, Player: payer, Amount: -3},
		{Kind: events.Note, Player: payer},
		{Kind: events.LifeChange, Player: payer, Amount: -3},
		{Kind: events.ManaAdd, Player: payer, Amount: 1, Counter: "R"},
	}
	if v := witnessOver(t, tapped, plan, payer); v == "" {
		t.Fatal("a tapped source that made nothing passed through another life payment")
	}
}

// witnessOver runs witnessViolation over a fixture engine whose log since the
// fork is exactly evs (the witness reads only a.L.Events[fork:], object names
// and the cast object's zone).
func witnessOver(t *testing.T, evs []events.Event, plan decision.PaymentPlan, payer state.PlayerID) string {
	t.Helper()
	e := fixtureEngine(t)
	fork := len(e.L.Events)
	e.L.Events = append(e.L.Events, evs...)
	return witnessViolation(e, fork, plan, payer)
}
