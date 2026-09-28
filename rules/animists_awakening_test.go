package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Animist's Awakening's spell-mastery tail is the corpus's ONLY
// Untap+ConditionZone$ line: the gate must count the ConditionPresent$ spec
// in the NAMED zone (the graveyard), not the battlefield. Before the
// ConditionZone$ read the hardcoded battlefield scan always counted 0, failed
// GE2, and the untap never fired even with two instants in the graveyard.
//
// animistsAwakeningSrc is the real card script verbatim (no corpus
// dependency); the pin resolves its DBUntap SVar exactly as the Dig's
// SubAbility$ chain would.
const animistsAwakeningSrc = "Name:Animist's Awakening\nManaCost:X G\nTypes:Sorcery\n" +
	"SVar:DBUntap:DB$ Untap | Defined$ Remembered | ConditionPresent$ Instant.YouOwn,Sorcery.YouOwn | ConditionZone$ Graveyard | ConditionCompare$ GE2\n" +
	"Oracle:x\n"

func untapAnimistEngine(t *testing.T, instants int) (*Engine, []state.ObjID) {
	t.Helper()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.priorityRound()

	landCard := card(t, "Name:Fixture Land\nTypes:Land\nOracle:x\n")
	var lands []state.ObjID
	for i := 0; i < 2; i++ {
		o := e.G.AddObject(landCard, 0)
		o.Zone = state.ZBattlefield
		o.Tapped = true
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
		lands = append(lands, o.ID)
	}
	instCard := card(t, "Name:Fixture Instant\nTypes:Instant\nOracle:x\n")
	for i := 0; i < instants; i++ {
		o := e.G.AddObject(instCard, 0)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
	}
	aw := card(t, animistsAwakeningSrc)
	e.G.AddObject(aw, 0) // the cast spell, as the resolution's source
	spell := e.G.Obj(e.G.NextID - 1)
	spell.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})

	ctx := &effects.Ctx{Source: spell.ID, Controller: 0,
		Remembered: []state.Target{{Obj: lands[0]}, {Obj: lands[1]}}}
	effects.Resolve(e, ctx, cards.ResolveSVar(aw.Faces[0].SVars, "DBUntap"))
	return e, lands
}

// TestAnimistsAwakeningSpellMasteryUntaps pins the corpus carrier end to end:
// two instants in the graveyard meet GE2 counted in the graveyard, and the
// remembered lands the spell put onto the battlefield tapped untap.
func TestAnimistsAwakeningSpellMasteryUntaps(t *testing.T) {
	t.Parallel()
	e, lands := untapAnimistEngine(t, 2)
	for _, id := range lands {
		if e.G.Obj(id).Tapped {
			t.Fatalf("land %d stayed tapped despite spell mastery", id)
		}
	}
}

// TestAnimistsAwakeningSpellMasteryFallsShort: one instant fails GE2, the
// lands stay tapped.
func TestAnimistsAwakeningSpellMasteryFallsShort(t *testing.T) {
	t.Parallel()
	e, lands := untapAnimistEngine(t, 1)
	for _, id := range lands {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("land %d untapped with only one instant in the graveyard", id)
		}
	}
}
