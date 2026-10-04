package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// populatedCost builds a Cost with EVERY slice field non-empty and every
// element non-zero, so the completeness walk below can prove cloneCost
// re-allocates each one. A slice field added to Cost without an entry here
// fails the walk's emptiness check; a field added here without a line in
// cloneCost fails the walk's backing-array check.
func populatedCost() Cost {
	cp := func(n int32, spec string) CostPart { return CostPart{N: n, Spec: spec} }
	return Cost{
		Hybrid:          []ManaPair{{A: 1, B: 2}, {A: 3, B: 4}},
		Phyrexian:       []byte{1, 2},
		Twobrid:         []Twobrid{{Generic: 1, Col: 2}, {Generic: 3, Col: 4}},
		HybridPhyrexian: []HybridPhyrexian{{A: 1, B: 2}, {A: 3, B: 4}},
		Sac:             []CostPart{cp(1, "Sac"), cp(2, "Sac2")},
		Discard:         []CostPart{cp(1, "Discard"), cp(2, "Discard2")},
		SubCounter:      []CostPart{cp(1, "SubCounter"), cp(2, "SubCounter2")},
		AddCounter:      []CostPart{cp(1, "AddCounter"), cp(2, "AddCounter2")},
		Exile:           []CostPart{cp(1, "Exile"), cp(2, "Exile2")},
		ExileFromTop:    []CostPart{cp(1, "ExileFromTop"), cp(2, "ExileFromTop2")},
		Reveal:          []CostPart{cp(1, "Reveal"), cp(2, "Reveal2")},
		RevealOrChoose:  []CostPart{cp(1, "RevealOrChoose"), cp(2, "RevealOrChoose2")},
		RevealChosen:    []CostPart{cp(1, "RevealChosen"), cp(2, "RevealChosen2")},
		Behold:          []CostPart{cp(1, "Behold"), cp(2, "Behold2")},
		TapPermanent:    []CostPart{cp(1, "TapPermanent"), cp(2, "TapPermanent2")},
		UntapPermanent:  []CostPart{cp(1, "UntapPermanent"), cp(2, "UntapPermanent2")},
		Blight:          []CostPart{cp(1, "Blight"), cp(2, "Blight2")},
		Exert:           []CostPart{cp(1, "Exert"), cp(2, "Exert2")},
		Draw:            []CostPart{cp(1, "Draw"), cp(2, "Draw2")},
		Energy:          []CostPart{cp(1, "Energy"), cp(2, "Energy2")},
		LifeX:           []CostPart{cp(1, "LifeX"), cp(2, "LifeX2")},
		DamageYou:       []CostPart{cp(1, "DamageYou"), cp(2, "DamageYou2")},
		GainLife:        []CostPart{cp(1, "GainLife"), cp(2, "GainLife2")},
		Return:          []CostPart{cp(1, "Return"), cp(2, "Return2")},
		PutToLib:        []CostPart{cp(1, "PutToLib"), cp(2, "PutToLib2")},
		MoveToGrave:     []CostPart{cp(1, "MoveToGrave"), cp(2, "MoveToGrave2")},
		Mill:            []CostPart{cp(1, "Mill"), cp(2, "Mill2")},
		Evidence:        []CostPart{cp(1, "Evidence"), cp(2, "Evidence2")},
		RollDice:        []CostPart{cp(1, "RollDice"), cp(2, "RollDice2")},
		Withheld:        []string{"Withheld1", "Withheld2"},
		Unknown:         []string{"Unknown1", "Unknown2"},
	}
}

