package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Eladamri's real ChooseCard ability has already picked one card before its
// ChangeZone sub runs. Both possible origin zones must admit that exact card,
// without a second (unrestricted) hidden search.
func assertEladamriChosenCardMovesFromEitherOrigin(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Eladamri, Korvecdal")
	if !ok {
		t.Fatal("corpus missing Eladamri, Korvecdal")
	}
	var move *cards.SA
	for _, ability := range card.Faces[0].Abilities {
		if ability.API == "ChooseCard" && ability.Sub != nil &&
			ability.Sub.Params["Origin"] == "Library,Hand" && ability.Sub.Params["Defined"] == "ChosenCard" {
			move = ability.Sub
			break
		}
	}
	if move == nil {
		t.Fatal("corpus pin moved: Eladamri's ChooseCard sub missing")
	}
	for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
		t.Run(zone.String(), func(t *testing.T) {
			h := &askHost{}
			h.g = state.NewGame(names(2))
			source := h.g.AddObject(card, 0)
			source.Zone = state.ZBattlefield
			chosen := h.g.AddObject(mkCard(t, "Name:Chosen Elf\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"), 0)
			chosen.Zone = zone
			distractor := h.g.AddObject(mkCard(t, "Name:Library Elf\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"), 0)
			distractor.Zone = state.ZLibrary
			h.g.SetZone(zone, 0, []state.ObjID{chosen.ID})
			if zone == state.ZLibrary {
				h.g.SetZone(zone, 0, []state.ObjID{chosen.ID, distractor.ID})
			} else {
				h.g.SetZone(state.ZLibrary, 0, []state.ObjID{distractor.ID})
			}
			if chosen.Zone != zone || chosen.ID == distractor.ID || distractor.Zone != state.ZLibrary {
				t.Fatal("Eladamri candidates not in distinct chosen/distractor positions")
			}
			c := &Ctx{Source: source.ID, Controller: 0, Chosen: []state.Target{{Obj: chosen.ID}}, ChosenValid: true}
			effChangeZone(h, c, move)
			if h.asked != nil {
				t.Fatalf("second, unrestricted choice: %+v", h.asked)
			}
			if got := h.g.Obj(chosen.ID).Zone; got != state.ZBattlefield {
				t.Fatalf("chosen card zone = %v, want battlefield", got)
			}
			if got := h.g.Obj(distractor.ID).Zone; got != state.ZLibrary {
				t.Fatalf("distractor zone = %v, want library", got)
			}
		})
	}
}

// Player-valued Defined$ and ValidTgts$ selectors share the same fetch
// contract as DefinedPlayer$: their answer identifies whose zones to offer,
// not a card to move. A battlefield/hand union is included as well.
func TestCompoundOriginPlayerSelectorKinds(t *testing.T) {
	for _, tc := range []struct {
		name, script string
	}{
		{"defined", "DB$ ChangeZone | Defined$ Targeted | Origin$ Library,Hand | Destination$ Exile | ChangeType$ Creature"},
		{"valid-target", "DB$ ChangeZone | ValidTgts$ Player | Origin$ Hand,Graveyard | Destination$ Exile | ChangeType$ Creature"},
		{"battlefield-hand", "DB$ ChangeZone | DefinedPlayer$ Targeted | Origin$ Battlefield,Hand | Destination$ Exile | ChangeType$ Creature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &askHost{}
			h.g = state.NewGame(names(2))
			source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0)
			hand := h.g.AddObject(mkCard(t, "Name:Hand Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
			hand.Zone = state.ZHand
			otherZone := state.ZGraveyard
			switch tc.name {
			case "defined":
				otherZone = state.ZLibrary
			case "battlefield-hand":
				otherZone = state.ZBattlefield
			}
			other := h.g.AddObject(mkCard(t, "Name:Other Creature\nTypes:Creature\nPT:3/3\nOracle:x\n"), 1)
			other.Zone = otherZone
			h.g.SetZone(state.ZHand, 1, []state.ObjID{hand.ID})
			h.g.SetZone(otherZone, 1, []state.ObjID{other.ID})
			if hand.Zone == other.Zone || hand.ID == other.ID {
				t.Fatal("candidates must differ in zone and identity")
			}
			c := &Ctx{Source: source.ID, Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}}
			s := sa(t, tc.script)
			effChangeZone(h, c, s)
			if h.asked == nil || h.asked.ResumeKind != "search" {
				t.Fatalf("decision = %+v, want compound search", h.asked)
			}
			seen := map[state.ObjID]bool{}
			for _, opt := range h.asked.Options {
				seen[opt.Obj] = true
				if opt.Player != 1 {
					t.Fatalf("option %+v not owned by selected player", opt)
				}
			}
			if !seen[hand.ID] || !seen[other.ID] {
				t.Fatalf("options = %+v, want selected player's hand and %v", h.asked.Options, otherZone)
			}
		})
	}
}
