// morph_test.go — the Morph / Megamorph / Disguise face-down cast (CR
// 702.37a/702.168a/702.169a). Each corpus carrier proves, end to end: the
// {3} face-down option is offered BESIDE the printed cast (the assertion
// that it is an addition, not a replacement), the flow poses no target ask
// (CR 708.4: a face-down spell has no targets and no printed spell
// abilities), it resolves as a face-down 2/2 colourless [Creature] with no
// printed keywords, the printed mana cost is never charged (the pool
// empties to exactly the {3}), the pay-time CastInfo records the keyword
// family (state.FlagMorphed / FlagMegamorphed / FlagDisguised — the
// provenance a later turn-face-up action validates and prices against),
// Disguise's face-down ward {2} rides the cloak state bit the Cloak
// machinery already reads, the opponent's view of both the stack spell and
// the face-down permanent is the blank redacted shape while the
// controller's own view shows the printed card, and the whole game replays
// byte-identically from the log.
//
// The decks are compiled corpus cards only (no Forge script text is
// committed here). The helpers come from manifest_test.go, altcast_test.go
// and cast_test.go. The import of the Deadly Disguise precon
// (internal/testutil/decks/deadly-disguise.json) seated 24 morph-family
// carriers in the repo deck set, but they join the acceptance/replay pool,
// not the closed legacyDeckNames list the golden heads seat from, so these
// tests still do not move the golden heads or the botbench pin.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// morphDownCast drives the named card's face-down cast from seat 0's hand
// at Main1, asserting the option PAIR (printed + face-down — the addition
// precondition) and the absence of a target ask, and returns once the spell
// has resolved onto the battlefield with both seats having passed. symbols
// funds the pool for BOTH costs (the face-down {3} plus the printed mana
// cost); poolAfter is the exact total that must remain after the face-down
// cast spends its {3} — the printed cost's mana rides untouched, so a cast
// that charged anything other than {3} fails this.
func morphDownCast(t *testing.T, e *Engine, name, mode, symbols string, poolAfter int) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, symbols)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	plain, down := -1, -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			if o.Mode == "" {
				plain = o.Index
			}
			if o.Mode == mode {
				down = o.Index
			}
		}
	}
	if plain < 0 {
		t.Fatalf("the printed cast option for %s is missing — the face-down option must be an ADDITION to it: %+v", name, d.Options)
	}
	if down < 0 {
		t.Fatalf("no (%s) face-down cast option for %s: %+v", mode, name, d.Options)
	}
	submitChoices(t, e, down)
	// CR 708.4: a face-down spell has no targets to announce.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("face-down cast posed a target ask: %+v", d)
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Pool.Total(); got != int32(poolAfter) {
		t.Fatalf("pool after face-down cast = %d, want %d (exactly the {3} spent)", got, poolAfter)
	}
	return id
}

// assertFaceDownTwoTwo is the shared CR 708.5 shape: the object is on its
// controller's battlefield, face down, a colourless 2/2 with exactly
// [Creature] and no other printed characteristics; the printed identity is
// blank in seat 1's view and shown in seat 0's own view.
func assertFaceDownTwoTwo(t *testing.T, e *Engine, id state.ObjID, name string, wantKeywords []string) {
	t.Helper()
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("%s: zone %s controller %d, want battlefield seat 0", name, o.Zone, o.Controller)
	}
	if !o.FaceDown {
		t.Fatalf("%s: FaceDown=false, want the face-down battlefield entry", name)
	}
	der := e.Derived(id)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("%s face-down P/T = %d/%d, want 2/2", name, der.Power, der.Toughness)
	}
	if len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("%s face-down types = %v, want exactly [Creature]", name, der.Types)
	}
	if der.Colors != "" {
		t.Fatalf("%s face-down colours = %q, want colourless", name, der.Colors)
	}
	if wantKeywords == nil && der.Keywords != nil && len(der.Keywords) != 0 {
		t.Fatalf("%s face-down keywords = %v, want none", name, der.Keywords)
	}
	for _, want := range wantKeywords {
		found := false
		for _, k := range der.Keywords {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s face-down keywords = %v, want %q among them", name, der.Keywords, want)
		}
	}
	// The view redaction (CR 708.4/708.5): the opponent sees the blank
	// face-down shape, the controller sees the printed card.
	oppView := view.Project(e.G, e, 1, nil)
	var blank, own *view.CardView
	for i := range oppView.Players[0].Battlefield {
		if oppView.Players[0].Battlefield[i].ID == id {
			blank = &oppView.Players[0].Battlefield[i]
		}
	}
	if blank == nil {
		t.Fatalf("%s missing from seat 1's battlefield view", name)
	}
	if blank.Name != "" || !blank.FaceDown || blank.Controller != 0 {
		t.Fatalf("opponent's view of the face-down %s: %+v, want the redacted blank", name, blank)
	}
	ctrlView := view.Project(e.G, e, 0, nil)
	for i := range ctrlView.Players[0].Battlefield {
		if ctrlView.Players[0].Battlefield[i].ID == id {
			own = &ctrlView.Players[0].Battlefield[i]
		}
	}
	if own == nil || own.Name != name || !own.FaceDown {
		t.Fatalf("controller's view of its own face-down %s: %+v, want name %q", name, own, name)
	}
}

