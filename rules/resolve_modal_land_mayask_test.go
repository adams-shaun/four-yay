package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A modal land's back-face replacement is not visible to the current-face
// replacement walk before face selection, so its priority play must retain a
// checkpoint conservatively.
func TestModalLandMayAskKeepsBackFaceReplacementCheckpoint(t *testing.T) {
	t.Parallel()
	const script = "Name:Front Form\nManaCost:0\nTypes:Creature\nPT:1/1\nAlternateMode:Modal\nOracle:x\nALTERNATE\n" +
		"Name:Back Form\nTypes:Land\nR:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ TapIt | ReplacementResult$ Updated | Description$ enter tapped\n" +
		"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ TapIt | ReplacementResult$ Updated | Description$ enter tapped\n" +
		"SVar:TapIt:DB$ Tap | Defined$ Self\nOracle:x\n"
	modal := card(t, script)
	if len(modal.Faces) != 2 || len(modal.Faces[1].Repls) != 2 || cards.FaceEntryMayAsk(modal.Faces[1]) {
		t.Fatal("precondition: the selectable back face must have two individually non-electing Moved replacements")
	}
	e := corpusEngine(t, testutil.CorpusRegistry(t), []*cards.Card{modal}, nil)
	land := moveByName(t, e, 0, "Front Form", state.ZHand)
	o := e.G.Obj(land)
	if o == nil || o.Zone != state.ZHand || o.FaceIdx != 0 || !o.Card.Faces[1].IsLand() {
		t.Fatalf("precondition: modal land not in hand on its front face: %+v", o)
	}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "play_land", Obj: land, Mode: modalLandMode}}}
	in := decision.Intent{Player: 0, Choices: []int{0}}
	if !(*resolveBoard)(e).MayAsk(d, in) {
		t.Fatal("MayAsk = false although selecting the back face can ask through its entry replacement")
	}
}