// TestCloneCostCoversEveryCostSlice walks Cost with reflect and proves that
// cloneCost re-allocates EVERY slice field: the clone's contents equal the
// original's, zeroing the clone's first element leaves the original intact,
// and appending to the clone does not grow the original. A slice field added
// to Cost fails this test automatically, either as an unpopulated field (the
// builder must learn it) or as an aliased backing array (cloneCost must
// learn it).
func TestCloneCostCoversEveryCostSlice(t *testing.T) {
	t.Parallel()
	orig := populatedCost()
	clone := cloneCost(orig)

	ov := reflect.ValueOf(&orig).Elem()
	cv := reflect.ValueOf(&clone).Elem()
	checked := 0
	for i := 0; i < ov.NumField(); i++ {
		f := ov.Field(i)
		if f.Kind() != reflect.Slice {
			continue
		}
		name := ov.Type().Field(i).Name
		cf := cv.Field(i)
		if f.Len() == 0 {
			t.Errorf("populatedCost left Cost slice field %s empty; extend the builder so the walk stays complete", name)
			continue
		}
		checked++
		if !reflect.DeepEqual(f.Interface(), cf.Interface()) {
			t.Errorf("cloneCost: field %s differs between original and clone", name)
			continue
		}
		want := f.Index(0).Interface()
		cf.Index(0).Set(reflect.Zero(f.Type().Elem()))
		if got := f.Index(0).Interface(); !reflect.DeepEqual(got, want) {
			t.Errorf("cloneCost does not own %s's backing array: zeroing the clone's element 0 changed the original (%+v -> %+v)", name, want, got)
		}
		before := f.Len()
		cv.Field(i).Set(reflect.Append(cv.Field(i), reflect.Zero(f.Type().Elem())))
		if f.Len() != before {
			t.Errorf("cloneCost does not own %s's backing array: appending to the clone grew the original (%d -> %d)", name, before, f.Len())
		}
	}
	if checked == 0 {
		t.Fatal("the walk checked no slice fields; Cost has no slices or the walk is broken")
	}
}