// assertFaceDownMarkers checks the two log markers (the Secret PutOnStack
// and the battlefield entry MoveZone) and that no target decision was ever
// recorded for the cast.
func assertFaceDownMarkers(t *testing.T, e *Engine, id state.ObjID, marker string) {
	t.Helper()
	pushed, entered := false, false
	for _, ev := range e.L.Events {
		switch {
		case ev.Kind == events.PutOnStack && ev.Obj == id:
			if ev.Counter != marker || !ev.Secret {
				t.Fatalf("PutOnStack Counter=%q Secret=%v, want %q Secret for the face-down cast", ev.Counter, ev.Secret, marker)
			}
			pushed = true
		case ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield:
			if ev.Counter != marker {
				t.Fatalf("battlefield entry Counter=%q, want %q", ev.Counter, marker)
			}
			entered = true
		case ev.Kind == events.TargetsChosen && ev.Obj == id:
			t.Fatalf("face-down cast recorded TargetsChosen: %+v", ev)
		}
	}
	if !pushed || !entered {
		t.Fatalf("markers: PutOnStack %v, battlefield entry %v", pushed, entered)
	}
}

func TestMorphFaceDownCastPaysThreeAndEntersFaceDown(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Kin-Tree Warden")
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	o := e.G.Obj(id)
	if o.CastFlags&state.FlagMorphed == 0 || o.CastFlags&(state.FlagMegamorphed|state.FlagDisguised) != 0 {
		t.Fatalf("Kin-Tree Warden CastFlags = %v, want exactly the morph family flag", o.CastFlags)
	}
	// The pool was exactly the {3} (morphDownCast asserted the leftover
	// G) — the printed {G} was never charged, even though morph's {3} costs
	// MORE than this card's printed cost.
	assertFaceDownTwoTwo(t, e, id, "Kin-Tree Warden", nil)
	assertFaceDownMarkers(t, e, id, events.FaceDownEntryCounter)
	replayCheck(t, e, cfg)
}

func TestMegamorphFaceDownCastRecordsTheFamily(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Kolaghan Stormsinger")
	id := morphDownCast(t, e, "Kolaghan Stormsinger", "megamorphed", "CCCR", 1)
	o := e.G.Obj(id)
	if o.CastFlags&state.FlagMegamorphed == 0 || o.CastFlags&(state.FlagMorphed|state.FlagDisguised) != 0 {
		t.Fatalf("Kolaghan Stormsinger CastFlags = %v, want exactly the megamorph family flag", o.CastFlags)
	}
	// Kolaghan Stormsinger prints haste; while face down it has none (its
	// TurnFaceUp trigger never fires either — there is no turn-up here).
	assertFaceDownTwoTwo(t, e, id, "Kolaghan Stormsinger", nil)
	assertFaceDownMarkers(t, e, id, events.FaceDownEntryCounter)
	replayCheck(t, e, cfg)
}

func TestDisguiseFaceDownCastHasWardTwo(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Basilica Stalker")
	id := morphDownCast(t, e, "Basilica Stalker", "disguised", "CCCCCB", 3)
	o := e.G.Obj(id)
	if o.CastFlags&state.FlagDisguised == 0 || o.CastFlags&(state.FlagMorphed|state.FlagMegamorphed) != 0 {
		t.Fatalf("Basilica Stalker CastFlags = %v, want exactly the disguise family flag", o.CastFlags)
	}
	if !o.Cloaked {
		t.Fatalf("disguised card Cloaked=false, want the ward {2} state bit")
	}
	// CR 702.168: a disguised creature has ward {2} while face down. It
	// prints flying; while face down it has none of that.
	assertFaceDownTwoTwo(t, e, id, "Basilica Stalker", []string{"Ward:2"})
	assertFaceDownMarkers(t, e, id, events.CloakEntryCounter)
	replayCheck(t, e, cfg)
}
