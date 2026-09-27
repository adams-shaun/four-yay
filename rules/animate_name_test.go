package rules

// api:Animate.Name (task api-animate-name): the end-to-end pins for the
// Animate `Name$` rider on REAL corpus cards. The rename is registered as a
// layer-3 SetName ContinuousEffect -- the same characteristic a printed
// SetName$ static registers -- so Derived.Name, Engine.Name and the effects
// tier's rename table all answer the new name, never a display alias.
//
//   - The Curse of Fenric: chapter II's `DB$ Animate | ... | Name$ Fenric`
//     renames a real nontoken creature. Chapter III of the SAME card is the
//     consumer that makes this load-bearing rather than cosmetic: its
//     `DB$ Fight | ValidTgts$ Creature.namedFenric` target offer only sees
//     the animated creature if the rename reaches the effects filter table.
//   - Awakening of Vitu-Ghazi: the `Defined$ Targeted` sub-ability shape,
//     proving the rename lands on the selected target, not the animating
//     source.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// renameTableName returns the deriving rename-table entry for id, if any, plus
// whether one exists at all. It reads the same table effects' resolving name
// filters see (Engine.EffectiveNames), so asserting on it proves the rename is
// a real rules-path characteristic, not just a Derived scalar.
func renameTableName(e *Engine, id state.ObjID) (string, bool) {
	for _, n := range e.EffectiveNames() {
		if n.ID == id {
			return n.Name, true
		}
	}
	return "", false
}

// TestCurseOfFenricChapterIIRenamesTargetToFenric drives chapter II of the
// real corpus The Curse of Fenric: a Grizzly Bears becomes a 6/6 legendary
// Horror named Fenric, and the name is a real layer-3 characteristic -- both
// Derived.Name and the effects rename table answer it. The test asserts the
// rename reaches the table the card's OWN chapter III reads
// (`Creature.namedFenric`), which is what makes the derived characteristic
// load-bearing.
func TestCurseOfFenricChapterIIRenamesTargetToFenric(t *testing.T) {
	reg := searchTestRegistry(t)
	fenric := lookup(t, reg, "The Curse of Fenric")
	bear := lookup(t, reg, "Grizzly Bears")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{fenric, bear}, []*cards.Card{})
	saga := moveByName(t, e, 0, "The Curse of Fenric", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	// Precondition: the target is on the battlefield under the animation's
	// controller and NOT yet named Fenric -- so the name assertion below
	// cannot pass vacuously.
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear zone %v, want battlefield", o.Zone)
	}
	if got := e.Derived(bearID).Name; got != "Grizzly Bears" {
		t.Fatalf("precondition: bear is already named %q", got)
	}
	if got := e.G.Obj(saga).Counter("LORE"); got != 1 {
		t.Fatalf("precondition: Saga entered with %d lore counters, want 1", got)
	}

	// Chapter I (the entry lore counter): an up-to-one-creature destroy that
	// finds nothing, so answerQuiet selects no target. Chapter II rides the
	// next lore counter.
	answerQuiet(t, e, 80)

	// Chapter II: emit the second lore counter; it poses the Animate target
	// ask. drainToTargetAsk settles chapter I's own asks first.
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	d := drainToTargetAsk(t, e, 120)
	if d.Kind != decision.KTarget {
		t.Fatalf("chapter II did not pose its target ask: %+v", d)
	}
	idx := indexOfObjOption(d, bearID)
	if idx < 0 {
		t.Fatalf("chapter II's Animate did not offer the Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	answerQuiet(t, e, 120)

	// The rename is a real derived characteristic: the layer walk answers it.
	if got := e.Derived(bearID).Name; got != "Fenric" {
		t.Fatalf("animated Grizzly Bears derived name = %q, want Fenric", got)
	}
	if got := e.Name(bearID); got != "Fenric" {
		t.Fatalf("Engine.Name = %q, want Fenric", got)
	}
	// And the OTHER half of chapter II applied beside it, proving the rename
	// effect rode the same grant and not a stray registration.
	db := e.Derived(bearID)
	if db.Power != 6 || db.Toughness != 6 {
		t.Fatalf("animated Grizzly Bears = %d/%d, want 6/6", db.Power, db.Toughness)
	}
	// The derived name reaches the effects filter table (the path a resolving
	// `namedFenric` target uses).
	tn, ok := renameTableName(e, bearID)
	if !ok {
		t.Fatal("the rename is absent from the effects rename table; a resolving name filter would read the printed name")
	}
	if tn != "Fenric" {
		t.Fatalf("rename table entry for the animated bear = %q, want Fenric", tn)
	}
	replayCheck(t, e, cfg)
}

// TestAwakeningOfVituGhaziRenamesTargetedLand pins the `Defined$ Targeted`
// sub-ability shape on the real corpus Awakening of Vitu-Ghazi: the targeted
// LAND (not the animating source, and not another permanent) carries the new
// name Vitu-Ghazi while a second, untouched land keeps its own.
func TestAwakeningOfVituGhaziRenamesTargetedLand(t *testing.T) {
	reg := searchTestRegistry(t)
	awakening := lookup(t, reg, "Awakening of Vitu-Ghazi")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{awakening}, []*cards.Card{})
	spell := moveByName(t, e, 0, "Awakening of Vitu-Ghazi", state.ZHand)
	target := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	other := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	if target == other {
		t.Fatal("precondition: the two lands are the same object")
	}

	addMana(t, e, 0, "GGCCC")
	d := e.Pending()
	optIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			optIdx = o.Index
		}
	}
	if optIdx < 0 {
		t.Fatalf("Awakening of Vitu-Ghazi was not castable: %+v", d.Options)
	}
	submitChoices(t, e, optIdx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the PutCounter target ask, got %+v", d)
	}
	idx := indexOfObjOption(d, target)
	if idx < 0 {
		t.Fatalf("the targeted Mountain is not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)

	if got := e.Derived(target).Name; got != "Vitu-Ghazi" {
		t.Fatalf("targeted land derived name = %q, want Vitu-Ghazi", got)
	}
	if got := e.Derived(other).Name; got != "Mountain" {
		t.Fatalf("untouched land derived name = %q, want Mountain", got)
	}
	// The land became a real creature characteristic as well, so the rename
	// rides the same Animate grant, not a stray registration.
	if !hasTypeWord(e.Derived(target).Types, "Creature") {
		t.Fatalf("targeted land types = %v, want Creature", e.Derived(target).Types)
	}
	replayCheck(t, e, cfg)
}
