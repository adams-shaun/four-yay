package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The gated combat-legality templates (cli-20261009T031407Z-6fac1771). Each
// test pins one rows-file card's generated item and proves the observation is
// not vacuous: the gate-held run holds, and the flipped polarity (the gate
// unheld with the expectation inverted) fails or holds as the engine's gate
// dictates. A template or engine change that breaks the gate shows up as a
// fails list here.

// gatedCardAt is the scenario ref of the card under test.
func gatedCardAt(name string) string { return cardAt(0, name) }

// lastExpect returns the pending decision's assertion list.
func lastExpect(it oraclegen.Item) []oraclegen.Expect {
	return it.Steps[len(it.Steps)-1].Expect
}

// TestGatedUnblockableNightwhorlHermit pins the Threshold-gated unblockable
// (BLB Nightwhorl Hermit): with six or fewer grave cards the Hermit may not
// be blocked by the probe blocker, though the same blocker still blocks the
// vanilla attacker beside it; the seven-card control offers the block.
func TestGatedUnblockableNightwhorlHermit(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Nightwhorl Hermit", "static#0.1", "static.cant-block-by-gated")
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: gate-held scenario does not hold: %v", res.Fails)
	}
	found := false
	for _, e := range lastExpect(it) {
		if e.CanBlock != nil && e.CanBlock.Attacker == gatedCardAt("Nightwhorl Hermit") &&
			e.Want != nil && !*e.Want {
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: item lacks the Hermit unblockable assertion")
	}
	ctl := it.Scenario
	p0 := ctl.Setup["p0"]
	p0.Graveyard = graveSeven[:6]
	ctl.Setup["p0"] = p0
	ctl.Steps = append([]oraclegen.Step(nil), ctl.Steps...)
	last := &ctl.Steps[len(ctl.Steps)-1]
	last.Expect = append([]oraclegen.Expect(nil), last.Expect...)
	for i := range last.Expect {
		if last.Expect[i].CanBlock != nil && last.Expect[i].CanBlock.Attacker == gatedCardAt("Nightwhorl Hermit") {
			last.Expect[i].Want = boolPtr(true)
		}
	}
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: one-card-short control does not offer the block: %v", res.Fails)
	}
}

// TestGatedCantAttackPatchworkBeastie pins the delirium-gated can't-attack
// (DSK Patchwork Beastie): with one grave card (delirium unmet) the attack is
// unoffered; the four-distinct-type control offers it.
func TestGatedCantAttackPatchworkBeastie(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Patchwork Beastie", "static#0.0", "static.cant-attack-gated")
	if len(it.Setup["p0"].Graveyard) != 1 {
		t.Fatalf("precondition: gate-held setup grave is %v, want the one untyped card", it.Setup["p0"].Graveyard)
	}
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: gate-held scenario does not hold: %v", res.Fails)
	}
	ctl := it.Scenario
	p0 := ctl.Setup["p0"]
	p0.Graveyard = deliriumGrave
	ctl.Setup["p0"] = p0
	ctl.Steps = append([]oraclegen.Step(nil), ctl.Steps...)
	last := &ctl.Steps[len(ctl.Steps)-1]
	last.Expect = append([]oraclegen.Expect(nil), last.Expect...)
	for i := range last.Expect {
		if last.Expect[i].CanAttack != nil && last.Expect[i].CanAttack.Attacker == gatedCardAt("Patchwork Beastie") {
			last.Expect[i].Want = boolPtr(true)
		}
	}
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: delirium control does not offer the attack: %v", res.Fails)
	}
}

// TestGatedCanAttackDefenderBristlepack pins the present-gated defender lift
// (OTJ Bristlepack Sentry): with a power-4+ creature beside it the Sentry's
// attack op succeeds and reaches the attacking state; without it the op is
// refused.
func TestGatedCanAttackDefenderBristlepack(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Bristlepack Sentry", "static#0.0", "static.can-attack-defender-gated")
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 || !onBattlefield(res, "p0:Craw Wurm") {
		t.Fatalf("precondition: gate-held attack did not replay: %v", res.Fails)
	}
	ctl := it.Scenario
	p0 := ctl.Setup["p0"]
	kept := p0.Battlefield[:0]
	for _, b := range p0.Battlefield {
		if b != "Craw Wurm" {
			kept = append(kept, b)
		}
	}
	p0.Battlefield = kept
	ctl.Setup["p0"] = p0
	if res, _ := runStatic(reg, ctl); !wallAttackRefused(res.Fails) {
		t.Fatalf("precondition: gate-unheld control did not refuse the attack: fails=%v", res.Fails)
	}
}

