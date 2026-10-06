package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestExtraTypesOnlyMatchesItsOwner(t *testing.T) {
	g, ids := board(t)
	owner, other := g.Obj(ids["myLand"]), g.Obj(ids["myArtifact"])
	if owner == nil || owner.Zone != state.ZBattlefield || other == nil || other.Zone != state.ZBattlefield {
		t.Fatal("precondition: owner and other must both be on the battlefield")
	}
	if hasType(owner, "Creature") || hasType(other, "Creature") || hasType(other, "Goblin") || !hasType(other, "Artifact") {
		t.Fatal("precondition: printed types must be Land for owner and Artifact only for other")
	}

	sc := SpecContext{
		You:             0,
		ExtraTypes:      []string{"Creature", "Goblin", "Land"},
		ExtraTypesOwner: owner.ID,
	}
	ps := CompilePredicatePrograms([]string{
		"Creature", "Creature.Goblin", "Creature.YouCtrl", "Artifact",
	})
	for _, path := range []struct {
		name  string
		match func(spec string, obj *state.Object) bool
	}{
		{"textual", func(spec string, obj *state.Object) bool { return MatchesSpecCtx(g, spec, obj.ID, sc) }},
		{"compiled", func(spec string, obj *state.Object) bool { return ps.Evaluate(spec, g, obj, sc) == PredicateYes }},
	} {
		for _, spec := range []string{"Creature.Goblin", "Creature.YouCtrl"} {
			if !path.match(spec, owner) {
				t.Errorf("%s: owner does not match %s", path.name, spec)
			}
		}
		for _, spec := range []string{"Creature", "Creature.Goblin"} {
			if path.match(spec, other) {
				t.Errorf("%s: non-owner matches %s from owner's ExtraTypes", path.name, spec)
			}
		}
		if !path.match("Artifact", other) {
			t.Errorf("%s: non-owner does not match printed Artifact", path.name)
		}
	}
}
