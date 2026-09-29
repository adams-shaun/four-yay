package manabrew

import (
	"strings"
	"testing"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// stubText is the fixed CardText seam the scoping spec's Q9 calls for: a
// deterministic lookup that records the names it was asked for, so a test can
// prove a hidden card was never looked up.
type stubText struct {
	text  map[string]string
	asked []string
}

func (s *stubText) Text(name string) (string, bool) {
	s.asked = append(s.asked, name)
	t, ok := s.text[name]
	return t, ok
}

// TestCardTextSeamPopulatesVisibleOnly proves the CardText dependency declared
// on Translator is actually consulted by the state projection, and that it is
// only ever consulted with a name the seat's redacted view already shows.
func TestCardTextSeamPopulatesVisibleOnly(t *testing.T) {
	stub := &stubText{text: map[string]string{"Island": "({T}: Add {U}.)", "Revealed top": "Look at the top card."}}
	vv := projectionFixture()

	// Precondition: the fixture must actually carry a visible, named card
	// (Island) and a face-down card whose view name is empty, or the
	// assertions below would pass vacuously.
	var sawNamed, sawFaceDown bool
	for _, p := range vv.Players {
		for _, c := range p.Hand {
			if c.Printing.Name == "Island" && !c.FaceDown {
				sawNamed = true
			}
		}
		for _, c := range p.Battlefield {
			if c.FaceDown {
				sawFaceDown = true
			}
		}
	}
	if !sawNamed || !sawFaceDown {
		t.Fatalf("fixture precondition failed: named=%v faceDown=%v", sawNamed, sawFaceDown)
	}

	dto := New("table", 2, stub).gameView(vv)

	var islandText, faceDownText *string
	for _, z := range dto.Zones {
		for _, cv := range z.Cards {
			vc, ok := cv.Value.(mb.VisibleCard)
			if !ok {
				continue
			}
			switch vc.CardDto.Identity.Name {
			case "Island":
				islandText = &vc.CardDto.Text
			case "":
				faceDownText = &vc.CardDto.Text
			}
		}
	}
	if islandText == nil {
		t.Fatal("visible Island card missing from projection")
	}
	if faceDownText == nil {
		t.Fatal("face-down permanent not projected as a VisibleCard")
	}
	if *islandText == "" {
		t.Fatalf("CardText seam not consulted: visible card Text=%q, want the stub text", *islandText)
	}
	if *faceDownText != "" {
		t.Fatalf("face-down card leaked text %q", *faceDownText)
	}
	// The seam may only be consulted with a name that appears on a card the
	// seat's redacted view actually exposes, and never with the empty name a
	// face-down card carries. Derive the allowed set from the fixture rather
	// than hardcoding it, so a new visible card cannot make this pass
	// vacuously.
	allowed := map[string]bool{}
	faceDownCount := 0
	for _, p := range vv.Players {
		for _, list := range [][]view.CardView{p.Hand, p.Battlefield, p.Graveyard, p.Exile, p.Command} {
			for _, c := range list {
				if c.FaceDown {
					faceDownCount++
					continue
				}
				allowed[c.Printing.Name] = true
			}
		}
		if p.LibraryTop != nil && !p.LibraryTop.FaceDown {
			allowed[p.LibraryTop.Printing.Name] = true
		}
	}
	if faceDownCount == 0 {
		t.Fatal("fixture has no face-down card: hidden-card assertion would be vacuous")
	}
	for _, name := range stub.asked {
		if strings.TrimSpace(name) == "" {
			t.Fatal("CardText was asked for the empty face-down name")
		}
		if !allowed[name] {
			t.Fatalf("CardText consulted with name %q that the view does not expose", name)
		}
	}
}
