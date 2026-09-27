package rules

import (
	"slices"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCountValidDerivationIsNotFactorial (general; the fuzzer's "hang"
// records: every off-lane hang game profiled 98-99% under staticAmountOn ->
// zoneCountFold.visit -> FilterDerivedPT -> derivedScalarFrom, and 4 of 7
// did not finish in 7 CPU-minutes): Count$Valid bound every battlefield
// candidate's derived P/T (effects/count.go zoneCountFold.visit) even when
// the spec reads none ("Artifact.YouCtrl"), so a P/T CDA that counts
// permanents (Master of Etherium: SetPower$ X / SetToughness$ X,
// X:Count$Valid Artifact.YouCtrl) derived every other counted creature,
// which counted again: the in-progress frame guard stops the cycle but not
// the factorial fan-out. Measured on this box before the fix: 4 Masters
// 2.9 ms, 5 44 ms, 6 577 ms, 7 10.6 s for ONE Derived. Eight must be
// instant; SpecReadsPT (effects/filter.go) now skips the bind unless the
// spec reads a P/T comparison field.
func TestCountValidDerivationIsNotFactorial(t *testing.T) {
	e := layerEngine(t)
	first := onBoardCard(t, e, 0, corpusCard(t, "Master of Etherium"))
	for i := 1; i < 8; i++ {
		onBoardCard(t, e, 0, corpusCard(t, "Master of Etherium"))
	}
	// Preconditions: the assertion below only exercises the counting CDA if
	// the board really holds eight artifacts and the corpus card really
	// carries the characteristic-defining count. A fixture without either
	// must fail loudly, not pass a 0/0 through.
	src := e.G.Obj(first)
	if src == nil || src.Face() == nil {
		t.Fatal("precondition: first Master of Etherium is not a face object on the battlefield")
	}
	if !src.Face().CharacteristicDefining() {
		t.Fatalf("precondition: Master of Etherium carries no P/T CDA (PT=%q)", src.Face().PT)
	}
	artifacts := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && slices.Contains(o.Face().Types, "Artifact") {
			artifacts++
		}
	}
	if artifacts != 8 {
		t.Fatalf("precondition: battlefield holds %d artifacts, want 8", artifacts)
	}
	done := make(chan Derived, 1)
	go func() { done <- e.Derived(first) }()
	select {
	case d := <-done:
		// Master of Etherium is 8/8 from its CDA (8 artifacts), plus +1/+1
		// from each of the other seven Masters' lord static = 15/15.
		if d.Power != 15 || d.Toughness != 15 {
			t.Fatalf("Master of Etherium among 8 = %d/%d, want 15/15 (8 artifacts, +7 from the others)", d.Power, d.Toughness)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Derived of one Master of Etherium among eight did not return within 5s (factorial Count$Valid P/T binding)")
	}
}

// BenchmarkMasterOfEtheriumDerivation pins the cost of one Derived over a
// Master of Etherium board. Before the SpecReadsPT gate each additional
// Master multiplied the work (~x15 per extra counting creature); after it,
// the count spec reads no P/T so no candidate is derived recursively.
func BenchmarkMasterOfEtheriumDerivation(b *testing.B) {
	e := layerEngine(b)
	master, ok := testutil.CorpusRegistry(b).Lookup("Master of Etherium")
	if !ok {
		b.Fatal("corpus fixture Master of Etherium is missing")
	}
	layout := func() {
		for i := 0; i < 8; i++ {
			o := e.G.AddObject(master, 0)
			o.Zone = state.ZBattlefield
			o.SummonSick = true
			e.G.Clock++
			o.Timestamp = e.G.Clock
			e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
			e.staticEpoch = -1
		}
	}
	layout()
	first := e.G.Zone(state.ZBattlefield, 0)[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Derived(first)
	}
}
