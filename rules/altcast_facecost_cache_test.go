package rules

import (
	"math/bits"
	"reflect"
	"testing"
	"unsafe"

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
//   - altCastMode.faceCost SERVES the sidecar (a cleared mask bit reads
//     absent), rather than reparsing on every offer,
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
		var row altFaceCost
		cachedOK := false
		if bit := uint8(1) << altCastID(i); ff.altCostMask&bit != 0 {
			row = ff.altCosts[bits.OnesCount8(ff.altCostMask&(bit-1))]
			cachedOK = true
		}
		if cachedOK != freshOK || (freshOK && !reflect.DeepEqual(row.cost, freshCost)) {
			t.Errorf("sidecar row %s = (%#v, present=%v), fresh parse (%#v, %v)", m.mode, row.cost, cachedOK, freshCost, freshOK)
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
			if field, spare := costSpareCapacity(row.cost); spare {
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

	// The reader must serve the sidecar: a shallow copy with the present
	// row's mask bit cleared reads absent. (A reparse would still return ok.)
	cp := *ff
	cp.altCostMask &^= 1 << present
	if _, ok := altCastModes[present].faceCost(f, &cp); ok {
		t.Errorf("faceCost(%s) with its mask bit cleared returned ok; it reparsed instead of serving the sidecar", altCastModes[present].mode)
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
	if ff == nil || !ff.keywordsCurrent(f) || ff.altCostMask&(1<<altEvoke) == 0 {
		b.Fatal("precondition: no current sidecar for the bench face")
	}
	return m, f, ff
}

// TestWalkFaceFactsAltCostSidecarIsSparse guards the memory shape of the
// alt-cost sidecar. altFaceCost embeds an 824-byte Cost, so storing one per
// altCastModes row by value (a [altCastCount]altFaceCost field) grows
// walkFaceFacts ~25x -- to ~6.9 KB -- for EVERY face, alt-cost or not, and
// every face's facts are held twice (the engine table and the copy published
// on the face's ExtSlot). The sidecar is sparse instead: a face with no
// alt-cost keyword allocates nothing and the facts stay a few hundred bytes.
// A dense array here is a >20x memory regression on a corpus-wide table.
func TestWalkFaceFactsAltCostSidecarIsSparse(t *testing.T) {
	// The dense form measured 6936 bytes; the sparse form ~312. 1024 is a
	// bound the dense array cannot pass and the sparse form clears with
	// room to spare.
	const bound = 1024
	if sz := unsafe.Sizeof(walkFaceFacts{}); sz > bound {
		t.Fatalf("walkFaceFacts is %d bytes (bound %d); a by-value alt-cost array (8x %d-byte altFaceCost) is back",
			sz, bound, unsafe.Sizeof(altFaceCost{}))
	}

	plain := card(t, "Name:AltCost Sparse Plain\nManaCost:1 G\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	allRows := card(t, "Name:AltCost Sparse AllRows\nManaCost:1 G\nTypes:Creature Test\nPT:1/1\n"+
		"K:Evoke:1 R\nK:Dash:2 R\nK:Overload:3 R\nK:Warp:4 R\nK:Impending:3:5 R\n"+
		"K:Bestow:6 R\nK:Blitz:1 R Sac<1/Creature> Sac<1/Creature> Sac<1/Creature>\n"+
		"K:Madness:8 R\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{plain, allRows}}})

	pf := e.walkFaceFactsOf(plain.Faces[0])
	if pf == nil || !pf.keywordsCurrent(plain.Faces[0]) {
		t.Fatal("precondition: no current facts for the plain face")
	}
	if pf.altCostMask != 0 || pf.altCosts != nil {
		t.Errorf("plain face carries alt-cost sidecar data: mask=%#x len=%d; it should allocate nothing", pf.altCostMask, len(pf.altCosts))
	}

	af := e.walkFaceFactsOf(allRows.Faces[0])
	if af == nil || !af.keywordsCurrent(allRows.Faces[0]) {
		t.Fatal("precondition: no current facts for the all-rows face")
	}
	// Precondition: the all-rows face really carries all eight rows, so the
	// sparse-length assertion below is not vacuous.
	if want := uint8(1<<altCastCount - 1); af.altCostMask != want {
		t.Fatalf("precondition: all-rows face mask=%#x, want %#x", af.altCostMask, want)
	}
	if len(af.altCosts) != int(altCastCount) {
		t.Errorf("all-rows face stores %d sidecar rows, want %d", len(af.altCosts), altCastCount)
	}
}
