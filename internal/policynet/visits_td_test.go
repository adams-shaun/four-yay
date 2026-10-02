package policynet

import (
	"math"
	"testing"
)

// A hand-built file: one game, two seats. Seat 0's three records are out of
// sequence order in the file; seat 1 has one record; a second game's outcome
// is unknown.
func TestVisitTDTargets(t *testing.T) {
	recs := []VisitRecord{
		{GameID: "g0", Seed: 7, Seat: 0, Sequence: 30, RootValue: 0.2, Outcome: 1, OutcomeKnown: true},
		{GameID: "g0", Seed: 7, Seat: 1, Sequence: 12, RootValue: 0.9, Outcome: 0, OutcomeKnown: true},
		{GameID: "g0", Seed: 7, Seat: 0, Sequence: 10, RootValue: 0.4, Outcome: 1, OutcomeKnown: true},
		{GameID: "g0", Seed: 7, Seat: 0, Sequence: 20, RootValue: 0.6, Outcome: 1, OutcomeKnown: true},
		{GameID: "g1", Seed: 8, Seat: 0, Sequence: 5, RootValue: 0.7},
	}
	const l = 0.5
	y, ok := VisitTDTargets(recs, l)
	// Seat 0, newest first: y(30) = l*1 + (1-l)*0.2, y(20) = l*y(30) +
	// (1-l)*0.6, y(10) = l*y(20) + (1-l)*0.4.
	y30 := l*1 + (1-l)*0.2
	y20 := l*y30 + (1-l)*0.6
	y10 := l*y20 + (1-l)*0.4
	want := []float64{y30, l*0 + (1-l)*0.9, y10, y20, 0}
	wantOK := []bool{true, true, true, true, false}
	for i := range recs {
		if ok[i] != wantOK[i] || math.Abs(y[i]-want[i]) > 1e-15 {
			t.Errorf("record %d: target %v ok %v, want %v ok %v", i, y[i], ok[i], want[i], wantOK[i])
		}
	}
	// lambda 1 is the game outcome at every record.
	y, _ = VisitTDTargets(recs, 1)
	if y[0] != 1 || y[2] != 1 || y[3] != 1 || y[1] != 0 {
		t.Errorf("lambda 1 targets %v, want the outcome", y)
	}
}

// A trajectory target replaces the blend; without one the blend stands.
func TestValueTargetPrefersTheTrajectoryTarget(t *testing.T) {
	lc := LossConfig{ValueWeight: 1}
	ex := Example{Outcome: 1, HasOutcome: true, TeacherValue: 0.3, HasTeacherValue: true}
	if v, ok := lc.ValueTarget(ex); !ok || v != 1 {
		t.Fatalf("blend 0 target %v %v", v, ok)
	}
	ex.TDTarget, ex.HasTDTarget = 0.61, true
	if v, ok := lc.ValueTarget(ex); !ok || v != 0.61 {
		t.Fatalf("trajectory target %v %v", v, ok)
	}
}

// PolicyWeight 0 leaves a visit example no policy term and no gradient on
// the policy head; the default config is untouched.
func TestVisitPolicyWeight(t *testing.T) {
	m, ex, lc := visitFixture(t, false, 11)
	base := m.Loss(ex, lc)
	g := m.NewGrads()
	g.Zero()
	lc.ScalePolicy, lc.PolicyWeight = true, 0
	st := m.LossGrad(ex, lc, g)
	if st.Parts.Rank != 0 || st.Parts.ValueHead != base.Parts.ValueHead || st.Loss != base.Parts.ValueHead {
		t.Fatalf("policy weight 0: %+v, base %+v", st.Parts, base.Parts)
	}
	for _, blk := range [][]float32{g.HidW, g.HidB, g.OutW, {g.OutB}} {
		for i, v := range blk {
			if v != 0 {
				t.Fatalf("policy head gradient [%d] = %g at policy weight 0", i, v)
			}
		}
	}
	nz := false
	for _, v := range g.VOutW {
		nz = nz || v != 0
	}
	if !nz {
		t.Fatal("the value head got no gradient")
	}
	lc.PolicyWeight = 0.5
	if half := m.Loss(ex, lc); math.Abs(half.Parts.Rank-0.5*base.Parts.Rank) > 1e-12 {
		t.Fatalf("policy weight 0.5: rank %g, want half of %g", half.Parts.Rank, base.Parts.Rank)
	}
}
