package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ChangeType$ "EACH <A> & <B>" (Forge's multi-type search grammar) used to
// reach the filter matcher as ONE alternative whose whole string was the
// base, so every carrier's eligible set was empty and Krosan Verge never
// fetched anything. These leaves pin the union matcher (any listed sub-spec
// matches), the truthful census, and the hidden-library search's per-type
// pick structure (one Option.Group per listed type).

// eachTestGame is a two-seat game with the matching-layer fixture cards
// already parsed.
func eachTestGame(t *testing.T) (*state.Game, *cards.Card, *cards.Card, *cards.Card) {
	t.Helper()
	forest := mkCard(t, "Name:F\nTypes:Land Forest\nOracle:x\n")
	plains := mkCard(t, "Name:P\nTypes:Land Plains\nOracle:x\n")
	island := mkCard(t, "Name:I\nTypes:Land Island\nOracle:x\n")
	return state.NewGame(names(2)), forest, plains, island
}

// TestEachChangeTypeMatchesAnyListedType is the union matcher: an EACH spec
// matches a candidate when ANY listed sub-spec matches it, never the whole
// string as one (never-matching) base.
func TestEachChangeTypeMatchesAnyListedType(t *testing.T) {
	g, forest, plains, island := eachTestGame(t)
	sc := SpecContext{You: 0}
	for _, tc := range []struct {
		spec string
		o    *cards.Card
		want bool
	}{
		{"EACH Forest & Plains", forest, true},
		{"EACH Forest & Plains", plains, true},
		{"EACH Forest & Plains", island, false},
		// Forge's spelling keeps sub-spec predicates intact.
		{"EACH Forest.Basic & Plains", forest, false}, // F is not Basic in this fixture
		{"EACH Creature & Land", forest, true},
		{"Forest", forest, true}, // sanity: the ordinary path still works
	} {
		got := MatchesObjectCtx(g, tc.spec, g.AddObject(tc.o, 0), sc)
		if got != tc.want {
			t.Fatalf("MatchesObjectCtx(%q, %s) = %v, want %v", tc.spec, tc.o.Faces[0].Name, got, tc.want)
		}
	}
}

// TestEachDottedSubSpecsEvaluateTheirPredicates covers the dotted form: each
// '&' part is an ordinary spec whose own predicates (YouOwn, IsRemembered)
// are evaluated, not one base cut at the first dot with the rest of the
// string as garbage predicate tokens.
func TestEachDottedSubSpecsEvaluateTheirPredicates(t *testing.T) {
	g, _, _, _ := eachTestGame(t)
	saga := mkCard(t, "Name:S\nTypes:Saga\nOracle:x\n")
	sc := SpecContext{You: 0, Remembered: []state.Target{}}
	sagaObj := g.AddObject(saga, 0)
	landObj := g.AddObject(mkCard(t, "Name:L\nTypes:Land\nOracle:x\n"), 0)
	spec := "EACH Saga.YouOwn+IsRemembered & Land.YouOwn+IsRemembered"
	if MatchesObjectCtx(g, spec, sagaObj, sc) {
		t.Fatal("unremembered saga matched an IsRemembered sub-spec")
	}
	sc.Remembered = []state.Target{{Obj: sagaObj.ID}, {Obj: landObj.ID}}
	if !MatchesObjectCtx(g, spec, sagaObj, sc) {
		t.Fatal("remembered saga did not match its sub-spec")
	}
	if !MatchesObjectCtx(g, spec, landObj, sc) {
		t.Fatal("remembered land did not match its sub-spec")
	}
	mountain := g.AddObject(mkCard(t, "Name:M\nTypes:Land Mountain\nOracle:x\n"), 0)
	if MatchesObjectCtx(g, spec, mountain, sc) {
		t.Fatal("unremembered mountain matched")
	}
}

// TestEachChangeTypeCensusIsTruthful pins the census reads: the dotted form
// no longer leaks the '&' join and the later clauses as garbage predicate
// tokens, the bare form's unknowns are the sub-specs', and the quality
// classification evaluates the sub-specs (a quantity-only EACH stays
// quantity-only).
func TestEachChangeTypeCensusIsTruthful(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want []string
	}{
		{"EACH Forest & Plains", nil},
		{"EACH Saga.YouOwn+IsRemembered & Land.YouOwn+IsRemembered", nil},
		{"EACH Land.FooBar & Forest", []string{"FooBar"}},
	} {
		got := UnknownPredicates(tc.spec)
		if len(got) != len(tc.want) {
			t.Fatalf("UnknownPredicates(%q) = %v, want %v", tc.spec, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("UnknownPredicates(%q) = %v, want %v", tc.spec, got, tc.want)
			}
		}
	}
	for _, tc := range []struct {
		spec string
		want bool
	}{
		{"EACH Forest & Plains", true},
		{"EACH Card.White & Card.Blue & Card.Black & Card.Red & Card.Green", true},
		{"EACH Card.YouOwn & Card.YouCtrl", false},
	} {
		if got := SearchStatesQuality(tc.spec); got != tc.want {
			t.Fatalf("SearchStatesQuality(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}

// eachVergeAbility returns Krosan Verge's compiled ChangeZone activated
// ability from the corpus.
func eachVergeAbility(t *testing.T) (*cards.Registry, *cards.Card, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("no corpus")
	}
	verge, ok := reg.Lookup("Krosan Verge")
	if !ok || len(verge.Faces) == 0 {
		t.Fatal("Krosan Verge is absent from the corpus")
	}
	for _, a := range verge.Faces[0].Abilities {
		if a.Kind == "AB" && a.API == "ChangeZone" {
			return reg, verge, a
		}
	}
	t.Fatal("Krosan Verge has no compiled ChangeZone ability")
	return nil, nil, nil
}

// fillLibEach appends the cards to p's library (fillLibrary SETS the zone,
// so consecutive fills would overwrite each other) and returns the ids in
// library order.
func fillLibEach(g *state.Game, p state.PlayerID, cs ...*cards.Card) []state.ObjID {
	lib := append([]state.ObjID(nil), g.Zone(state.ZLibrary, p)...)
	var added []state.ObjID
	for _, c := range cs {
		added = append(added, g.AddObject(c, p).ID)
	}
	g.SetZone(state.ZLibrary, p, append(lib, added...))
	return added
}

// TestEachSearchAllTypesEmptyFailsToFindSilently keeps the no-ask
// fail-to-find: a library with neither a Forest nor a Plains asks nothing,
// still shuffles, and moves nothing.
func TestEachSearchAllTypesEmptyFailsToFindSilently(t *testing.T) {
	reg, verge, ab := eachVergeAbility(t)
	h := newHost(t, 2)
	src := h.g.AddObject(verge, 0)
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("Mountain absent from the corpus")
	}
	fillLibEach(h.g, 0, mountain, mountain, mountain)
	sh := &suspendHost{fakeHost: *h}
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, ab)
	if sh.asked != nil {
		t.Fatalf("posed %+v; an all-types-empty EACH search must fail to find silently", sh.asked)
	}
	var moves, shuffles, notes int
	for _, ev := range sh.log {
		switch ev.Kind {
		case events.MoveZone:
			moves++
		case events.Shuffle:
			shuffles++
		case events.Note:
			notes++
		}
	}
	if moves != 0 || shuffles != 1 || notes != 0 {
		t.Fatalf("moves=%d shuffles=%d notes=%d, want 0/1/0: %v", moves, shuffles, notes, sh.log)
	}
}
