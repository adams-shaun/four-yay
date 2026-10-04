package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Restored from the W3 legacy removal: Transmute / Cycling / TypeCycling
// activations from hand, their searches answered through the kernel.

func TestTransmuteAndCyclingRealHandActivations(t *testing.T) {
	t.Parallel()
	t.Run("Dizzy Spell searches matching mana value", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Dizzy Spell"))
		// Transmute searches for a card with the source's PRINTED mana value
		// (Dizzy Spell's is one). handEngine's library is all Mountains, and a
		// land has no mana cost, so its mana value is zero: the unmodified
		// library holds no legal search result and the search would correctly
		// fail to find. Seed one mana-value-one card for the search to find.
		seed := card(t, "Name:Ember Study\nManaCost:U\nTypes:Instant\nOracle:x\n")
		sObj := e.G.AddObject(seed, 0)
		e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{sObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
		wanted := sObj.ID
		id := e.G.Zone(state.ZHand, 0)[0]
		e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MU] = 1, 2
		var opt decision.Option
		for _, o := range e.legalActions(0) {
			if o.Kind == "ability" && o.Obj == id {
				opt = o
				break
			}
		}
		if opt.Kind != "ability" {
			t.Fatal("Dizzy Spell Transmute activation not offered")
		}
		e.beginActivation(0, opt)
		submitChoices(t, e, 0) // discard Dizzy Spell
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("Transmute discard zone=%s", e.G.Obj(id).Zone)
		}
		e.pending = nil // the kernel probe serves asks only with no decision posed
		e.resolveTop()
		d := e.Pending()
		choice := -1
		if d != nil {
			for i, o := range d.Options {
				if o.Obj == wanted {
					choice = i
					break
				}
			}
		}
		if choice < 0 {
			t.Fatalf("Transmute did not offer matching mana-value library card: %+v", d)
		}
		submitChoices(t, e, choice)
		if e.G.Obj(wanted).Zone != state.ZHand {
			t.Fatalf("Transmute target zone=%s, want hand", e.G.Obj(wanted).Zone)
		}
	})
	t.Run("Ziatora's Proving Ground cycling draws", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Ziatora's Proving Ground"))
		drawn := e.G.Zone(state.ZLibrary, 0)[0]
		id := e.G.Zone(state.ZHand, 0)[0]
		e.G.Players[0].Pool[state.MC] = 3
		var opt decision.Option
		for _, o := range e.legalActions(0) {
			if o.Kind == "ability" && o.Obj == id {
				opt = o
				break
			}
		}
		if opt.Kind != "ability" {
			t.Fatal("Ziatora cycling activation not offered")
		}
		e.beginActivation(0, opt)
		submitChoices(t, e, 0) // discard the cycling card
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("Cycling discard zone=%s", e.G.Obj(id).Zone)
		}
		e.pending = nil // the kernel probe serves asks only with no decision posed
		e.resolveTop()
		if e.G.Obj(drawn).Zone != state.ZHand {
			t.Fatalf("Cycling did not draw: zone=%s", e.G.Obj(drawn).Zone)
		}
	})
}

// TestTypeCyclingSearchesTheNamedType is the CR 702.28d headline: typed
// cycling is a LIBRARY SEARCH for a card of the named type, revealed and put
// into hand -- NOT a draw. Monstrosity of the Lake prints "Islandcycling {2}"
// (K:TypeCycling:Island:2), so it must find the seeded Island and not a
// Mountain, and the stated-quality ChangeType$ must make the search reveal
// the found card by default.
func TestTypeCyclingSearchesTheNamedType(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Monstrosity of the Lake"))
	island := card(t, "Name:Island\nTypes:Basic Land Island\nA:AB$ Mana | Cost$ T | Produced$ U\nOracle:x\n")
	sObj := e.G.AddObject(island, 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{sObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
	wanted := sObj.ID
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == id {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("Monstrosity of the Lake Islandcycling activation not offered")
	}
	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // discard Monstrosity of the Lake
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("TypeCycling discard zone=%s, want graveyard", e.G.Obj(id).Zone)
	}
	e.pending = nil // the kernel probe serves asks only with no decision posed
	e.resolveTop()
	d := e.Pending()
	choice := -1
	for i, o := range d.Options {
		if o.Obj == wanted {
			choice = i
		}
		// An Islandcycling search must not find a Mountain (a different land
		// type). The library is otherwise all Mountains.
		if o.Obj != wanted && e.G.Obj(o.Obj).Face().Name == "Mountain" {
			t.Fatalf("Islandcycling offered a non-Island: %+v", o)
		}
	}
	if choice < 0 {
		t.Fatalf("TypeCycling did not offer the seeded Island: %+v", d)
	}
	submitChoices(t, e, choice)
	if e.G.Obj(wanted).Zone != state.ZHand {
		t.Fatalf("TypeCycling searched card zone=%s, want hand", e.G.Obj(wanted).Zone)
	}
	revealed := false
	for _, ev := range revealNotes(e) {
		for _, rid := range ev.IDs {
			if rid == wanted {
				revealed = true
			}
		}
	}
	if !revealed {
		t.Fatal("TypeCycling search did not reveal the found card")
	}
}

// TestTypeCyclingBasicLandSearchesAnyBasic covers the Basic base (CR 702.28d's
// "Basic landcycling"): the filter must match ANY basic land card, not one
// named type. Kree Sentinel prints K:TypeCycling:Basic:2.
func TestTypeCyclingBasicLandSearchesAnyBasic(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Kree Sentinel"))
	forest := card(t, "Name:Forest\nTypes:Basic Land Forest\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n")
	sObj := e.G.AddObject(forest, 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{sObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
	wanted := sObj.ID
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == id {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("Kree Sentinel basic landcycling activation not offered")
	}
	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // discard Kree Sentinel
	e.pending = nil        // the kernel probe serves asks only with no decision posed
	e.resolveTop()
	d := e.Pending()
	choice := -1
	for i, o := range d.Options {
		if o.Obj == wanted {
			choice = i
		}
	}
	if choice < 0 {
		t.Fatalf("basic landcycling did not offer the seeded Forest: %+v", d)
	}
	submitChoices(t, e, choice)
	if e.G.Obj(wanted).Zone != state.ZHand {
		t.Fatalf("basic landcycling searched card zone=%s, want hand", e.G.Obj(wanted).Zone)
	}
}
