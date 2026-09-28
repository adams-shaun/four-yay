package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// assertCybermanEntry checks the folded entry rather than the printed card:
// the original face must differ so a missing marker cannot satisfy the test.
func assertCybermanEntry(t *testing.T, e *Engine, id state.ObjID, name string) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Card == nil || o.Face() == nil || o.Face().Name != name {
		t.Fatalf("entry %d: wrong printed card: %+v", id, o)
	}
	if slices.Equal(o.Face().Types, []string{"Artifact", "Creature", "Cyberman"}) || (o.Face().Power() == 2 && o.Face().Toughness() == 2) {
		t.Fatalf("precondition: printed face %q already resembles the Cyberman entry: %+v", name, o.Face())
	}
	if o.Zone != state.ZBattlefield || !o.FaceDown || o.Controller != 0 || o.FaceDownSetType != "Artifact & Creature & Cyberman" || !o.FaceDownHasPT || o.FaceDownPower != 2 || o.FaceDownToughness != 2 {
		t.Fatalf("%q entry: zone=%s faceDown=%v controller=%d setType=%q pt=%d/%d hasPT=%v", name, o.Zone, o.FaceDown, o.Controller, o.FaceDownSetType, o.FaceDownPower, o.FaceDownToughness, o.FaceDownHasPT)
	}
	d := e.Derived(id)
	if !slices.Equal(d.Types, []string{"Artifact", "Creature", "Cyberman"}) || d.Power != 2 || d.Toughness != 2 || len(d.Keywords) != 0 {
		t.Fatalf("%q derived face: types=%v pt=%d/%d keywords=%v", name, d.Types, d.Power, d.Toughness, d.Keywords)
	}
	opp := view.Project(e.G, e, 1, nil)
	found := false
	for _, card := range opp.Players[0].Battlefield {
		if card.ID == id {
			found = true
			if !card.FaceDown || card.Name != "" {
				t.Fatalf("opponent sees printed face %q: %+v", name, card)
			}
		}
	}
	if !found {
		t.Fatalf("opponent view missing %q on controller's battlefield", name)
	}
}

func TestDeathInHeavenChapterIIIReturnsFaceDownCyberman(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Death in Heaven")}, []*cards.Card{lookup(t, reg, "Llanowar Elves")})
	saga := moveByName(t, e, 0, "Death in Heaven", state.ZBattlefield)
	if o := e.G.Obj(saga); o.Zone != state.ZBattlefield || o.Counter("LORE") != 1 {
		t.Fatalf("precondition: Saga entered without chapter I: %+v", o)
	}
	// Let the first two real chapter triggers resolve (their target-player
	// mills are harmless); then seed an exile associated with this Saga, as
	// those chapters do not themselves attach ExiledWith provenance.
	answerQuiet(t, e, 80)
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	answerQuiet(t, e, 80)
	elf := moveByName(t, e, 1, "Llanowar Elves", state.ZExile)
	// Associate it through the logged MoveZone payload, not a direct state write.
	e.emit(events.Event{Kind: events.MoveZone, Obj: elf, From: state.ZExile, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: elf, From: state.ZGraveyard, To: state.ZExile, IDs: []state.ObjID{saga}})
	if o := e.G.Obj(elf); o.Zone != state.ZExile || o.ExiledWith != saga || o.Controller == 0 || o.Face().Power() == 2 {
		t.Fatalf("precondition: exiled elf must belong to opponent, be associated with Saga and differ from 2/2: %+v", o)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	answerQuiet(t, e, 80)
	assertCybermanEntry(t, e, elf, "Llanowar Elves")
	replayCheck(t, e, cfg)
}

func TestCybershipCombatDigReturnsTwoFaceDownCybermen(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Cybership")}, []*cards.Card{})
	ship := moveByName(t, e, 0, "Cybership", state.ZBattlefield)
	lib := e.G.Zone(state.ZLibrary, 1)
	if len(lib) < 2 || lib[0] == lib[1] || e.G.Obj(ship).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Cybership must be in play and opponent's library have two distinct top objects: %v", lib)
	}
	top := append([]state.ObjID(nil), lib[:2]...)
	for _, id := range top {
		if o := e.G.Obj(id); o.Zone != state.ZLibrary || o.Owner != 1 || o.Controller == 0 || o.Face().Name != "Mountain" {
			t.Fatalf("precondition: top card must be opponent's printed Mountain in library: %+v", o)
		}
	}
	// Same combat-damage provenance as dealCombatDamage; damage from a
	// non-combat ability must not fire Cybership's CombatDamage$ trigger.
	e.damaging, e.combatDamaging = ship, true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.damaging, e.combatDamaging = 0, false
	e.priorityRound()
	answerQuiet(t, e, 80)
	for _, id := range top {
		assertCybermanEntry(t, e, id, "Mountain")
	}
	replayCheck(t, e, cfg)
}
