package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestDerivedMemoKeepsEmptyKeywordListBound pins the round-8 fuzz root cause
// (cardfuzz replay divergences, seeds 20814263898706950, 1777867497197181625
// and 2567947918426673293; paymirror random2 control pending.Options.len,
// seed 8090). matchesSpec binds Derived's keyword list as
// SpecContext.ExtraKeywords, and a NIL ExtraKeywords means "unbound: read the
// printed face". The Derived memo copied a computed EMPTY list into an entry
// that had never held a keyword as nil, so Surge Engine -- its printed
// Defender removed by its own Animate -- matched Card.Self+!withDefender (its
// second ability's IsPresent$) in an engine whose memo entry was first filled
// while it still had Defender, and failed it in a replay or a Clone whose
// first derive came after the removal: the two engines offered different
// priority option lists.
func TestDerivedMemoKeepsEmptyKeywordListBound(t *testing.T) {
	t.Parallel()
	const spec = "Card.Self+!withDefender"
	e := layerEngine(t)
	wall := onBoard(t, e, 0, "Name:Wall Engine\nManaCost:2\nTypes:Artifact Creature Construct\nPT:3/2\nK:Defender\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{Source: wall, Affects: "Card.Self", Layer: state.LAbilities,
		Timestamp: e.G.Clock + 1, RemoveKeywords: []string{"Defender"}})
	if e.HasKeyword(wall, "Defender") {
		t.Fatal("precondition: the removal must strip the printed Defender")
	}
	if n := e.countPresent(spec, wall, 0); n != 1 {
		t.Fatalf("unscoped %s = %d, want 1", spec, n)
	}
	count := func(eng *Engine) (int, []string) {
		eng.beginDerivedMemo()
		defer eng.endDerivedMemo()
		kw := eng.Derived(wall).Keywords
		return eng.countPresent(spec, wall, 0), kw
	}
	// The engine's memo entry for wall has never held a keyword: its first
	// derive is the empty post-removal list.
	if n, kw := count(e); n != 1 || kw == nil {
		t.Fatalf("memoized %s = %d (keywords %#v), want 1 with a bound empty list", spec, n, kw)
	}
	// A clone starts with an empty memo and must answer the same way.
	if n, kw := count(e.Clone()); n != 1 || kw == nil {
		t.Fatalf("clone's memoized %s = %d (keywords %#v), want 1 with a bound empty list", spec, n, kw)
	}
	// A cross-walk hit serves the stored entry: still bound.
	if n, kw := count(e); n != 1 || kw == nil {
		t.Fatalf("second walk's %s = %d (keywords %#v), want 1 with a bound empty list", spec, n, kw)
	}
}

// TestDerivedKeywordsBoundBeforeScratchGrows pins derivedCompute's half of
// the same contract: a faced object's derived keyword list is never nil, even
// in an engine whose scratch buffer has not grown yet (a fresh engine, a
// replay, a Clone). A face-down 2/2 has no keywords (CR 708.2), so its
// printed Flying must not answer a withFlying filter through the unbound
// printed-face fallback.
func TestDerivedKeywordsBoundBeforeScratchGrows(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Hidden Flyer\nManaCost:3 U\nTypes:Creature Bird\nPT:3/3\nK:Flying\nOracle:x\n")
	e.G.Obj(id).FaceDown = true
	for _, eng := range []*Engine{e.Clone(), e} {
		eng.derivedKW, eng.derivedTypes = nil, nil
		if kw := eng.Derived(id).Keywords; kw == nil || len(kw) != 0 {
			t.Fatalf("face-down derived keywords = %#v, want a bound empty list", kw)
		}
		eng.derivedKW, eng.derivedTypes = nil, nil
		if n := eng.countPresent("Creature.withFlying", id, 0); n != 0 {
			t.Fatalf("a face-down 2/2 matched withFlying through its printed face (%d)", n)
		}
	}
}