// TestUnlessPaymentCloneOwnsItsSlices pins the live-alias hazard of
// unlessPayment's Return/Exile heads: a clone of an engine carrying a
// populated unlessPayment (cap>len slices, exactly what
// recordUnlessPaymentPick's growth leaves behind) must own its own backing
// arrays for u.cost.Return, u.cost.Exile, u.returns and u.exiles, so
// appends through the clone and the original can never write each other's
// slot.
func TestUnlessPaymentCloneOwnsItsSlices(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 731)
	e.UnlessPayment = &unlessPayment{
		Payer: 0,
		Cost:  Cost{Return: make([]CostPart, 2, 4), Exile: make([]CostPart, 2, 4)},
	}
	e.UnlessPayment.Returns = make([]state.ObjID, 2, 4)
	e.UnlessPayment.Exiles = make([]state.ObjID, 2, 4)
	for i := range e.UnlessPayment.Cost.Return {
		e.UnlessPayment.Cost.Return[i] = CostPart{N: int32(i + 1), Spec: "Plains"}
		e.UnlessPayment.Cost.Exile[i] = CostPart{N: int32(i + 1), Spec: "Card"}
		e.UnlessPayment.Returns[i] = state.ObjID(i + 1)
		e.UnlessPayment.Exiles[i] = state.ObjID(i + 3)
	}
	ret0, ret1 := e.UnlessPayment.Returns[0], e.UnlessPayment.Returns[1]
	exl0, exl1 := e.UnlessPayment.Exiles[0], e.UnlessPayment.Exiles[1]
	costRet0, costRet1 := e.UnlessPayment.Cost.Return[0], e.UnlessPayment.Cost.Return[1]
	costExl0, costExl1 := e.UnlessPayment.Cost.Exile[0], e.UnlessPayment.Cost.Exile[1]
	if ret1 == 0 || costRet1.Spec == "" {
		t.Fatal("test precondition failed: populated unlessPayment slices are empty")
	}

	c := e.Clone()
	if c.UnlessPayment == nil {
		t.Fatal("clone lost unlessPayment entirely")
	}
	cp := c.UnlessPayment
	// Precondition: the clone carries the populated slices before any
	// mutation.
	if len(cp.Returns) != 2 || len(cp.Exiles) != 2 || len(cp.Cost.Return) != 2 || len(cp.Cost.Exile) != 2 {
		t.Fatalf("clone pick slices = %d/%d/%d/%d, want 2 each",
			len(cp.Returns), len(cp.Exiles), len(cp.Cost.Return), len(cp.Cost.Exile))
	}
	if cp.Returns[0] != ret0 || cp.Returns[1] != ret1 ||
		cp.Exiles[0] != exl0 || cp.Exiles[1] != exl1 ||
		cp.Cost.Return[0] != costRet0 || cp.Cost.Return[1] != costRet1 ||
		cp.Cost.Exile[0] != costExl0 || cp.Cost.Exile[1] != costExl1 {
		t.Fatalf("clone's populated slices differ from the original before any mutation")
	}

	// Advance BOTH engines the way two live continuations would: one pick
	// appended through the clone, one through the original, per slice.
	cp.Returns = append(cp.Returns, 9001)
	cp.Exiles = append(cp.Exiles, 9002)
	cp.Cost.Return = append(cp.Cost.Return, CostPart{N: 99, Spec: "cloneRet"})
	cp.Cost.Exile = append(cp.Cost.Exile, CostPart{N: 98, Spec: "cloneExl"})
	e.UnlessPayment.Returns = append(e.UnlessPayment.Returns, 5001)
	e.UnlessPayment.Exiles = append(e.UnlessPayment.Exiles, 5002)
	e.UnlessPayment.Cost.Return = append(e.UnlessPayment.Cost.Return, CostPart{N: 88, Spec: "origRet"})
	e.UnlessPayment.Cost.Exile = append(e.UnlessPayment.Cost.Exile, CostPart{N: 87, Spec: "origExl"})

	// The clone's own entries survive at their own indices, and the earlier
	// elements are untouched on both sides.
	if got := cp.Returns[2]; got != 9001 {
		t.Errorf("clone.returns[2] = %d, want 9001 (its own appended pick; a shared backing slot reads the original's 5001)", got)
	}
	if got := cp.Exiles[2]; got != 9002 {
		t.Errorf("clone.exiles[2] = %d, want 9002 (its own appended pick; a shared backing slot reads the original's 5002)", got)
	}
	if got := cp.Cost.Return[2]; got != (CostPart{N: 99, Spec: "cloneRet"}) {
		t.Errorf("clone.cost.Return[2] = %+v, want {99 cloneRet} (a shared backing slot reads the original's appended part)", got)
	}
	if got := cp.Cost.Exile[2]; got != (CostPart{N: 98, Spec: "cloneExl"}) {
		t.Errorf("clone.cost.Exile[2] = %+v, want {98 cloneExl} (a shared backing slot reads the original's appended part)", got)
	}
	if got := e.UnlessPayment.Returns[2]; got != 5001 {
		t.Errorf("orig.returns[2] = %d, want 5001 (its own appended pick; a shared backing slot reads the clone's 9001)", got)
	}
	if got := e.UnlessPayment.Exiles[2]; got != 5002 {
		t.Errorf("orig.exiles[2] = %d, want 5002 (its own appended pick)", got)
	}
	if got := e.UnlessPayment.Cost.Return[2]; got != (CostPart{N: 88, Spec: "origRet"}) {
		t.Errorf("orig.cost.Return[2] = %+v, want {88 origRet}", got)
	}
	if got := e.UnlessPayment.Cost.Exile[2]; got != (CostPart{N: 87, Spec: "origExl"}) {
		t.Errorf("orig.cost.Exile[2] = %+v, want {87 origExl}", got)
	}
	if e.UnlessPayment.Returns[0] != ret0 || e.UnlessPayment.Returns[1] != ret1 ||
		cp.Returns[0] != ret0 || cp.Returns[1] != ret1 {
		t.Errorf("earlier return picks were clobbered by the appends: orig=[%d %d] clone=[%d %d], want [%d %d] both",
			e.UnlessPayment.Returns[0], e.UnlessPayment.Returns[1], cp.Returns[0], cp.Returns[1], ret0, ret1)
	}
	if e.UnlessPayment.Exiles[0] != exl0 || e.UnlessPayment.Exiles[1] != exl1 ||
		cp.Exiles[0] != exl0 || cp.Exiles[1] != exl1 {
		t.Errorf("earlier exile picks were clobbered by the appends")
	}
	if e.UnlessPayment.Cost.Return[0] != costRet0 || e.UnlessPayment.Cost.Return[1] != costRet1 ||
		cp.Cost.Return[0] != costRet0 || cp.Cost.Return[1] != costRet1 ||
		e.UnlessPayment.Cost.Exile[0] != costExl0 || e.UnlessPayment.Cost.Exile[1] != costExl1 ||
		cp.Cost.Exile[0] != costExl0 || cp.Cost.Exile[1] != costExl1 {
		t.Errorf("earlier cost parts were clobbered by the appends")
	}
}

