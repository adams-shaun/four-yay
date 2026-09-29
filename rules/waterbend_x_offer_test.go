package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const waterbendXOfferSource = "Name:Waterbend Offer\nManaCost:0\nTypes:Enchantment\nA:AB$ AnimateAll | Cost$ Waterbend<X> | XMin$ 1 | ValidCards$ Creature.YouCtrl | Power$ X | Toughness$ X | SpellDescription$ x.\nOracle:x\n"
const waterbendXOfferTap = "Name:Tap Crab\nManaCost:0\nTypes:Creature Crab\nPT:1/1\nOracle:x\n"

func requireWaterbendXOfferSetup(t *testing.T, e *Engine, id state.ObjID, taps []state.ObjID) {
	t.Helper()
	src := e.G.Obj(id)
	if src == nil || src.Zone != state.ZBattlefield || src.Controller != 0 || src.Face() == nil || len(src.Face().Abilities) == 0 {
		t.Fatalf("precondition: ability source %d must be controlled on battlefield with an ability: %+v", id, src)
	}
	ab := src.Face().Abilities[0]
	if ab.Kind != "AB" || ab.Params["Cost"] != "Waterbend<X>" || ab.Params["XMin"] != "1" {
		t.Fatalf("precondition: ability must carry Waterbend<X> and XMin$ 1: %+v", ab)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: mana pool must be empty, got %d", got)
	}
	for _, tap := range taps {
		o := e.G.Obj(tap)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.Tapped || !(o.EffectiveIsArtifact() || o.EffectiveIsCreature()) || e.untappedManaSource(0, tap) {
			t.Fatalf("precondition: %d must be an untapped, eligible non-mana artifact/creature on the battlefield: %+v", tap, o)
		}
	}
}

func TestWaterbendXAbilityOfferedWithEmptyPool(t *testing.T) {
	e, _, id := newFixtureDeck(t, 7, waterbendXOfferSource)
	tap := onBoardReady(t, e, 0, waterbendXOfferTap)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	requireWaterbendXOfferSetup(t, e, id, []state.ObjID{tap})
	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatal("Waterbend<X> ability withheld with XMin=1, one eligible tap and no mana")
	}
}

func TestKataraWaterbendXAbilityOffered(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Katara, Water Tribe's Hope")
	var taps []state.ObjID
	for i := 0; i < 3; i++ {
		taps = append(taps, onBoardReady(t, e, 0, waterbendXOfferTap))
	}
	id := searchMoveByName(t, e, "Katara, Water Tribe's Hope", state.ZBattlefield)
	requireWaterbendXOfferSetup(t, e, id, taps)
	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatal("Katara's Waterbend<X> ability withheld with three eligible taps and no mana")
	}
}

func TestWaterbendXAbilityWithheldWithoutTaps(t *testing.T) {
	e, _, id := newFixtureDeck(t, 7, waterbendXOfferSource)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	requireWaterbendXOfferSetup(t, e, id, nil)
	for _, oid := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(oid)
		if !o.Tapped && (o.EffectiveIsArtifact() || o.EffectiveIsCreature()) {
			t.Fatalf("precondition: no untapped tap candidates, found %d", oid)
		}
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatal("Waterbend<X> ability offered without mana or eligible taps")
	}
	// The only changed input is one eligible tap. This also proves the
	// negative assertion isn't just a permanently unavailable ability.
	tap := onBoardReady(t, e, 0, waterbendXOfferTap)
	e.pending = nil
	e.priorityRound()
	requireWaterbendXOfferSetup(t, e, id, []state.ObjID{tap})
	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatal("Waterbend<X> still withheld after adding one eligible tap")
	}
}
