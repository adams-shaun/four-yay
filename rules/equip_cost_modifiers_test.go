package rules

// eqcm1: cards/keywords.go's Equip expansion kept only the cost field of
// Forge's `K:Equip:<cost>:<restriction>:<desc>:<extras>` line and dropped
// every trailing colon field, so the equip-cost-modifier cards, the
// equip-restriction cards and the ActivationLimit riders all collapsed to a
// plain unrestricted "Equip N" at full price. The expansion now parses the
// trailing fields (ReduceCost$ / ActivationLimit$ riders ride the minted SA
// verbatim; the first real field is a target-restriction spec passed through
// as ValidTgts$). These leaves pin each shape on REAL corpus cards through
// the ordinary offer/charge path, the same way rules/activation_limit_test.go
// pins Withering Wisps.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCrownOfGondorEquipCostsLessAsMonarch pins the monarch discount: Crown
// of Gondor's `K:Equip:4:::ReduceCost$ Monarch` (SVar:Monarch:Count$Monarch.3.0)
// costs {1} while its controller is the monarch. With an EMPTY pool the
// ability is not offered ({1} is not payable); with exactly {1} in the pool it
// is offered and the full activation completes, so the pushed ability object
// proves the discounted charge succeeded.
func TestCrownOfGondorEquipCostsLessAsMonarch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Crown of Gondor", "Grizzly Bears"}, nil)
	crown := findOnBoard(t, e, 0, "Crown of Gondor")
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	e.priorityRound()

	if _, ok := findAbilityOption(e, crown, 0); ok {
		t.Fatal("monarch discount not applied: Crown of Gondor's equip offered from an empty pool (still {4})")
	}
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, crown, 0)
	if !ok {
		t.Fatal("Crown of Gondor's equip not offered for {1} while monarch")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(crown).AttachedTo != bear {
		t.Fatalf("Crown of Gondor attached to %d, want the bear %d", e.G.Obj(crown).AttachedTo, bear)
	}
}

// TestCrownOfGondorEquipFullPriceWithoutMonarch pins the other half: without
// the monarch the ReduceCost$ resolves to 0, so the equip stays at {4} —
// not offered with {3} in the pool, offered with {4}.
func TestCrownOfGondorEquipFullPriceWithoutMonarch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Crown of Gondor", "Grizzly Bears"}, nil)
	crown := findOnBoard(t, e, 0, "Crown of Gondor")
	addMana(t, e, 0, "CCC")
	if _, ok := findAbilityOption(e, crown, 0); ok {
		t.Fatal("Crown of Gondor's equip offered for {3} without the monarch (a discount was applied)")
	}
	addMana(t, e, 0, "C")
	if _, ok := findAbilityOption(e, crown, 0); !ok {
		t.Fatal("Crown of Gondor's equip not offered at full price {4} without the monarch")
	}
}

// TestPlateArmorEquipDiscount pins the Valid-count reduction: Plate Armor's
// `K:Equip:3:::ReduceCost$ Y` (SVar:Y:Count$Valid Equipment.YouCtrl+Other)
// costs {3} minus one per OTHER Equipment its controller controls. With two
// other Equipment on the battlefield the equip costs {1} and completes from a
// one-generic pool; with no other Equipment it stays {3} and a one-generic
// pool does not offer it.
func TestPlateArmorEquipDiscount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	e, _ := linkBoard(t, reg, []string{"Plate Armor", "Basilisk Collar", "Lightning Greaves", "Grizzly Bears"}, nil)
	plate := findOnBoard(t, e, 0, "Plate Armor")
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, plate, 0)
	if !ok {
		t.Fatal("Plate Armor's equip not offered for {1} with two other Equipment on the battlefield")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(plate).AttachedTo != bear {
		t.Fatalf("Plate Armor attached to %d, want the bear %d", e.G.Obj(plate).AttachedTo, bear)
	}

	e2, _ := linkBoard(t, reg, []string{"Plate Armor", "Grizzly Bears"}, nil)
	plate2 := findOnBoard(t, e2, 0, "Plate Armor")
	addMana(t, e2, 0, "C")
	if _, ok := findAbilityOption(e2, plate2, 0); ok {
		t.Fatal("Plate Armor's equip offered for {1} with no other Equipment (no discount should apply)")
	}
}

// TestHulkbusterEquipRestriction pins the restriction field: Hulkbuster
// Armor's ability 0 is `K:Equip:3:Hero.YouCtrl`, so its target list must
// contain the Hero creature and NOT a non-Hero creature (ability 1 is the
// unrestricted Equip {6}, untouched).
func TestHulkbusterEquipRestriction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Hulkbuster Armor", "Brave Brawler", "Grizzly Bears"}, nil)
	hulk := findOnBoard(t, e, 0, "Hulkbuster Armor")
	brawler := findOnBoard(t, e, 0, "Brave Brawler")
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	addMana(t, e, 0, "CCC")
	opt, ok := findAbilityOption(e, hulk, 0)
	if !ok {
		t.Fatal("Hulkbuster Armor's restricted equip (ability 0) not offered for {3}")
	}
	if !strings.HasSuffix(opt.Label, "Equip 3") {
		t.Fatalf("ability 0 label = %q, want the restricted Equip 3 line", opt.Label)
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target decision after selecting the restricted equip, got %+v", d)
	}
	offered := map[state.ObjID]bool{}
	for _, o := range d.Options {
		offered[o.Obj] = true
	}
	if !offered[brawler] {
		t.Fatalf("Hero creature %d not offered as an equip target: %+v", brawler, d.Options)
	}
	if offered[bear] {
		t.Fatalf("non-Hero creature %d offered as an equip target of the restricted line: %+v", bear, d.Options)
	}
	targetObject(t, e, brawler)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(hulk).AttachedTo != brawler {
		t.Fatalf("Hulkbuster Armor attached to %d, want the Hero %d", e.G.Obj(hulk).AttachedTo, brawler)
	}
}

// TestEquipActivationLimit pins the rider: Leather Armor's
// `K:Equip:0:::ActivationLimit$ 1` costs {0} but may activate only once each
// turn. The first (free) equip completes; the second activation the SAME turn
// is withheld.
func TestEquipActivationLimit(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Leather Armor", "Grizzly Bears"}, nil)
	la := findOnBoard(t, e, 0, "Leather Armor")
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	addMana(t, e, 0, "") // drive to Main1 priority; the equip is free

	opt, ok := findAbilityOption(e, la, 0)
	if !ok {
		t.Fatal("Leather Armor's free equip not offered")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(la).AttachedTo != bear {
		t.Fatalf("Leather Armor attached to %d, want the bear %d", e.G.Obj(la).AttachedTo, bear)
	}
	if _, ok := findAbilityOption(e, la, 0); ok {
		t.Fatal("Leather Armor's equip offered a second time the same turn (ActivationLimit$ 1 not enforced)")
	}
}
