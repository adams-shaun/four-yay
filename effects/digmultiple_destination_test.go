package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

type greenSunDestinationHost struct {
	*digMultipleHost
	destination int
}

func (h *greenSunDestinationHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	if d.ResumeKind == "changezone_dest_alt" {
		h.asked = append(h.asked, d)
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{h.destination}}, true
	}
	return h.digMultipleHost.TapeAnswer(d)
}

func TestDigMultipleGreenSunConditionalDestinationChoice(t *testing.T) {
	_, ability, _ := corpusRiderSA(t, "Green Sun's Twilight", "")
	if ability.API != "DigMultiple" || ability.Params["ChangeLater"] != "True" {
		t.Fatalf("precondition: expected corpus DigMultiple chain, got %+v", ability)
	}
	for _, tc := range []struct {
		name        string
		destination int
		wantZone    state.Zone
	}{
		{name: "hand", destination: 0, wantZone: state.ZHand},
		{name: "battlefield", destination: 1, wantZone: state.ZBattlefield},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &digMultipleHost{fakeHost: newHost(t, 2)}
			h := &greenSunDestinationHost{digMultipleHost: base, destination: tc.destination}
			var ids []state.ObjID
			for _, text := range []string{
				"Name:BearA\nTypes:Creature\nPT:2/2\nOracle:x\n",
				"Name:Isle\nTypes:Basic Land Island\nOracle:x\n",
				"Name:BearB\nTypes:Creature\nPT:2/2\nOracle:x\n",
				"Name:BlastA\nTypes:Sorcery\nOracle:x\n",
				"Name:BlastB\nTypes:Sorcery\nOracle:x\n",
				"Name:BlastC\nTypes:Sorcery\nOracle:x\n",
			} {
				ids = append(ids, h.g.AddObject(mkCard(t, text), 0).ID)
			}
			h.g.SetZone(state.ZLibrary, 0, ids)
			if len(ids) != 6 || !MatchesSpecCtx(h.g, "Creature", ids[0], NewSpecContext(0, 0)) || !MatchesSpecCtx(h.g, "Land", ids[1], NewSpecContext(0, 0)) || MatchesSpecCtx(h.g, "Land", ids[0], NewSpecContext(0, 0)) {
				t.Fatal("precondition: six-card window has distinct creature and land choices")
			}
			h.choices = []int{0, 1}
			source := h.g.AddObject(mkCard(t, "Name:GreenSun\nTypes:Sorcery\nOracle:x\n"), 0).ID
			c := &Ctx{Controller: 0, Source: source, SVars: map[string]string{"X": "Number$5"}}
			p := compileDigMultiple(ability)
			if got := numText(h, c, p.Num, 1); got != 6 {
				t.Fatalf("precondition: X+1 window = %d, want 6", got)
			}
			Resolve(h, c, ability)
			if len(h.asked) == 0 {
				t.Fatal("no real conditional destination decision was posed")
			}
			var destinationAsk *decision.Decision
			for _, d := range h.asked {
				if d.ResumeKind == "changezone_dest_alt" {
					destinationAsk = d
					break
				}
			}
			if destinationAsk == nil || len(destinationAsk.Options) != 2 || destinationAsk.Options[0].Label != "hand" || destinationAsk.Options[1].Label != "battlefield" {
				t.Fatalf("destination choice %+v", destinationAsk)
			}
			if !reflect.DeepEqual(h.g.Zone(tc.wantZone, 0), []state.ObjID{ids[0], ids[1]}) {
				t.Fatalf("chosen destination %v = %v, want selected creature and land", tc.wantZone, h.g.Zone(tc.wantZone, 0))
			}
			other := state.ZHand
			if tc.wantZone == state.ZHand {
				other = state.ZBattlefield
			}
			if len(h.g.Zone(other, 0)) != 0 {
				t.Fatalf("other destination=%v", h.g.Zone(other, 0))
			}
			gotRest := h.g.Zone(state.ZLibrary, 0)
			if len(gotRest) != 4 {
				t.Fatalf("remainder should be bottomed, got library %v", gotRest)
			}
			for _, id := range ids[2:] {
				if !containsObj(gotRest, id) {
					t.Fatalf("remainder %v lost unselected window card %d", gotRest, id)
				}
			}
		})
	}
}
