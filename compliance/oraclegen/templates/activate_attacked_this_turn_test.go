package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateAttackedThisTurnIsServedByAnAttackPrelude: Hexhaven Dueling
// Arena's `{2},{T}: target creature that attacked this turn becomes prepared`
// is sorcery speed, so the scenario declares a p0 attacker, passes to the
// second main phase and only then activates, targeting that attacker.
func TestActivateAttackedThisTurnIsServedByAnAttackPrelude(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Hexhaven Dueling Arena"
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	// Precondition: the ability under test really reads the predicate.
	if got := c.Faces[0].Abilities[1].Params["ValidTgts"]; got != "Creature.attackedThisTurn" {
		t.Fatalf("precondition: %s ability 1 ValidTgts = %q, want Creature.attackedThisTurn", name, got)
	}
	it, _ := activateRequirement(t, reg, name, "activate#0.1")

	var attack, passTo, activate = -1, -1, -1
	for i, st := range it.Scenario.Steps {
		switch st.Op {
		case "attack":
			attack = i
		case "pass_to":
			if st.Step == "main2" {
				passTo = i
			}
		case "activate":
			activate = i
		}
	}
	if attack < 0 || passTo < 0 || activate < 0 || !(attack < passTo && passTo < activate) {
		t.Fatalf("steps attack=%d pass_to main2=%d activate=%d, want attack < pass_to main2 < activate: %+v",
			attack, passTo, activate, it.Scenario.Steps)
	}
	st := it.Scenario.Steps
	if st[attack].Seat != 0 || len(st[attack].Attackers) != 1 {
		t.Fatalf("attack step = %+v, want one p0 attacker", st[attack])
	}
	// The target is the creature that attacked, on p0's side.
	if tg := st[activate].Targets; len(tg) != 1 || tg[0] != st[attack].Attackers[0] {
		t.Fatalf("activate targets = %v, want the attacker %v", tg, st[attack].Attackers)
	}
	// The ability line the generator served is still the real one.
	if len(it.XAbility) != len(st) || it.XAbility[activate] == "" {
		t.Fatalf("xmage_ability = %v, want a non-empty prefix on the activate step", it.XAbility)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("%s activate#0.1 does not play through gorge: ok=%v fails=%v", name, ok, res.Fails)
	}
}

// TestActivateAttackedThisTurnRewriteSparesNegatedAndOppCtrl: only a positive
// attackedThisTurn word is rewritten to "attacking", and an opponent-controlled
// demand stays a named skip (p0's own turn has no opposing attackers).
func TestActivateAttackedThisTurnRewriteSparesNegatedAndOppCtrl(t *testing.T) {
	in := []oraclegen.Slot{
		{Filter: "Creature.attackedThisTurn"},
		{Filter: "Creature.YouCtrl+!attackedThisTurn"},
		{Filter: "Card.Self+attackedThisTurn"},
	}
	out := attackedThisTurnAsAttacking(in)
	want := []string{"Creature.attacking", "Creature.YouCtrl+!attackedThisTurn", "Card.Self+attacking"}
	for i := range want {
		if out[i].Filter != want[i] {
			t.Errorf("rewritten slot %d = %q, want %q", i, out[i].Filter, want[i])
		}
	}
	if in[0].Filter != "Creature.attackedThisTurn" {
		t.Errorf("the caller's slot was mutated to %q", in[0].Filter)
	}
	if slotsDemandAttackedThisTurn([]oraclegen.Slot{{Filter: "Creature.YouCtrl+!attackedThisTurn"}}) {
		t.Error("a negated attackedThisTurn counts as a demand")
	}
}
