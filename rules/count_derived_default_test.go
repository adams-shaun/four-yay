package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// TestCountOutsideResolveReadsDerivedTypes pins W1d's "derived by default"
// count read. A count evaluated outside effects.Resolve -- here a static's
// numeric-RHS SVar resolved through specCtx, the shape a Chalice-style
// `cmcEQX` restriction reads -- used to run on a Ctx with no layer tables, so
// its Count$Valid matched the PRINTED type line while the same count inside a
// resolution matched the layer-4 derived one: a land a continuous effect made
// a creature was counted at resolution and not at the static's check.
func TestCountOutsideResolveReadsDerivedTypes(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Counter Source\nTypes:Artifact\n"+
		"SVar:X:Count$Valid Creature.YouCtrl\nOracle:x\n")
	onBoard(t, e, 0, "Name:Plain Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	land := onBoard(t, e, 0, "Name:Animated Land\nTypes:Land\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: land, Timestamp: 1, Layer: LType,
		Affects: "Card.Self", AddTypes: []string{"Creature"}})
	if !e.IsCreature(land) {
		t.Fatal("precondition: the layer-4 effect did not make the land a creature")
	}

	// The resolution-time read: a Ctx carrying the published tables.
	resolving := effects.NewCtxPtr(src, 0, effects.CtxInit{SVars: e.G.Obj(src).Face().SVars})
	resolving.Layers = e.boardLayers()
	want := effects.EvalCount(e, resolving, "SVar$X")
	if want != 2 {
		t.Fatalf("precondition: resolution-time Count$Valid Creature.YouCtrl = %d, want 2 (the bear and the animated land)", want)
	}

	// The static's numeric-RHS read: specCtx's resolver builds its own count
	// Ctx with no tables.
	sc := e.specCtx(src, 0)
	got, ok := sc.Resolve("X")
	if !ok || got != want {
		t.Fatalf("static numeric-RHS X = %d (ok=%v), want the resolution-time %d", got, ok, want)
	}
	// And a bare table-less Ctx (a cost or target-offer count) agrees too.
	if bare := effects.EvalCount(e, effects.NewCtxPtr(src, 0, effects.CtxInit{SVars: e.G.Obj(src).Face().SVars}), "SVar$X"); bare != want {
		t.Fatalf("table-less Count$Valid = %d, want %d", bare, want)
	}
}
