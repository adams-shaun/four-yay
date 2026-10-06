package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// greenSunDestinationHost answers the DigMultiple selection like
// digMultipleHost and the X>=5 destination ask with dest (an option index).
type greenSunDestinationHost struct {
	*digMultipleHost
	dest      int
	destAsked *decision.Decision
	destAsks  int
}

func (h *greenSunDestinationHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	if d.ResumeKind == "changezone_dest_alt" {
		cp := *d
		cp.Options = append([]decision.Option(nil), d.Options...)
		h.destAsked = &cp
		h.destAsks++
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{h.dest}}, true
	}
	return h.digMultipleHost.TapeAnswer(d)
}

// TestDigMultipleGreenSunConditionalDestinationChoice resolves the real
// compiled Green Sun's Twilight with X=5 (a six-card window) and proves the
// controller's "battlefield or hand" answer moves exactly the chosen cards,
// while the unchosen window remainder still goes to the library bottom.
func TestDigMultipleGreenSunConditionalDestinationChoice(t *testing.T) {
	for _, tc := range []struct {
		name string
		dest int
		want state.Zone
	}{
		{name: "hand", dest: 0, want: state.ZHand},
		{name: "battlefield", dest: 1, want: state.ZBattlefield},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ability, vars := corpusRiderSA(t, "Green Sun's Twilight", "")
			if ability.API != "DigMultiple" || !strings.Contains(ability.Params["DigNum"], "X/Plus.1") {
				t.Fatalf("precondition: Green Sun compiled root ability = %+v", ability)
			}
			h := &greenSunDestinationHost{digMultipleHost: &digMultipleHost{fakeHost: newHost(t, 2), choices: []int{0, 2}}, dest: tc.dest}
			window := []state.ObjID{
				h.g.AddObject(mkCard(t, riderBear), 0).ID,
				h.g.AddObject(mkCard(t, "Name:Second Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID,
				h.g.AddObject(mkCard(t, riderLand), 0).ID,
				h.g.AddObject(mkCard(t, riderSurge), 0).ID,
				h.g.AddObject(mkCard(t, "Name:Third Surge\nTypes:Sorcery\nOracle:x\n"), 0).ID,
				h.g.AddObject(mkCard(t, "Name:Fourth Surge\nTypes:Sorcery\nOracle:x\n"), 0).ID,
			}
			tail := []state.ObjID{
				h.g.AddObject(mkCard(t, riderLand), 0).ID,
				h.g.AddObject(mkCard(t, riderBear), 0).ID,
			}
			h.g.SetZone(state.ZLibrary, 0, append(append([]state.ObjID(nil), window...), tail...))
			for _, id := range append(append([]state.ObjID(nil), window...), tail...) {
				h.g.Obj(id).Zone = state.ZLibrary
			}
			if !MatchesSpecCtx(h.g, "Creature", window[0], NewSpecContext(0, 0)) ||
				!MatchesSpecCtx(h.g, "Land", window[2], NewSpecContext(0, 0)) {
				t.Fatal("precondition: the chosen window cards must be a creature and a land")
			}

			source := h.g.AddObject(card, 0)
			source.Zone = state.ZStack
			h.g.Stack = []state.ObjID{source.ID}
			svars := make(map[string]string, len(vars))
			for k, v := range vars {
				svars[k] = v
			}
			svars["X"] = "Number$5"
			ctx := &Ctx{Controller: 0, Source: source.ID, SVars: svars}
			if got := numText(h, ctx, DigMultipleOf(ability).Num, 1); got != 6 {
				t.Fatalf("precondition: X+1 window = %d, want 6", got)
			}
			Resolve(h, ctx, ability)

			if h.destAsked == nil || h.destAsks != 1 || len(h.destAsked.Options) != 2 || h.destAsked.Player != 0 {
				t.Fatalf("X>=5 destination choice was not asked exactly once of the controller (asks=%d): %+v; events=%+v", h.destAsks, h.destAsked, h.log)
			}
			if h.destAsked.Options[0].Label == h.destAsked.Options[1].Label {
				t.Fatalf("precondition: the two destination options must differ: %+v", h.destAsked.Options)
			}
			selected := []state.ObjID{window[0], window[2]}
			for _, id := range selected {
				if got := h.g.Obj(id).Zone; got != tc.want {
					t.Fatalf("chosen card %d zone = %s, want %s; events=%+v", id, got, tc.want, h.log)
				}
			}
			other := state.ZBattlefield
			if tc.want == state.ZBattlefield {
				other = state.ZHand
			}
			if n := len(zoneOf(h.g, other, 0)); n != 0 {
				t.Fatalf("%d cards reached %s; the choice must move the chosen cards as one group", n, other)
			}
			lib := h.g.Zone(state.ZLibrary, 0)
			if len(lib) != len(tail)+4 {
				t.Fatalf("library = %v, want tail %v above the four unchosen window cards", lib, tail)
			}
			for i, id := range tail {
				if lib[i] != id {
					t.Fatalf("untouched library tail = %v, want %v", lib[:len(tail)], tail)
				}
			}
			bottom := append([]state.ObjID(nil), lib[len(tail):]...)
			want := []state.ObjID{window[1], window[3], window[4], window[5]}
			sort.Slice(bottom, func(i, j int) bool { return bottom[i] < bottom[j] })
			for i := range want {
				if bottom[i] != want[i] {
					t.Fatalf("library bottom = %v, want exactly the unchosen window cards %v", lib[len(tail):], want)
				}
			}
			for _, ev := range h.log {
				if ev.Kind == events.Note && strings.Contains(ev.Text, "DestAltSVar$") {
					t.Fatalf("an answered destination ask must not emit the fallback Note: %+v", ev)
				}
			}
		})
	}
}
