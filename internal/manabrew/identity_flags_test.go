//go:build manabrew

package manabrew

import (
	"encoding/json"
	"testing"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// TestIdentityFlagsRideTheWire pins MBX-1: CardIdentity.IsToken comes from
// the view's projected state.Object flag, and CardDto.IsCopy with it. Before
// the view carried the flags, the translator derived isToken from
// view.CardView.Token -- the "#<id>" display tag EVERY card carries -- so
// every card went out on the ManaBrew wire marked isToken=true.
func TestIdentityFlagsRideTheWire(t *testing.T) {
	v := view.View{Viewer: 0, Turn: 1, Step: "main1", Active: 0, Priority: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, HandSize: 0, LibrarySize: 30, Battlefield: []view.CardView{
			{ID: 2, Name: "Plain", Printing: view.Printing{Name: "Plain"}, Types: "Creature — Bear", Owner: 0, Controller: 0},
			{ID: 3, Name: "Soldier", Printing: view.Printing{Name: "Soldier"}, Types: "Creature — Soldier", Owner: 0, Controller: 0, IsToken: true},
			{ID: 4, Name: "Myriad", Printing: view.Printing{Name: "Myriad"}, Types: "Creature — Bear", Owner: 0, Controller: 0, IsToken: true, IsCopy: true},
		}},
		{ID: 1, Name: "Bob", Life: 20, HandSize: 0, LibrarySize: 30},
	}}
	// Precondition: the fixture cards really do carry the three flag shapes.
	if v.Players[0].Battlefield[0].IsToken || v.Players[0].Battlefield[0].IsCopy {
		t.Fatal("fixture plain card must carry neither flag")
	}
	if !v.Players[0].Battlefield[1].IsToken || v.Players[0].Battlefield[1].IsCopy {
		t.Fatal("fixture token must carry IsToken only")
	}
	if !v.Players[0].Battlefield[2].IsToken || !v.Players[0].Battlefield[2].IsCopy {
		t.Fatal("fixture token copy must carry both flags")
	}

	got, err := json.Marshal(New("table", 2, nil).gameView(v))
	if err != nil {
		t.Fatal(err)
	}
	var dto mb.GameViewDto
	if err := json.Unmarshal(got, &dto); err != nil {
		t.Fatal(err)
	}
	byID := map[string]mb.CardDto{}
	for _, z := range dto.Zones {
		for _, entry := range z.Cards {
			if vc, ok := entry.Value.(mb.VisibleCard); ok {
				byID[vc.ID] = vc.CardDto
			}
		}
	}
	if len(byID) != 3 {
		t.Fatalf("projected %d battlefield cards, want 3: %#v", len(byID), byID)
	}
	if byID["o2"].Identity.IsToken || byID["o2"].IsCopy {
		t.Fatalf("nontoken card o2 on the wire: identity.IsToken=%v IsCopy=%v, want false/false",
			byID["o2"].Identity.IsToken, byID["o2"].IsCopy)
	}
	if !byID["o3"].Identity.IsToken || byID["o3"].IsCopy {
		t.Fatalf("token o3 on the wire: identity.IsToken=%v IsCopy=%v, want true/false",
			byID["o3"].Identity.IsToken, byID["o3"].IsCopy)
	}
	if !byID["o4"].Identity.IsToken || !byID["o4"].IsCopy {
		t.Fatalf("token copy o4 on the wire: identity.IsToken=%v IsCopy=%v, want true/true",
			byID["o4"].Identity.IsToken, byID["o4"].IsCopy)
	}
}

// TestFaceDownIdentityStaysHidden pins the translator half of the same
// guard: a face-down BATTLEFIELD card is projected visible with a zeroed
// identity (the contract TestStateProjectionGolden pins: public state,
// redacted identity), and neither token nor copy flags may survive the
// face-down projection, even if a malformed/intermediate view carries them.
func TestFaceDownIdentityStaysHidden(t *testing.T) {
	v := view.View{Viewer: 0, Turn: 1, Step: "main1", Active: 0, Priority: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, HandSize: 0, LibrarySize: 30, Battlefield: []view.CardView{
			// A malformed/intermediate view can carry flags despite FaceDown;
			// the translator must not resurrect either one onto the wire.
			{ID: 5, FaceDown: true, Token: "#5", Owner: 0, Controller: 0, IsToken: true, IsCopy: true},
		}},
		{ID: 1, Name: "Bob", Life: 20, HandSize: 0, LibrarySize: 30},
	}}
	fixture := v.Players[0].Battlefield[0]
	if !fixture.FaceDown || !fixture.IsToken || !fixture.IsCopy {
		t.Fatalf("precondition: face-down fixture flags = facedown:%v token:%v copy:%v", fixture.FaceDown, fixture.IsToken, fixture.IsCopy)
	}
	got, err := json.Marshal(New("table", 2, nil).gameView(v))
	if err != nil {
		t.Fatal(err)
	}
	var dto mb.GameViewDto
	if err := json.Unmarshal(got, &dto); err != nil {
		t.Fatal(err)
	}
	var faceDown *mb.CardDto
	for _, z := range dto.Zones {
		for _, entry := range z.Cards {
			if vc, ok := entry.Value.(mb.VisibleCard); ok && vc.ID == "o5" {
				faceDown = &vc.CardDto
			}
		}
	}
	if faceDown == nil {
		t.Fatal("face-down battlefield card not projected")
	}
	if !faceDown.IsFaceDown || faceDown.Identity != (mb.CardIdentity{}) {
		t.Fatalf("face-down card projected with identity %#v IsFaceDown=%v, want zeroed identity", faceDown.Identity, faceDown.IsFaceDown)
	}
	if faceDown.Identity.IsToken || faceDown.IsCopy {
		t.Fatalf("face-down card leaked token/copy flags: IsToken=%v IsCopy=%v", faceDown.Identity.IsToken, faceDown.IsCopy)
	}
}
