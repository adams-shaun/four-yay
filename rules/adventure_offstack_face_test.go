package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CR 715.4: in every zone except the stack (and on the stack when not cast
// as an Adventure) an adventurer card has only its normal characteristics.
// A resolved Adventure spell rests in exile as its MAIN face -- named and
// typed as the creature -- and a countered one goes to the graveyard as the
// main face too. The adventure-zone recast is still offered from exile.
func TestAdventureCardIsMainFaceOffTheStack(t *testing.T) {
	t.Parallel()
	castSwipe := func(t *testing.T, e *Engine, id, bearID state.ObjID) {
		t.Helper()
		alt := adventureOption(t, e, id, "adventure_alt")
		if alt == nil {
			t.Fatalf("adventure_alt offer missing: %+v", castOptions(t, e))
		}
		submitChoices(t, e, alt.Index)
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("Swipe target ask: %+v", d)
		}
		for _, o := range d.Options {
			if o.Obj == bearID {
				submitChoices(t, e, o.Index)
				if f := e.G.Obj(id).Face(); e.G.Obj(id).Zone != state.ZStack || f.Name != "Swipe" {
					t.Fatalf("precondition: on the stack as %q in %s, want the Swipe spell", f.Name, e.G.Obj(id).Zone)
				}
				return
			}
		}
		t.Fatalf("no option targeting the bear: %+v", d.Options)
	}

	t.Run("resolved into exile", func(t *testing.T) {
		e, cfg, id := newFixtureDeck(t, 7411, adventureFixtureSrc, adventureBearSrc)
		bearID := putCreature(t, e, 0, adventureBearSrc)
		addMana(t, e, 0, "RR")
		castSwipe(t, e, id, bearID)
		passUntilStackEmpty(t, e, 20)
		if e.G.Obj(bearID).Zone == state.ZBattlefield {
			t.Fatal("precondition: Swipe did not resolve (the bear survived)")
		}
		o := e.G.Obj(id)
		if o.Zone != state.ZExile || o.FaceIdx != 0 || o.Face().Name != "Swindle" || !o.Face().IsCreature() {
			t.Fatalf("resolved Adventure card: zone=%s face=%d %q, want exile as the main face Swindle", o.Zone, o.FaceIdx, o.Face().Name)
		}
		addMana(t, e, 0, "RRR")
		if adventureOption(t, e, id, "adventure_recast") == nil {
			t.Fatalf("adventure_recast missing from exile: %+v", castOptions(t, e))
		}
		replayCheck(t, e, cfg)
	})

	t.Run("countered into the graveyard", func(t *testing.T) {
		e, cfg, id := newFixtureDeck(t, 7412, adventureFixtureSrc, adventureCancelSrc, adventureBearSrc)
		cancelID := addToHand(t, e, 0, adventureCancelSrc)
		bearID := putCreature(t, e, 0, adventureBearSrc)
		addMana(t, e, 0, "RRUUU")
		castSwipe(t, e, id, bearID)
		submitChoices(t, e, castOptionFor(t, e, cancelID).Index)
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("no target decision for the counter: %+v", d)
		}
		for _, o := range d.Options {
			if o.Obj == id {
				submitChoices(t, e, o.Index)
				break
			}
		}
		passUntilStackEmpty(t, e, 20)
		o := e.G.Obj(id)
		if o.Zone != state.ZGraveyard || o.FaceIdx != 0 || o.Face().Name != "Swindle" {
			t.Fatalf("countered Adventure card: zone=%s face=%d %q, want graveyard as the main face Swindle", o.Zone, o.FaceIdx, o.Face().Name)
		}
		replayCheck(t, e, cfg)
	})
}
