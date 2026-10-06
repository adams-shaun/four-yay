package templates

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// activateRequirement finds the named card's level-B activate requirement for
// key.
func activateRequirement(t *testing.T, reg *cards.Registry, name, key string) (oraclegen.Item, levelb.Requirement) {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s: %s", name, key, skip.Reason)
		}
		return it, r
	}
	t.Fatalf("precondition: %s carries no activate requirement %s", name, key)
	return oraclegen.Item{}, levelb.Requirement{}
}

// assertActivateItem checks the shared shape of a generated activate item:
// only activate/resolve ops, the requirement's ability index on the activate
// step, a non-empty XMage prefix on that step and only that step, the level-B
// identity, and a clean gorge replay (PlaysThrough rewrites the fixture
// targets to gorge's own picks, so a surplus target would fail here).
func assertActivateItem(t *testing.T, reg *cards.Registry, name, key, wantPrefix string) {
	t.Helper()
	it, req := activateRequirement(t, reg, name, key)
	wantIdx, err := strconv.Atoi(req.Slot)
	if err != nil {
		t.Fatalf("%s: requirement slot %q is not an index: %v", name, req.Slot, err)
	}
	// Precondition: the indexed ability exists and is a real activated ability.
	c, _ := reg.Lookup(name)
	if req.Face < 0 || req.Face >= len(c.Faces) {
		t.Fatalf("precondition: %s requirement face %d out of range", name, req.Face)
	}
	f := c.Faces[req.Face]
	if wantIdx < 0 || wantIdx >= len(f.Abilities) || !f.Abilities[wantIdx].IsActivated() {
		t.Fatalf("precondition: %s ability %d is not an activated ability", name, wantIdx)
	}
	if it.Template != key || it.Card != name {
		t.Fatalf("%s: item identity = (%s, %s), want (%s, %s)", name, it.Card, it.Template, name, key)
	}
	if len(it.CR) != 1 || it.CR[0] != "602.2" {
		t.Fatalf("%s: CR = %v, want [602.2]", name, it.CR)
	}
	activateSteps := 0
	for i, st := range it.Scenario.Steps {
		switch st.Op {
		case "activate":
			activateSteps++
			if st.AbilityIndex == nil || *st.AbilityIndex != wantIdx {
				t.Fatalf("%s step %d ability_index = %v, want %d", name, i, st.AbilityIndex, wantIdx)
			}
			if i >= len(it.XAbility) || it.XAbility[i] != wantPrefix {
				t.Fatalf("%s step %d xmage_ability = %v, want %q at %d", name, i, it.XAbility, wantPrefix, i)
			}
		case "resolve":
		default:
			t.Fatalf("%s step %d uses op %q; activate v1 emits only activate and resolve", name, i, st.Op)
		}
	}
	if activateSteps != 1 {
		t.Fatalf("%s has %d activate steps, want exactly 1", name, activateSteps)
	}
	if len(it.XAbility) != len(it.Scenario.Steps) {
		t.Fatalf("%s: xmage_ability has %d entries for %d steps", name, len(it.XAbility), len(it.Scenario.Steps))
	}
	for i, xab := range it.XAbility {
		if xab == "" && it.Scenario.Steps[i].Op == "activate" {
			t.Fatalf("%s step %d is an activate with no xmage_ability", name, i)
		}
	}
	// Precondition: the card under test is on p0's battlefield in the setup.
	found := false
	for _, bf := range it.Scenario.Setup["p0"].Battlefield {
		if bf == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: %s is not on p0's battlefield in the setup: %v", name, it.Scenario.Setup["p0"].Battlefield)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("%s does not play through gorge: ok=%v fails=%v", name, ok, res.Fails)
	}
}

// TestActivateManaCreature covers an ordinary mana creature: its {T} mana
// ability is a true mana ability and uses no stack, so the scenario is one
// activate step and no resolve.
func TestActivateManaCreature(t *testing.T) {
	reg := loadGenRegistry(t)
	assertActivateItem(t, reg, "Druid of the Cowl", "activate#0.0", "{T}")
	it, _ := activateRequirement(t, reg, "Druid of the Cowl", "activate#0.0")
	for _, st := range it.Scenario.Steps {
		if st.Op == "resolve" {
			t.Fatal("precondition: Druid of the Cowl's mana ability must not use the stack")
		}
	}
}

// TestActivateTapAbilityWithTarget covers a {T} ability that targets, so the
// fixture supplies a legal target and the rewrite runs.
func TestActivateTapAbilityWithTarget(t *testing.T) {
	reg := loadGenRegistry(t)
	c, _ := reg.Lookup("Axgard Cavalry")
	// Precondition: the ability really declares a target; otherwise the
	// target rewrite this test is about would never run.
	if tgts := c.Faces[0].Abilities[0].Params["ValidTgts"]; tgts == "" {
		t.Fatalf("precondition: Axgard Cavalry's ability has no ValidTgts$")
	}
	assertActivateItem(t, reg, "Axgard Cavalry", "activate#0.0", "{T}")
}

