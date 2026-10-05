package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// costSpareCapacity names the first slice field of c whose capacity exceeds
// its length (spare backing capacity an append could write into), and reports
// whether c has one.
func costSpareCapacity(c Cost) (string, bool) {
	v := reflect.ValueOf(c)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Slice && f.Cap() > f.Len() {
			return v.Type().Field(i).Name, true
		}
	}
	return "", false
}

// TestAltCastFaceCostSidecar pins the per-face compile of the alternative-cost
// keyword family's printed parameters (rules/walk_face_facts.go altCosts,
// populated from altCastMode.faceCostRaw):
//
//   - every row's sidecar entry equals a fresh parse,
//   - altCastMode.faceCost SERVES the sidecar (a zeroed entry reads absent),
//     rather than reparsing on every offer,
//   - the cached Cost is frozen (capacity-clipped) so no caller can append
//     into the shared backing array,
//   - a face with no current sidecar still parses (the fallback).
//
// The probe face carries all eight rows, one of them (Blitz) with a
// spare-capacity Sac slice, so the freeze assertion is not vacuous.
func TestAltCastFaceCostSidecar(t *testing.T) {
	c := card(t, "Name:AltCost Sidecar Probe\nManaCost:2 R\nTypes:Creature Test\nPT:2/2\n"+
		"K:Evoke:1 R\nK:Dash:2 R\nK:Overload:3 R\nK:Warp:4 R\nK:Impending:3:5 R\n"+
		"K:Bestow:6 R\nK:Blitz:1 R Sac<1/Creature> Sac<1/Creature> Sac<1/Creature>\n"+
		"K:Madness:8 R\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	f := c.Faces[0]

	// Precondition: the compiled sidecar exists and its keyword guard passes
	// (a nil or stale sidecar would make the reader fall back and the whole
	// test vacuous).
	ff := e.walkFaceFactsOf(f)
	if ff == nil || !ff.keywordsCurrent(f) {
		t.Fatalf("precondition: no current sidecar for %q", f.Name)
	}

	present := altCastID(0)
	foundPresent, spareSeen := false, false
	for i := range altCastModes {
		m := &altCastModes[i]
		freshCost, freshOK := m.faceCostRaw(f)
		cached := ff.altCosts[i]
		if cached.ok != freshOK || !reflect.DeepEqual(cached.cost, freshCost) {
			t.Errorf("sidecar row %s = (%#v, %v), fresh parse (%#v, %v)", m.mode, cached.cost, cached.ok, freshCost, freshOK)
		}
		if gotCost, gotOK := m.faceCost(f, ff); gotOK != freshOK || !reflect.DeepEqual(gotCost, freshCost) {
			t.Errorf("faceCost(%s) = (%#v, %v), fresh parse (%#v, %v)", m.mode, gotCost, gotOK, freshCost, freshOK)
		}
		if freshOK && !foundPresent {
			present, foundPresent = altCastID(i), true
		}
		// Freeze: the raw parse of the Blitz probe carries spare capacity in
		// its Sac slice; the cached copy must not.
		if _, spare := costSpareCapacity(freshCost); spare {
			spareSeen = true
			if field, spare := costSpareCapacity(cached.cost); spare {
				t.Errorf("sidecar row %s is not frozen: %s slice has spare capacity", m.mode, field)
			}
		}
	}
	if !foundPresent {
		t.Fatal("precondition: the probe face carries no alternative-cost keyword; the test would be vacuous")
	}
	if !spareSeen {
		t.Fatal("precondition: no row's fresh parse had spare slice capacity; the freeze assertion would be vacuous")
	}

	// The reader must serve the sidecar: a shallow copy with the present row
	// zeroed reads absent. (A reparse would still return ok.)
	cp := *ff
	cp.altCosts[present] = altFaceCost{}
	if _, ok := altCastModes[present].faceCost(f, &cp); ok {
		t.Errorf("faceCost(%s) with a zeroed sidecar entry returned ok; it reparsed instead of serving the sidecar", altCastModes[present].mode)
	}

	// The fallback still parses for a face with no sidecar.
	if _, ok := altCastModes[present].faceCost(f, nil); !ok {
		t.Errorf("faceCost(%s) with a nil sidecar returned !ok", altCastModes[present].mode)
	}
}

// BenchmarkAltCastFaceCostCached is the per-offer read the offer walk, the
// command-zone offer, the potential-plan pricing and the charge now make:
// one sidecar lookup. BenchmarkAltCastFaceCostParse is the parse that read
// replaced (altCastMode.faceCostRaw), the per-offer work before this ticket.
// Run both in one invocation: `go test -run '^$' -bench
// 'BenchmarkAltCastFaceCost' ./rules/`.
func BenchmarkAltCastFaceCostCached(b *testing.B) {
	m, f, ff := altCastFaceCostBench(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := m.faceCost(f, ff); !ok {
			b.Fatal("sidecar row missing")
		}
	}
}

// BenchmarkAltCastFaceCostParse is the raw parse the cached read replaced.
func BenchmarkAltCastFaceCostParse(b *testing.B) {
	m, f, _ := altCastFaceCostBench(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := m.faceCostRaw(f); !ok {
			b.Fatal("row missing")
		}
	}
}

func altCastFaceCostBench(b *testing.B) (*altCastMode, *cards.Face, *walkFaceFacts) {
	b.Helper()
	c := card(b, "Name:AltCost FaceCost Bench\nManaCost:2 R\nTypes:Creature Test\nPT:2/2\n"+
		"K:Evoke:1 R Sac<1/Creature> Sac<1/Creature> Sac<1/Creature>\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	f := c.Faces[0]
	ff := e.walkFaceFactsOf(f)
	m := &altCastModes[altEvoke]
	if ff == nil || !ff.keywordsCurrent(f) || !ff.altCosts[altEvoke].ok {
		b.Fatal("precondition: no current sidecar for the bench face")
	}
	return m, f, ff
}
