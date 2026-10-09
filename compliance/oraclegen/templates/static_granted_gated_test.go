package templates

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestStaticGatedGrantOffered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, kind string }{
		{"Evendo, Waking Haven", "static#0.0", "activate"},
		{"The Eternity Elevator", "static#0.0", "activate"},
		{"Kavaron, Memorial World", "static#0.0", "activate"},
		{"Susur Secundi, Void Altar", "static#0.0", "activate"},
		// Max-speed grants are the engine's "granted" option kind, which an
		// "activate" assertion does not match.
		{"Muraganda Raceway", "static#0.0", "granted"},
		{"Amonkhet Raceway", "static#0.0", "granted"},
		{"Endrider Catalyzer", "static#0.0", "granted"},
		{"Howlsquad Heavy", "static#0.1", "granted"},
		// A speed-gated card with its own ETB is cast, not placed.
		{"Kickoff Celebrations", "static#0.0", "granted"},
		{"Perilous Snare", "static#0.0", "granted"},
	} {
		card := tc.card
		t.Run(card, func(t *testing.T) {
			req := counterReq(t, reg, card, tc.key)
			item, skip := GenerateB(reg, card, req)
			if skip != nil {
				t.Fatalf("GenerateB skip = %v; want a served self offered observation", skip)
			}
			if len(item.Scenario.Steps) == 0 {
				t.Fatal("served item has no offered-observation steps")
			}
			found := false
			for _, step := range item.Scenario.Steps {
				for _, ex := range step.Expect {
					if ex.Offered != nil && ex.Offered.Card == "p0:"+card && ex.Offered.Kind == tc.kind && ex.Want != nil && *ex.Want {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("scenario does not assert an offered self activation: %+v", item.Scenario.Steps)
			}
		})
	}
}

func TestStaticGatedGrantFallsThroughToNamedGap(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, reason string }{
		// Debris Field Crusher left this table when the setup-counters
		// fixture served its station gate on the placed path
		// (TestStaticCounterGatesServed): the cast-path gap it pinned is gone.
		// Dawnsire's trigger grant moved to the granted-trigger observation
		// (static_granted_trigger_test.go pins its served row).
		{"Far Fortune, End Boss", "static#0.0", staticGrantReplacementReason},
	} {
		t.Run(tc.card, func(t *testing.T) {
			req := counterReq(t, reg, tc.card, tc.key)
			_, skip := GenerateB(reg, tc.card, req)
			if skip == nil || skip.Reason != "static "+tc.reason {
				t.Fatalf("skip = %v, want static %q", skip, tc.reason)
			}
		})
	}
}

// TestStaticGatedGrantControlRejectsGateOff pins the gate-off control a gated
// self grant is served with. The served scenario's own offered label is
// re-run on the same board with ONLY the gate removed (the counters, or max
// speed) and must not be offered; a grant that ignored its static gate would
// fail here with offered=true where want=false.
func TestStaticGatedGrantControlRejectsGateOff(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, kind string }{
		{"Evendo, Waking Haven", "static#0.0", "activate"},
		{"Muraganda Raceway", "static#0.0", "granted"},
		{"Amonkhet Raceway", "static#0.0", "granted"},
	} {
		card, key := tc.card, tc.key
		t.Run(card, func(t *testing.T) {
			req := counterReq(t, reg, card, key)
			item, skip := GenerateB(reg, card, req)
			if skip != nil {
				t.Fatalf("GenerateB skip = %v; want a served self offered observation", skip)
			}
			sc := item.Scenario
			if len(sc.Steps) == 0 {
				t.Fatal("served item has no steps")
			}
			last := sc.Steps[len(sc.Steps)-1]
			if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
				t.Fatalf("last step is not a want=true offered expectation: %+v", last)
			}
			off := last.Expect[0].Offered
			if off.Card != "p0:"+card || off.Kind != tc.kind || off.Label == "" {
				t.Fatalf("offered expectation = %+v; want kind %q on p0:%s with a non-empty label", off, tc.kind, card)
			}

			st := staticAt(t, reg, card, key)
			ckind, need, counterGated := staticCounterGate(&st)
			speedGated := staticGatedOnMaxSpeed(&st)
			if counterGated == speedGated {
				t.Fatalf("%s must be gated by exactly one of counters (%v) or max speed (%v)", card, counterGated, speedGated)
			}
			// The gate-on board really holds the gate the control removes.
			on := sc.Setup["p0"]
			if !slices.Contains(on.Battlefield, card) {
				t.Fatalf("precondition: %s is absent from p0's battlefield: %v", card, on.Battlefield)
			}
			if counterGated && need <= 0 {
				t.Fatalf("precondition: counter gate needs %d; want a positive threshold", need)
			}
			if counterGated && on.Counters[card][ckind] < need {
				t.Fatalf("gate-on board has %d %s counters on %s; the gate needs %d", on.Counters[card][ckind], ckind, card, need)
			}
			if speedGated && on.Speed != maxSpeed {
				t.Fatalf("gate-on board speed = %d; want max speed %d", on.Speed, maxSpeed)
			}

			setup := cloneSeats(sc.Setup)
			p0 := setup["p0"]
			if counterGated {
				delete(p0.Counters[card], ckind)
			} else {
				p0.Speed = 0
			}
			setup["p0"] = p0
			control := sc
			control.Setup = setup
			control.Steps = append(append([]oraclegen.Step(nil), sc.Steps[:len(sc.Steps)-1]...),
				gatedGrantExpectation(card, off.Kind, off.Label, false))

			res, ok := runStatic(reg, control)
			if !ok {
				t.Fatal("gate-off control did not run")
			}
			if len(res.Fails) != 0 {
				t.Fatalf("gate-off control still offers %q (%s) with the gate removed: %v", off.Label, off.Kind, res.Fails)
			}
			// Removing the control's gate must not alter the positive fixture.
			if !slices.Contains(p0.Battlefield, card) {
				t.Fatalf("control lost its battlefield source %s", card)
			}
			if counterGated && (p0.Counters[card][ckind] != 0 || on.Counters[card][ckind] < need) {
				t.Fatalf("counter gates must differ: on=%d, off=%d, need=%d", on.Counters[card][ckind], p0.Counters[card][ckind], need)
			}
			if speedGated && (p0.Speed != 0 || on.Speed != maxSpeed) {
				t.Fatalf("speed gates must differ: on=%d, off=%d", on.Speed, p0.Speed)
			}
		})
	}
}
