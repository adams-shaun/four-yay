package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestDefinedTargetedKeepsChainUnion pins the generic Defined$ Targeted
// contract even while a sub-ability is answering its own target ask. The
// local pick and the root target are deliberately distinct so replacing the
// chain union with PickedTargets cannot pass.
func TestDefinedTargetedKeepsChainUnion(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	root := state.Target{Obj: ids["myBear"]}
	sub := state.Target{Obj: ids["theirBig"]}
	if root.Obj == sub.Obj || g.Obj(root.Obj) == nil || g.Obj(sub.Obj) == nil {
		t.Fatal("precondition: root and sub targets must be distinct live objects")
	}
	ctx := &Ctx{
		Source:        ids["myBear"],
		Controller:    0,
		Targets:       []state.Target{root},
		AllTargets:    []state.Target{root, sub},
		PickedTargets: []state.Target{sub},
	}

	got := DefinedSpec(h, ctx, "Targeted")
	if len(got) != 2 || got[0] != root || got[1] != sub {
		t.Fatalf("ordinary Defined$ Targeted = %+v, want root/sub chain union [%+v %+v]", got, root, sub)
	}
}