// TestUnlessPaymentCloneOwnsEveryPickSlice is the completeness walk over
// unlessPayment's pick slices: it enumerates EVERY []state.ObjID field the
// struct carries, populates them (cap>len, what recordUnlessPaymentPick's
// growth leaves behind), clones the engine, and proves each slice on the
// clone is value-equal and backing-array-independent. reflect cannot write
// the struct's unexported fields, so population and the mutation checks go
// through in-package field access; the enumeration is the ratchet -- a pick
// slice added to unlessPayment makes the enumerated set diverge from the
// populated set and fails here until the test learns it, and once learned a
// cloneWith that forgets it fails the backing-array check exactly the way
// the missing returns/exiles lines did.
func TestUnlessPaymentCloneOwnsEveryPickSlice(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 733)
	up := &unlessPayment{}
	up.Sacs = make([]state.ObjID, 2, 4)
	up.Discards = make([]state.ObjID, 2, 4)
	up.Reveals = make([]state.ObjID, 2, 4)
	up.Beholds = make([]state.ObjID, 2, 4)
	up.Returns = make([]state.ObjID, 2, 4)
	up.Exiles = make([]state.ObjID, 2, 4)
	for _, s := range [][]state.ObjID{up.Sacs, up.Discards, up.Reveals, up.Beholds, up.Returns, up.Exiles} {
		s[0], s[1] = 1, 2
	}
	e.UnlessPayment = up

	// Ratchet: enumerate every []state.ObjID field of unlessPayment and
	// require the populated set to match exactly, so a newly added pick
	// slice cannot slip past this test unpopulated.
	ut := reflect.TypeOf(unlessPayment{})
	objIDType := reflect.TypeOf(state.ObjID(0))
	enumerated := map[string]bool{}
	for i := 0; i < ut.NumField(); i++ {
		ft := ut.Field(i).Type
		if ft.Kind() == reflect.Slice && ft.Elem() == objIDType {
			enumerated[ut.Field(i).Name] = true
		}
	}
	populated := map[string]bool{"Sacs": true, "Discards": true, "Reveals": true,
		"Beholds": true, "Returns": true, "Exiles": true}
	for _, name := range []string{"Sacs", "Discards", "Reveals", "Beholds", "Returns", "Exiles"} {
		if !enumerated[name] {
			t.Errorf("the walk did not enumerate unlessPayment pick slice %q; its struct shape changed and the walk must follow", name)
		}
	}
	if len(enumerated) == 0 {
		t.Fatal("the walk found no []state.ObjID pick slices; unlessPayment's shape changed and the walk is vacuous")
	}
	for name := range enumerated {
		if !populated[name] {
			t.Errorf("unlessPayment gained pick slice %q: extend this test's population and checks so the new field is covered", name)
		}
	}

	c := e.Clone()
	if c.UnlessPayment == nil {
		t.Fatal("clone lost unlessPayment entirely")
	}
	checkPickSliceClone := func(name string, orig, clone *[]state.ObjID) {
		t.Helper()
		if !reflect.DeepEqual(*orig, *clone) {
			t.Errorf("clone pick slice %s differs from the original: %v vs %v", name, *orig, *clone)
			return
		}
		(*clone)[0] = 90
		if (*orig)[0] != 1 {
			t.Errorf("cloneWith does not own %s's backing array: writing through the clone changed the original's element 0 (%d)", name, (*orig)[0])
		}
		before := len(*orig)
		*clone = append(*clone, 91)
		*orig = append(*orig, 51)
		if len(*orig) != before+1 || len(*clone) != before+1 {
			t.Errorf("cloneWith does not own %s's backing array: appends leaked across the clone boundary (orig len %d, clone len %d, want %d each)", name, len(*orig), len(*clone), before+1)
			return
		}
		if got := (*clone)[before]; got != 91 {
			t.Errorf("clone %s[%d] = %d, want 91 (its own appended pick; a shared slot reads the original's 51)", name, before, got)
		}
		if got := (*orig)[before]; got != 51 {
			t.Errorf("original %s[%d] = %d, want 51 (its own appended pick; a shared slot reads the clone's 91)", name, before, got)
		}
	}
	checkPickSliceClone("sacs", &e.UnlessPayment.Sacs, &c.UnlessPayment.Sacs)
	checkPickSliceClone("discards", &e.UnlessPayment.Discards, &c.UnlessPayment.Discards)
	checkPickSliceClone("reveals", &e.UnlessPayment.Reveals, &c.UnlessPayment.Reveals)
	checkPickSliceClone("beholds", &e.UnlessPayment.Beholds, &c.UnlessPayment.Beholds)
	checkPickSliceClone("returns", &e.UnlessPayment.Returns, &c.UnlessPayment.Returns)
	checkPickSliceClone("exiles", &e.UnlessPayment.Exiles, &c.UnlessPayment.Exiles)
}