// TestActivateEquip covers a keyword-expanded Equip ability, whose prefix is
// its keyword line rather than a cost line.
func TestActivateEquip(t *testing.T) {
	reg := loadGenRegistry(t)
	c, _ := reg.Lookup("Basilisk Collar")
	if kw := c.Faces[0].Abilities[0].ParamStr(cards.PKKeyword); kw != "Equip" {
		t.Fatalf("precondition: Basilisk Collar's ability keyword = %q, want Equip", kw)
	}
	assertActivateItem(t, reg, "Basilisk Collar", "activate#0.0", "Equip {2}")
}

// TestActivatePlaneswalkerLoyalty covers a planeswalker's loyalty ability:
// the +/-N prefix and a device whose loyalty is high enough for the cost.
func TestActivatePlaneswalkerLoyalty(t *testing.T) {
	reg := loadGenRegistry(t)
	c, _ := reg.Lookup("Ajani, Caller of the Pride")
	f := c.Faces[0]
	if !f.IsPlaneswalker() {
		t.Fatalf("precondition: %s is not a planeswalker", f.Name)
	}
	if f.Loyalty == "" || f.Loyalty == "0" {
		t.Fatalf("precondition: %s printed loyalty = %q", f.Name, f.Loyalty)
	}
	assertActivateItem(t, reg, "Ajani, Caller of the Pride", "activate#0.0", "+1")
	assertActivateItem(t, reg, "Ajani, Caller of the Pride", "activate#0.1", "-3")
}

// TestActivateSacrificeSelf covers a cost of Sac<1/CARDNAME>: the source
// sacrifices itself and the ability is an ordinary (non-mana) ability that
// resolves.
func TestActivateSacrificeSelf(t *testing.T) {
	reg := loadGenRegistry(t)
	c, _ := reg.Lookup("Cathar Commando")
	if cost := c.Faces[0].Abilities[0].ParamStr(cards.PKCost); !sacSelf(cost) {
		t.Fatalf("precondition: Cathar Commando cost %q is not Sac<.../CARDNAME>", cost)
	}
	assertActivateItem(t, reg, "Cathar Commando", "activate#0.0",
		"{1}, Sacrifice {this}")
	it, _ := activateRequirement(t, reg, "Cathar Commando", "activate#0.0")
	resolves := 0
	for _, st := range it.Scenario.Steps {
		if st.Op == "resolve" {
			resolves++
		}
	}
	if resolves == 0 {
		t.Fatal("precondition: a non-mana ability needs at least one resolve step")
	}
}

// TestActivateLoyaltyManaUsesStack pins the CR 605.1b subtlety: a Mana-API
// ability with a loyalty cost (Chandra, Flameshaper's "+2: Add {R}{R}{R}")
// is not a mana ability, so its scenario carries a resolve step even though
// its sub-family is activate.mana.
func TestActivateLoyaltyManaUsesStack(t *testing.T) {
	reg := loadGenRegistry(t)
	it, req := activateRequirement(t, reg, "Chandra, Flameshaper", "activate#0.0")
	if req.Sub != "activate.mana" {
		t.Fatalf("precondition: sub = %q, want activate.mana", req.Sub)
	}
	if cost := it.Scenario.Setup["p0"].Battlefield; len(cost) == 0 {
		t.Fatal("precondition: Chandra is not in the setup")
	}
	resolves := 0
	for _, st := range it.Scenario.Steps {
		if st.Op == "resolve" {
			resolves++
		}
	}
	if resolves == 0 {
		t.Fatal("a loyalty-cost mana ability uses the stack and needs a resolve step")
	}
	assertActivateItem(t, reg, "Chandra, Flameshaper", "activate#0.0", "+2")
}

// TestActivateCostGapNamesTheToken checks the v1 cost whitelist's fail-closed
// direction: an ability whose cost carries an unmodelled token skips with the
// token's head named, never generating a scenario that would silently drop it.
func TestActivateCostGapNamesTheToken(t *testing.T) {
	if pool, gap := activationCost("2 T Discard<1/CARDNAME>"); gap != "Discard<...>" || pool != "" {
		t.Fatalf("Discard cost = (%q, %q), want gap Discard<...>", pool, gap)
	}
	if pool, gap := activationCost("1 Sac<1/CARDNAME/this creature>"); gap != "" || pool != "C" {
		t.Fatalf("Sac-self cost = (%q, %q), want pool C", pool, gap)
	}
	if pool, gap := activationCost("PayLife<2>"); gap != "" || pool != "" {
		t.Fatalf("PayLife cost = (%q, %q), want no pool and no gap", pool, gap)
	}
	if pool, gap := activationCost("SubCounter<3/LOYALTY>"); gap != "" || pool != "" {
		t.Fatalf("loyalty cost = (%q, %q), want no pool and no gap", pool, gap)
	}
}
