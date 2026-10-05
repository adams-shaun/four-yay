package effects

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type greenSunRandomHost struct {
	*digMultipleHost
	randCalls int
}

func (h *greenSunRandomHost) Rand(n int) int {
	h.randCalls++
	return 0
}

func TestDigMultipleGreenSunSelectionAndBottoming(t *testing.T) {
	_, ability, vars := corpusRiderSA(t, "Green Sun's Twilight", "")
	if ability.API != "DigMultiple" || ability.Params["ChangeLater"] != "True" || ability.Params["ImprintRest"] != "True" {
		t.Fatalf("precondition: Green Sun params %+v", ability)
	}
	restBottom := cards.ResolveSVar(vars, "RestBottom")
	bottomParams := ChangeZoneOf(restBottom)
	if !bottomParams.NoShuffle || !strings.HasPrefix(bottomParams.LibraryPositionText, "-") {
		t.Fatalf("precondition: RestBottom params noShuffle=%t position=%q", bottomParams.NoShuffle, bottomParams.LibraryPositionText)
	}
	base, firstFour := digMultipleBoard(t)
	fifth := base.g.AddObject(mkCard(t, "Name:UntouchedTop\nTypes:Artifact\nOracle:x\n"), 0).ID
	ids := append(append([]state.ObjID(nil), firstFour...), fifth)
	base.g.SetZone(state.ZLibrary, 0, ids)
	if !reflect.DeepEqual(base.g.Zone(state.ZLibrary, 0), []state.ObjID{firstFour[0], firstFour[1], firstFour[2], firstFour[3], fifth}) {
		t.Fatal("precondition: library is four-card Dig window followed by untouched fifth card")
	}
	selected := []state.ObjID{firstFour[1], firstFour[2]}
	unselected := []state.ObjID{firstFour[0], firstFour[3]}
	if reflect.DeepEqual(selected, unselected) || len(selected) != 2 || len(unselected) != 2 {
		t.Fatalf("precondition: selected %v and rest %v are distinct two-card sets", selected, unselected)
	}
	h := &greenSunRandomHost{digMultipleHost: base}
	h.choices = []int{1, 2}
	source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	svars := make(map[string]string, len(vars)+1)
	for k, v := range vars {
		svars[k] = v
	}
	svars["X"] = "Number$3"
	Resolve(h, &Ctx{Controller: 0, Source: source, SVars: svars}, ability)

	if !reflect.DeepEqual(h.g.Zone(state.ZHand, 0), selected) {
		t.Fatalf("selected cards in hand %v, want %v", h.g.Zone(state.ZHand, 0), selected)
	}
	library := h.g.Zone(state.ZLibrary, 0)
	if len(library) != 3 || library[0] != fifth {
		t.Fatalf("untouched fifth card must remain above bottom pile: library %v ids=%v firstFour=%v fifth=%v log=%+v", library, ids, firstFour, fifth, h.log)
	}
	for _, id := range library[1:] {
		if id != unselected[0] && id != unselected[1] {
			t.Fatalf("unexpected card in randomized bottom pile: library %v", library)
		}
	}
	if library[1] == library[2] {
		t.Fatalf("bottom pile does not contain two distinct unselected window cards: %v", library)
	}
	if h.randCalls != 1 {
		t.Fatalf("precondition: RandomOrder must randomize the two-card bottom pile (Rand calls %d)", h.randCalls)
	}
	for _, e := range h.log {
		if e.Kind == events.Shuffle {
			t.Fatal("RandomOrder$ NoShuffle$ True shuffled the whole library")
		}
		if e.Kind == events.Note && strings.Contains(e.Text, "ChangeZone ignores unread parameter(s) RandomOrder$") {
			t.Fatalf("RandomOrder parameter was not consumed: %q", e.Text)
		}
	}
}
