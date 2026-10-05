package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type altSearchHost struct {
	*digMultipleHost
	destination int
}

func (h *altSearchHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	if d.ResumeKind == "changezone_dest_alt" {
		h.asked = append(h.asked, d)
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{h.destination}}, true
	}
	return h.digMultipleHost.TapeAnswer(d)
}

func caravanVigilSearch(t *testing.T) *cards.SA {
	t.Helper()
	_, ability, _ := corpusRiderSA(t, "Caravan Vigil", "")
	if ability.API != "ChangeZone" || ability.Params["Origin"] != "Library" || ability.Params["DestinationAlternative"] != "Battlefield" {
		t.Fatalf("precondition: Caravan Vigil corpus search = %+v", ability)
	}
	copySA := *ability
	copySA.Params = make(map[string]string, len(ability.Params))
	for key, value := range ability.Params {
		copySA.Params[key] = value
	}
	// Keep the corpus ability and destinations, while making the Morbid gate
	// deterministically true in this isolated resolution.
	copySA.Params["DestAltSVar"] = "One"
	return &copySA
}

func TestChangeZoneAltDestinationWaitsForSuccessfulLibrarySearch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		library   string
		wantDest  state.Zone
		wantAsked bool
	}{
		{name: "no eligible basic land", library: "Name:Spell\nTypes:Sorcery\nOracle:x\n", wantDest: state.ZHand},
		{name: "found basic land", library: "Name:Isle\nTypes:Basic Land Island\nOracle:x\n", wantDest: state.ZBattlefield, wantAsked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &altSearchHost{digMultipleHost: &digMultipleHost{fakeHost: newHost(t, 2), choices: []int{0}}, destination: 1}
			id := h.g.AddObject(mkCard(t, tc.library), 0).ID
			h.g.SetZone(state.ZLibrary, 0, []state.ObjID{id})
			sa := caravanVigilSearch(t)
			c := &Ctx{Controller: 0, SVars: map[string]string{"One": "Number$1"}}
			Resolve(h, c, sa)
			asked := false
			for _, d := range h.asked {
				if d.ResumeKind == "changezone_dest_alt" {
					asked = true
				}
			}
			if asked != tc.wantAsked {
				t.Fatalf("destination ask=%v, want %v; asks=%+v", asked, tc.wantAsked, h.asked)
			}
			if !tc.wantAsked {
				if len(h.g.Zone(state.ZHand, 0)) != 0 || len(h.g.Zone(state.ZBattlefield, 0)) != 0 {
					t.Fatalf("empty search moved card: hand=%v battlefield=%v", h.g.Zone(state.ZHand, 0), h.g.Zone(state.ZBattlefield, 0))
				}
				shuffled := false
				for _, event := range h.log {
					if event.Kind == events.Shuffle && event.Player == 0 {
						shuffled = true
					}
				}
				if !shuffled {
					t.Fatal("precondition/handler: empty library search did not complete its shuffle tail")
				}
				return
			}
			if got := h.g.Zone(tc.wantDest, 0); len(got) != 1 || got[0] != id {
				t.Fatalf("found card destination %v = %v, want [%d]", tc.wantDest, got, id)
			}
		})
	}
}
