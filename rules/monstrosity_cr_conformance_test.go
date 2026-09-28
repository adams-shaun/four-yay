package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestCR70134MonstrosityDesignation checks CR 701.34a-b: the action places
// the specified counters and makes the permanent monstrous, while another
// attempt does not perform the action again (CR 701.34b).
func TestCR70134MonstrosityDesignation(t *testing.T) {
	t.Parallel()
	e, cfg, id := monstrosityEngine(t, "Giggling Skitterspike", "CCCCC")
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Monstrous {
		t.Fatalf("precondition: source zone=%v monstrous=%v", e.G.Obj(id).Zone, e.G.Obj(id).Monstrous)
	}
	idx := monstrosityAbilityIndex(t, e, id)
	if _, ok := findAbilityOption(e, id, idx); !ok {
		t.Fatal("precondition: Monstrosity 5 ability is not offered")
	}
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(id).Monstrous || e.G.Obj(id).Counter("P1P1") != 5 {
		t.Fatalf("CR 701.34a: monstrous=%v counters=%d, want true and 5", e.G.Obj(id).Monstrous, e.G.Obj(id).Counter("P1P1"))
	}
	marks := monstrousMarkEvents(e)
	if len(marks) != 1 || marks[0].Amount != 5 {
		t.Fatalf("CR 701.34a: designation events=%+v, want one event carrying 5", marks)
	}
	addMana(t, e, 0, "CCCCC")
	if _, ok := findAbilityOption(e, id, idx); ok {
		t.Fatalf("CR 701.34b: Monstrosity was offered again: %+v", e.Pending().Options)
	}
	if len(monstrousMarkEvents(e)) != 1 {
		t.Fatalf("CR 701.34b: second attempt added a designation event: %+v", monstrousMarkEvents(e))
	}
	replayCheck(t, e, cfg)
}

// TestHydraBroodmasterMonstrosityCreatesXTokens exercises the requested
// Hydra Broodmaster path end to end, including TriggerCount$Amount flowing
// through MonstrosityX into token count and token power/toughness.
func TestHydraBroodmasterMonstrosityCreatesXTokens(t *testing.T) {
	t.Parallel()
	e, cfg, id := monstrosityEngine(t, "Hydra Broodmaster", "CCCCCG")
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Monstrous {
		t.Fatalf("precondition: source zone=%v monstrous=%v", e.G.Obj(id).Zone, e.G.Obj(id).Monstrous)
	}
	idx := monstrosityAbilityIndex(t, e, id)
	opt, ok := findAbilityOption(e, id, idx)
	if !ok {
		t.Fatal("precondition: Hydra Broodmaster's Monstrosity X ability is not offered")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want X announcement, got %+v", d)
	}
	x2 := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 2 {
			x2 = o.Index
		}
	}
	if x2 < 0 {
		t.Fatalf("X=2 not offered: %+v", d.Options)
	}
	submitChoices(t, e, x2)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(id).Monstrous || e.G.Obj(id).Counter("P1P1") != 2 {
		t.Fatalf("Hydra Broodmaster after X=2: monstrous=%v counters=%d", e.G.Obj(id).Monstrous, e.G.Obj(id).Counter("P1P1"))
	}
	var hydras []state.ObjID
	for _, oid := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(oid)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Hydra Token" {
			hydras = append(hydras, oid)
		}
	}
	if len(hydras) != 2 {
		var board []string
		for _, oid := range e.G.Zone(state.ZBattlefield, 0) {
			o := e.G.Obj(oid)
			if o != nil && o.Face() != nil {
				board = append(board, o.Face().Name)
			}
		}
		t.Fatalf("Hydra Broodmaster X=2 created %d Hydra tokens, want 2; battlefield=%v", len(hydras), board)
	}
	for _, hid := range hydras {
		p, toughness, _ := e.Characteristics(hid)
		if p != 2 || toughness != 2 {
			t.Fatalf("Hydra token %d is %d/%d, want 2/2", hid, p, toughness)
		}
	}
	if marks := monstrousMarkEvents(e); len(marks) != 1 || marks[0].Amount != 2 {
		t.Fatalf("Hydra trigger amount = %+v, want one mark carrying X=2", marks)
	}
	replayCheck(t, e, cfg)
}

// TestPolukranosMonstrosityTriggerReadsX checks the other TriggerCount$Amount
// carrier: its BecomeMonstrous trigger's target/damage ask is bounded by the
// X recorded on the designation event, rather than the ability's later frame.
func TestPolukranosMonstrosityTriggerReadsX(t *testing.T) {
	t.Parallel()
	e, _, id := monstrosityEngine(t, "Polukranos, World Eater", "CCCCCG")
	bear := crAbortMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Monstrous || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Polukranos=%+v opponent creature=%+v", e.G.Obj(id), e.G.Obj(bear))
	}
	idx := monstrosityAbilityIndex(t, e, id)
	opt, ok := findAbilityOption(e, id, idx)
	if !ok {
		t.Fatal("precondition: Polukranos's Monstrosity X ability is not offered")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want X announcement, got %+v", d)
	}
	x2 := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 2 {
			x2 = o.Index
		}
	}
	if x2 < 0 {
		t.Fatalf("X=2 not offered: %+v", d.Options)
	}
	submitChoices(t, e, x2)
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want Polukranos BecomeMonstrous target ask, got %+v", d)
	}
	if d.Max != 1 {
		t.Fatalf("with one legal opponent creature, TriggerCount$Amount=2 should permit the creature target (bounded by pool to 1), got Max=%d options=%+v", d.Max, d.Options)
	}
	foundBear := false
	for _, o := range d.Options {
		if o.Obj == bear {
			foundBear = true
		}
	}
	if !foundBear {
		t.Fatalf("Polukranos trigger did not offer opponent's creature: %+v", d.Options)
	}
	if len(monstrousMarkEvents(e)) != 1 || monstrousMarkEvents(e)[0].Amount != 2 {
		t.Fatalf("Polukranos trigger has no X=2 designation binding: %+v", monstrousMarkEvents(e))
	}
	bearIndex := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			bearIndex = o.Index
		}
	}
	submitChoices(t, e, bearIndex)
	passUntilStackEmpty(t, e, 30)
	if got := bearDamage(e, bear); got != 2 {
		t.Fatalf("Polukranos trigger dealt %d damage to the chosen creature, want X=2", got)
	}
	// The damage amount proves the body read the designation's X, not the
	// later trigger-frame default. The return-damage subability is separate.
}
