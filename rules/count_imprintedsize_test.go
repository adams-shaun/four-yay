// Count$ImprintedSize — the source-object imprint count head (ticket
// cli-20261005T092134Z-fed167ca, part 1). Three corpus faces carry it: TDM
// Unexpected Conversion and Grizzled Huntmaster write `SVar:Y:Count$ImprintedSize`
// directly, and DSK Oblivious Bookworm writes `SVar:Condition:Count$ImprintedSize/Plus.Y`.
// Forge reads c.getImprintedCards().size() -- the cards Imprint$/ImprintCards$
// associated with the ability's host -- which this engine records on
// state.Object.Imprinted through the events.Imprint fold. Before the head
// existed the bodies degraded to the unresolvable zero.
//
// The pin verifies each real carrier body is still present, then stamps
// imprints through the same event fold the primitives use and asserts the
// head moves with them.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestImprintedSizeHeadOnCorpusCarriers pins Count$ImprintedSize against the
// real SVar bodies that carry it. Each fixture stamps imprints through the
// real events.Imprint fold and asserts the head moves, so a hardcoded or
// vacuous answer cannot pass.
func TestImprintedSizeHeadOnCorpusCarriers(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	reg := testutil.CorpusRegistry(t)

	// The three face bodies that carry the head. Each is verified against
	// the corpus rather than assumed, so a rename fails this test.
	type carrier struct {
		name string
		svar string
		body string
	}
	carriers := []carrier{
		{"Unexpected Conversion", "Y", "Count$ImprintedSize"},
		{"Grizzled Huntmaster", "Y", "Count$ImprintedSize"},
		{"Oblivious Bookworm", "Condition", "Count$ImprintedSize/Plus.Y"},
	}
	seen := 0
	for _, c := range carriers {
		card, ok := reg.Lookup(c.name)
		if !ok {
			t.Fatalf("corpus fixture %q is missing", c.name)
		}
		var body string
		var found bool
		for fi := range card.Faces {
			if b, ok := card.Faces[fi].SVars[c.svar]; ok {
				body, found = b, true
				break
			}
		}
		if !found || body != c.body {
			t.Fatalf("test precondition: %s SVar %s = %q (found %v), want %q", c.name, c.svar, body, found, c.body)
		}
		seen++
	}
	if seen != 3 {
		t.Fatalf("precondition: checked %d carriers, want 3", seen)
	}

	// A concrete source with three imprints. Unexpected Conversion's own
	// body is a clean `Count$ImprintedSize` (no /Op), so it is the eval pin.
	src := onBoardCard(t, e, 0, corpusCard(t, "Unexpected Conversion"))
	body := e.G.Obj(src).Face().SVars["Y"]
	ctx := &effects.Ctx{Controller: 0, Source: src, SVars: e.G.Obj(src).Face().SVars}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("baseline ImprintedSize = %d (ok %v), want evaluated 0", n, ok)
	}
	// Imprint two real cards through the SAME event fold Imprint$ True uses.
	imprinted := []state.ObjID{
		onBoardCard(t, e, 0, corpusCard(t, "Island")),
		onBoardCard(t, e, 0, corpusCard(t, "Mountain")),
	}
	e.emit(events.Event{Kind: events.Imprint, Obj: src, IDs: imprinted})
	if got := len(e.G.Obj(src).Imprinted); got != 2 {
		t.Fatalf("precondition: source Imprinted list has %d entries, want 2 (the event did not fold)", got)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("ImprintedSize after two imprints = %d (ok %v), want 2", n, ok)
	}

	// The /Plus.<SVar> carrier resolves through the same base read: Oblivious
	// Bookworm's Condition body with the other operand Y = 0 answers 2.
	bw := onBoardCard(t, e, 0, corpusCard(t, "Oblivious Bookworm"))
	bwFace := e.G.Obj(bw).Face()
	bwIDs := []state.ObjID{onBoardCard(t, e, 0, corpusCard(t, "Plains"))}
	e.emit(events.Event{Kind: events.Imprint, Obj: bw, IDs: bwIDs})
	bwCtx := &effects.Ctx{Controller: 0, Source: bw, SVars: bwFace.SVars}
	if n, ok := effects.EvalCountOK(e, bwCtx, "Count$ImprintedSize/Plus.Y"); !ok || n != 1 {
		t.Fatalf("Oblivious Bookworm Condition = %d (ok %v), want 1", n, ok)
	}
	// A different number of imprints (three) proves it is not a hardcoded one.
	e.emit(events.Event{Kind: events.Imprint, Obj: bw, IDs: []state.ObjID{
		onBoardCard(t, e, 0, corpusCard(t, "Island")),
		onBoardCard(t, e, 0, corpusCard(t, "Mountain")),
	}})
	if n, ok := effects.EvalCountOK(e, bwCtx, "Count$ImprintedSize/Plus.Y"); !ok || n != 3 {
		t.Fatalf("Oblivious Bookworm Condition after three imprints = %d (ok %v), want 3", n, ok)
	}
}