// TestMaxBlockersSafewrightCavalry pins the MinMaxBlocker Max$ bound (ECL
// Safewright Cavalry): the capped attacker's pair options publish max 1 and
// the vanilla probe attacker's publish none.
func TestMaxBlockersSafewrightCavalry(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Safewright Cavalry", "static#0.0", "static.max-blockers")
	expect := lastExpect(it)
	bound, plain := false, false
	for _, e := range expect {
		if e.CanBlock == nil || e.Want == nil || !*e.Want {
			continue
		}
		if e.CanBlock.Attacker == gatedCardAt("Safewright Cavalry") && e.CanBlock.MaxBlockers != nil && *e.CanBlock.MaxBlockers == 1 {
			bound = true
		}
		if e.CanBlock.Attacker == cardAt(0, largeAttackerProbe) && e.CanBlock.MaxBlockers == nil {
			plain = true
		}
	}
	if !bound || !plain {
		t.Fatalf("precondition: item lacks the bound pair assertion (bound=%v plain=%v): %+v", bound, plain, expect)
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: bound scenario does not hold: %v", res.Fails)
	}
}

// TestMustAttackRedHerring pins the MustAttack requirement flag (MKM Red
// Herring): the card's attacker option carries it, the probe's does not, and
// flipping the card's assertion makes the engine fail the run.
func TestMustAttackRedHerring(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Red Herring", "static#0.0", "static.must-attack-self")
	req, probeReq := false, false
	for _, e := range lastExpect(it) {
		if e.AttackRequired == nil || e.Want == nil {
			continue
		}
		switch e.AttackRequired.Attacker {
		case gatedCardAt("Red Herring"):
			req = *e.Want
		case cardAt(0, smallAttackerProbe):
			probeReq = !*e.Want
		}
	}
	if !req || !probeReq {
		t.Fatalf("precondition: item lacks the requirement assertions (card=%v probe-unrequired=%v)", req, probeReq)
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: requirement scenario does not hold: %v", res.Fails)
	}
	ctl := it.Scenario
	ctl.Steps = append([]oraclegen.Step(nil), ctl.Steps...)
	last := &ctl.Steps[len(ctl.Steps)-1]
	last.Expect = append([]oraclegen.Expect(nil), last.Expect...)
	for i := range last.Expect {
		if last.Expect[i].AttackRequired != nil && last.Expect[i].AttackRequired.Attacker == gatedCardAt("Red Herring") {
			last.Expect[i].Want = boolPtr(false)
		}
	}
	if res, ok := runStatic(reg, ctl); !ok || !failsName(res.Fails, "Red Herring must attack") {
		t.Fatalf("precondition: flipped requirement assertion was not enforced: %v", res.Fails)
	}
}

// TestGatedCrimeUnblockableNimbleBrigand pins the crime-gated unblockable
// (OTJ Nimble Brigand): the scenario commits a crime (Shock at p1's Wall)
// before combat, the Brigand is then unblockable, and the no-crime control
// offers the block.
func TestGatedCrimeUnblockableNimbleBrigand(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Nimble Brigand", "static#0.0", "static.cant-block-by-gated")
	cast := false
	for _, st := range it.Steps {
		if st.Op == "cast" && st.Card == "p0:Shock" {
			cast = true
		}
	}
	if !cast {
		t.Fatal("precondition: item lacks the crime-committing cast step")
	}
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: gate-held scenario does not hold: %v", res.Fails)
	}
	ctl := it.Scenario
	ctl.Steps = append([]oraclegen.Step(nil), ctl.Steps...)
	// Drop the gate steps (mana, cast, resolve) and flip the expectation.
	ctl.Steps = ctl.Steps[3:]
	last := &ctl.Steps[len(ctl.Steps)-1]
	last.Expect = append([]oraclegen.Expect(nil), last.Expect...)
	for i := range last.Expect {
		if last.Expect[i].CanBlock != nil && last.Expect[i].CanBlock.Attacker == gatedCardAt("Nimble Brigand") {
			last.Expect[i].Want = boolPtr(true)
		}
	}
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: no-crime control does not offer the block: %v", res.Fails)
	}
}
