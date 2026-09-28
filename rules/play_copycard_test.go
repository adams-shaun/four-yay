package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPlayCopyCardCastsCopyAndLeavesOriginalInExile pins the rules-side
// CopyCard cast transaction: the source card remains in exile while a
// distinct copy of its face is put on the stack and cast.
func TestPlayCopyCardCastsCopyAndLeavesOriginalInExile(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 44021,
		"Name:Play copy fixture\nManaCost:0\nTypes:Sorcery\nOracle:\n",
		"Name:Exiled spell\nManaCost:1 R\nTypes:Sorcery\nOracle:Deal 3 damage.\n")
	original := moveSeeded(t, e, 0, "Name:Exiled spell\nManaCost:1 R\nTypes:Sorcery\nOracle:Deal 3 damage.\n", state.ZExile)
	if o := e.G.Obj(original); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: original %d is not in exile: %+v", original, o)
	}
	originalFace := e.G.Obj(original).Face()
	if originalFace == nil || originalFace.Name != "Exiled spell" {
		t.Fatalf("precondition: source face = %+v", originalFace)
	}

	// Resolve an actual DB$ Play ability on a stack object. Its synthetic
	// ability body is deliberately the same parameter shape as the reported
	// corpus carriers; only the source card script is inline.
	source := moveSeeded(t, e, 0, "Name:Play copy fixture\nManaCost:0\nTypes:Sorcery\nOracle:\n", state.ZStack)
	e.G.Obj(source).Ability = &cards.SA{Kind: "DB", API: "Play", Params: map[string]string{
		"Valid": "Card", "ValidZone": "Exile", "CopyCard": "True", "WithoutManaCost": "True",
	}}
	before := len(e.L.Events)
	e.resolveTop()
	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KModes || ask.ResumeKind != "play" {
		t.Fatalf("DB$ Play ask = %+v; events=%+v", ask, e.L.Events[before:])
	}
	if len(ask.Options) != 1 || ask.Options[0].Obj != original {
		t.Fatalf("Play options = %+v, want original card %d", ask.Options, original)
	}
	submitChoices(t, e, ask.Options[0].Index)
	if e.G.Obj(original).Zone != state.ZExile {
		t.Fatalf("CopyCard moved original out of exile: %+v", e.G.Obj(original))
	}
	var copied state.ObjID
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.PutOnStack && ev.Obj != original {
			copied = ev.Obj
		}
	}
	if copied == 0 {
		t.Fatalf("no distinct copied spell was put on stack; stack=%v events=%+v", e.G.Stack, e.L.Events[before:])
	}
	copyObj := e.G.Obj(copied)
	if copyObj == nil || copyObj.Zone != state.ZStack || !copyObj.IsCopy {
		t.Fatalf("cast object %d = %+v, want copied spell on stack", copied, copyObj)
	}
	if copyObj.Owner != 0 || copyObj.Controller != 0 {
		t.Fatalf("copy owner/controller = %d/%d, want casting seat 0", copyObj.Owner, copyObj.Controller)
	}
	if copyObj.Face() == nil || copyObj.Face().Name != originalFace.Name || copyObj.Face().Cmc() != originalFace.Cmc() {
		t.Fatalf("copy face = %+v, original = %+v", copyObj.Face(), originalFace)
	}
	if copyObj.ID == original || copyObj.Face().Name == "" {
		t.Fatalf("copy identity/characteristics did not differ as expected: original=%d copy=%+v", original, copyObj)
	}

	// Resolve the copied spell too: resolving it must move only the copy to
	// its resting zone, never consume the original exile card.
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(original).Zone != state.ZExile {
		t.Fatalf("resolving copied spell moved original: %+v", e.G.Obj(original))
	}
	if got := e.G.Obj(copied); got == nil || got.Zone == state.ZStack {
		t.Fatalf("resolved copy = %+v, want it to leave the stack", got)
	}
}
