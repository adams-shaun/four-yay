package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestExtraKeywordsOnlyMatchesItsOwner(t *testing.T) {
	g, ids := board(t)
	owner := g.Obj(ids["myBear"])
	if owner == nil || owner.Zone != state.ZBattlefield {
		t.Fatal("precondition: owner must be on the battlefield")
	}
	card, diags := cards.ParseBytes("t.txt", []byte("Name:Reach Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Reach\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	card.Link()
	for _, face := range card.Faces {
		face.ApplyIntrinsics()
	}
	other := g.AddObject(card, 0)
	other.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), other.ID))
	if other.Zone != state.ZBattlefield || owner.Zone != state.ZBattlefield {
		t.Fatal("precondition: both objects must be on the battlefield")
	}
	if owner.Face().HasKeyword("Flying") || other.Face().HasKeyword("Flying") || !other.Face().HasKeyword("Reach") {
		t.Fatal("precondition: owner and other must not print Flying, and other must print Reach")
	}
	if owner.Face().HasKeyword("Reach") == other.Face().HasKeyword("Reach") {
		t.Fatal("precondition: printed keyword sets must differ")
	}

	sc := SpecContext{You: 0, ExtraKeywords: []string{"Flying", "Double Strike", "Affinity"}, ExtraKeywordsOwner: owner.ID}
	programs := CompilePredicatePrograms([]string{"Creature.withFlying", "Creature.withReach", "Affinity"})
	compiledCtx := sc
	compiledCtx.PredicatePrograms = programs
	for _, path := range []struct {
		name  string
		match func(string, *state.Object) bool
	}{
		{"textual", func(spec string, o *state.Object) bool { return MatchesSpecCtx(g, spec, o.ID, sc) }},
		{"compiled", func(spec string, o *state.Object) bool { return MatchesSpecCtx(g, spec, o.ID, compiledCtx) }},
	} {
		if !path.match("Creature.withFlying", owner) {
			t.Errorf("%s: owner does not match its bound Flying keyword", path.name)
		}
		if path.match("Creature.withFlying", other) {
			t.Errorf("%s: non-owner inherited the owner's Flying keyword", path.name)
		}
		if !path.match("Creature.withReach", other) {
			t.Errorf("%s: non-owner lost its printed Reach keyword", path.name)
		}
		if !path.match("Affinity", owner) {
			t.Errorf("%s: owner does not match its bound Affinity keyword", path.name)
		}
		if path.match("Affinity", other) {
			t.Errorf("%s: non-owner inherited the owner's Affinity keyword", path.name)
		}
	}

	// A relational keyword qualifier reuses the candidate's context while
	// inspecting another object; that object must not inherit the candidate list.
	if keywordInCtx(other, "Flying", &sc) {
		t.Fatal("non-owner bearer inherited the candidate's Flying keyword")
	}

	// The board-wide published table remains the fallback for non-owners.
	sc.Layers.DerivedKeywords = []ObjectKeywords{{ID: other.ID, Keywords: []string{"Flying"}}}
	if !MatchesSpecCtx(g, "Creature.withFlying", other.ID, sc) {
		t.Fatal("non-owner did not read Flying from the published derived-keyword table")
	}
	if !MatchesSpecCtx(g, "Creature.withFlying", owner.ID, sc) {
		t.Fatal("owner's ExtraKeywords ceased to be authoritative")
	}
}
