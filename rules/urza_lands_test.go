package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The three Urza lands, authored with the corpus shape: an A:AB$ Mana
// ability whose Amount$ names an SVar that is a Count$UrzaLands expression.
// The plant's Name is "Urza's Power Plant" (no hyphen) while its Types line
// carries the hyphenated "Urza's Power-Plant" subtype, reproducing the
// corpus's own spelling split so the pool leaf pins which string the count
// matches.
const (
	urzaMineSrc = "Name:Urza's Mine\nTypes:Land Urza's Mine\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ UrzaAmount\n" +
		"SVar:UrzaAmount:Count$UrzaLands.2.1\nOracle:x\n"
	urzaTowerSrc = "Name:Urza's Tower\nTypes:Land Urza's Tower\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ UrzaAmount\n" +
		"SVar:UrzaAmount:Count$UrzaLands.3.1\nOracle:x\n"
	urzaPlantSrc = "Name:Urza's Power Plant\nTypes:Land Urza's Power-Plant\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ UrzaAmount\n" +
		"SVar:UrzaAmount:Count$UrzaLands.2.1\nOracle:x\n"
)

// urzaEngine builds a 2-seat game seeded with all three Urza lands and places
// the ones named by `place` onto the battlefield. ctrl overrides which seat
// controls each placed land (default seat 0); a land not in `place` stays in
// the seat's hand/library. It returns the engine, its config (for
// replayCheck) and the placed object ids keyed by "mine"/"tower"/"plant".
func urzaEngine(t *testing.T, place map[string]bool, ctrl map[string]state.PlayerID) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	e, cfg, _ := newFixtureDeck(t, 44, urzaMineSrc, urzaTowerSrc, urzaPlantSrc)
	ids := map[string]state.ObjID{}
	setup := []struct{ key, src string }{
		{"mine", urzaMineSrc},
		{"tower", urzaTowerSrc},
		{"plant", urzaPlantSrc},
	}
	for _, s := range setup {
		if !place[s.key] {
			continue
		}
		id := moveSeeded(t, e, 0, s.src, state.ZBattlefield)
		p := ctrl[s.key]
		if p != 0 {
			// Theft-style setup: events.Move keys the battlefield by
			// controller, so a fixture whose controller differs from its
			// owner must update both the object and the two zone lists --
			// the same class of direct setup write
			// TestStolenCommanderGoesToItsOwnersCommandZoneByItsOwnersChoice
			// uses. There is no control-change event in this build, so this
			// setup is not replay-reconstructable and the leaf that uses it
			// does not call replayCheck.
			if o := e.G.Obj(id); o != nil {
				o.Controller = p
			}
			e.G.SetZone(state.ZBattlefield, 0, withoutID(e.G.Zone(state.ZBattlefield, 0), id))
			e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), id))
		}
		ids[s.key] = id
	}
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.priorityRound()
	return e, cfg, ids
}

// activateMana submits the "activate" (tap-for-mana) option for obj from the
// pending priority decision.
func activateMana(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority for activate of %d: %+v", obj, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == obj {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no activate option for %d: %+v", obj, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit activate %d: %v", obj, err)
	}
}

// Leaf 1: all three lands under one controller. Mine and Plant each tap for 2,
// Tower for 3.
func TestUrzaLandsPoolAllThree(t *testing.T) {
	t.Parallel()
	e, cfg, ids := urzaEngine(t, map[string]bool{"mine": true, "tower": true, "plant": true}, nil)
	activateMana(t, e, ids["mine"])
	if got := e.G.Players[0].Pool[state.MC]; got != 2 {
		t.Fatalf("Mine pool = %d, want 2 colourless", got)
	}
	activateMana(t, e, ids["tower"])
	if got := e.G.Players[0].Pool.Total(); got != 5 {
		t.Fatalf("Mine+Tower pool = %d, want 5 (2+3)", got)
	}
	activateMana(t, e, ids["plant"])
	if got := e.G.Players[0].Pool.Total(); got != 7 {
		t.Fatalf("all three pool = %d, want 7 (2+3+2)", got)
	}
	replayCheck(t, e, cfg)
}

// Leaf 2a: only Mine+Tower present (plant missing); each taps for 1.
func TestUrzaLandsPoolOnlyMineAndTower(t *testing.T) {
	t.Parallel()
	e, cfg, ids := urzaEngine(t, map[string]bool{"mine": true, "tower": true}, nil)
	activateMana(t, e, ids["mine"])
	if got := e.G.Players[0].Pool[state.MC]; got != 1 {
		t.Fatalf("Mine pool = %d, want 1", got)
	}
	activateMana(t, e, ids["tower"])
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("Mine+Tower pool = %d, want 2 (1+1)", got)
	}
	replayCheck(t, e, cfg)
}

// Leaf 2b (the "other" pair): only Mine+Plant present (tower missing); each
// taps for 1.
func TestUrzaLandsPoolOnlyMineAndPlant(t *testing.T) {
	t.Parallel()
	e, cfg, ids := urzaEngine(t, map[string]bool{"mine": true, "plant": true}, nil)
	activateMana(t, e, ids["mine"])
	if got := e.G.Players[0].Pool[state.MC]; got != 1 {
		t.Fatalf("Mine pool = %d, want 1", got)
	}
	activateMana(t, e, ids["plant"])
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("Mine+Plant pool = %d, want 2 (1+1)", got)
	}
	replayCheck(t, e, cfg)
}

// Leaf 3: the lands are split across two controllers, so no single seat
// controls all three. Seat 0 controls Mine+Tower, seat 1 controls the Plant;
// seat 0's Mine still taps for 1 because its count must read seat 0's own
// battlefield, not every battlefield. This setup changes a controller
// directly (no control-change event exists), so it is not replay-checked.
func TestUrzaLandsPoolSplitAcrossControllers(t *testing.T) {
	t.Parallel()
	e, _, ids := urzaEngine(t, map[string]bool{"mine": true, "tower": true, "plant": true},
		map[string]state.PlayerID{"plant": 1})
	activateMana(t, e, ids["mine"])
	if got := e.G.Players[0].Pool[state.MC]; got != 1 {
		t.Fatalf("seat 0's Mine pool = %d, want 1 (the Plant is seat 1's)", got)
	}
	activateMana(t, e, ids["tower"])
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("seat 0's Mine+Tower pool = %d, want 2 (1+1)", got)
	}
}
