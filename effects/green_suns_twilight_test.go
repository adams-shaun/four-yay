package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDigMultipleGreenSunSelectionAndBottoming(t *testing.T) {
	card, ability, vars := corpusRiderSA(t, "Green Sun's Twilight", "")
	if ability.API != "DigMultiple" || ability.Params["ChangeLater"] != "True" ||
		ability.Params["RememberChanged"] != "True" || ability.Params["ImprintRest"] != "True" ||
		!strings.Contains(ability.Params["DigNum"], "X/Plus.1") {
		t.Fatalf("precondition: Green Sun compiled root ability = %+v", ability)
	}
	h := &digMultipleHost{fakeHost: newHost(t, 2), choices: []int{0, 2}}
	ids := []state.ObjID{
		h.g.AddObject(mkCard(t, riderBear), 0).ID,
		h.g.AddObject(mkCard(t, "Name:Second Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID,
		h.g.AddObject(mkCard(t, riderLand), 0).ID,
		h.g.AddObject(mkCard(t, riderSurge), 0).ID,
	}
	tail := []state.ObjID{
		h.g.AddObject(mkCard(t, riderLand), 0).ID,
		h.g.AddObject(mkCard(t, riderBear), 0).ID,
		h.g.AddObject(mkCard(t, riderSurge), 0).ID,
	}
	library := append(append([]state.ObjID(nil), ids...), tail...)
	h.g.SetZone(state.ZLibrary, 0, library)
	if len(h.g.Zone(state.ZLibrary, 0)) != 7 ||
		!MatchesSpecCtx(h.g, "Creature", ids[0], NewSpecContext(0, 0)) ||
		!MatchesSpecCtx(h.g, "Creature", ids[1], NewSpecContext(0, 0)) ||
		!MatchesSpecCtx(h.g, "Land", ids[2], NewSpecContext(0, 0)) ||
		MatchesSpecCtx(h.g, "Creature", ids[3], NewSpecContext(0, 0)) ||
		MatchesSpecCtx(h.g, "Land", ids[3], NewSpecContext(0, 0)) {
		t.Fatal("precondition: library window must contain two creatures, a land, and an ineligible sorcery")
	}

	source := h.g.AddObject(card, 0)
	source.Zone = state.ZStack
	h.g.Stack = []state.ObjID{source.ID}
	// Model a paid X of 3: the real compiled DigNum is X+1, so all four
	// distinct library cards are in the resolving window and X<5 selects Hand.
	svars := make(map[string]string, len(vars))
	for k, v := range vars {
		svars[k] = v
	}
	svars["X"] = "Number$3"
	ctx := &Ctx{Controller: 0, Source: source.ID, SVars: svars}
	if got := numText(h, ctx, DigMultipleOf(ability).Num, 1); got != 4 {
		t.Fatalf("precondition: X+1 window = %d, want 4", got)
	}
	Resolve(h, ctx, ability)

	if len(h.asked) == 0 || h.asked[0].ResumeKind != "digmultiple" {
		t.Fatalf("compiled DigMultiple selection was not asked: %+v; events=%+v", h.asked, h.log)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API DigMultiple") {
			t.Fatalf("Green Sun root fell through unimplemented dispatch: %+v", ev)
		}
	}
	selected := []state.ObjID{ids[0], ids[2]}
	unselected := []state.ObjID{ids[1], ids[3]}
	for _, id := range selected {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("selected object %d zone = %v, want Hand", id, zoneOf(h.g, state.ZLibrary, 0))
		}
		for _, other := range []state.Zone{state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZLibrary} {
			if o := h.g.Obj(id); o.Zone == other {
				t.Fatalf("selected object %d incorrectly reached %s", id, other)
			}
		}
	}
	if got := h.g.Zone(state.ZHand, 0); len(got) != len(selected) || got[0] == got[1] {
		t.Fatalf("selected hand IDs = %v, want the two distinct chosen objects %v", got, selected)
	}
	gotLibrary := h.g.Zone(state.ZLibrary, 0)
	if len(gotLibrary) != len(unselected)+len(tail) {
		t.Fatalf("library = %v, want untouched tail %v above bottom remainder %v; events=%+v", gotLibrary, tail, unselected, h.log)
	}
	for i, id := range tail {
		if gotLibrary[i] != id {
			t.Fatalf("untouched library tail = %v, want original order %v", gotLibrary[:len(tail)], tail)
		}
	}
	bottom := append([]state.ObjID(nil), gotLibrary[len(tail):]...)
	wantBottom := append([]state.ObjID(nil), unselected...)
	sort.Slice(bottom, func(i, j int) bool { return bottom[i] < bottom[j] })
	sort.Slice(wantBottom, func(i, j int) bool { return wantBottom[i] < wantBottom[j] })
	for i := range wantBottom {
		if bottom[i] != wantBottom[i] {
			t.Fatalf("library bottom = %v, want exactly unselected IDs %v", gotLibrary[len(tail):], unselected)
		}
	}
	for _, ev := range h.log {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			t.Fatalf("Green Sun shuffled the whole library instead of only bottoming the window remainder: %+v", ev)
		}
	}
	for _, id := range unselected {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("unselected object %d zone = %v, want Library bottom", id, o.Zone)
		}
	}
}
