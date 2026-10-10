// Level-B combat/keyword and static-observation remainder recipes: one real
// card per served shape. Each row's requirement must generate, play through
// gorge, and show the row's own trigger on the stack, so a stack entry from a
// sibling trigger cannot pass for the row.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestCombatRemainderTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, sub, slot, faceName string }{
		{"Assimilation Aegis", "trigger#0.1", "trigger.attached", "1", ""},
		{"Blade of Shared Souls", "trigger#0.0", "trigger.attached", "0", ""},
		{"Enormous Energy Blade", "trigger#0.0", "trigger.attached", "0", ""},
		{"Inchblade Companion", "trigger#0.0", "trigger.attached", "0", ""},
		{"Bramble Elemental", "trigger#0.0", "trigger.attached", "0", ""},
		{"Brood Keeper", "trigger#0.0", "trigger.attached", "0", ""},
		{"Siona, Captain of the Pyleas", "trigger#0.1", "trigger.attached", "1", ""},
		{"The Millennium Calendar", "trigger#0.0", "trigger.untap-all", "0", ""},
		{"Magmatic Galleon", "trigger#0.1", "trigger.excess-damage", "1", ""},
		{"Tomik, Wielder of Law", "trigger#0.0", "trigger.opponent-attacks", "0", ""},
		{"Party Dude", "trigger#0.2", "trigger.attacks-opponent", "2", ""},
		{"Spider-Mobile", "trigger#0.1", "trigger.blocks-vehicle", "1", ""},
		{"Gideon the Oathless", "trigger#0.1", "trigger.ability-activated-opponent", "1", ""},
		{"Firebender Ascension", "trigger#0.1", "trigger.ability-triggered", "1", ""},
		{"Case File Auditor", "trigger#0.1", "trigger.case-solved", "1", ""},
		// Unstable Glyphbridge's back-face trigger, served since
		// agent-20261009T174731Z-42d5e0f4 read the dotless
		// `Player.Opponent+Active` clause: the cause is a main1@p1 pass and
		// p1's own cast. The permanent stands on its back face, so the stack
		// entry's source is reported under "Sandswirl Wanderglyph"
		// (rules/oracle_snapshot.go stackSourceRef); the row carries it.
		{"Unstable Glyphbridge", "trigger#1.0", "trigger.spell-cast-opponent-active", "0", "Sandswirl Wanderglyph"},
	} {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !combatRemainderOnStack(t, reg, it.Scenario, tc.name, tc.slot, tc.faceName) {
				t.Fatalf("%s's trigger never appears on stack: %+v", tc.name, it.Scenario.Steps)
			}
		})
	}
}

// combatRemainderOnStack replays the item with the cause steps (trailing
// resolves dropped: a resolve op empties the stack, spending the trigger it
// was meant to expose) and with one pass round appended, and reports whether
// any snapshot shows the row's own trigger on the stack. extra names are
// additional source-name prefixes a row's stack entry may report (a back-face
// source reports under its own face's name).
func combatRemainderOnStack(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name, slot string, extra ...string) bool {
	t.Helper()
	cause := sc.Steps
	for len(cause) > 0 && cause[len(cause)-1].Op == "resolve" {
		cause = cause[:len(cause)-1]
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	variants := [][]oraclegen.Step{
		cause,
		append(append([]oraclegen.Step(nil), cause...), passes...),
	}
	wants := append([]string{strings.ToLower(name)}, extra...)
	for i := range wants {
		wants[i] = strings.ToLower(wants[i])
	}
	for _, steps := range variants {
		res := runSteps(t, reg, sc, steps)
		if abilityOnStack(res.Snapshots, wants, slot) {
			return true
		}
	}
	return false
}

// TestCombatRemainderCauseDetails pins what the ops alone do not say: who
// attacks and with how many, which seat activates, and where the cause's
// pass_to lands.
func TestCombatRemainderCauseDetails(t *testing.T) {
	reg := loadGenRegistry(t)
	step := func(it oraclegen.Item, op string) []oraclegen.Step {
		var out []oraclegen.Step
		for _, st := range it.Scenario.Steps {
			if st.Op == op {
				out = append(out, st)
			}
		}
		return out
	}
	// Tomik's CheckSVar$ GE2 floor: two creatures attack p0, both p1's.
	tomik := triggerRequirement(t, reg, "Tomik, Wielder of Law", "trigger#0.0", "trigger.opponent-attacks")
	attacks := step(tomik, "attack")
	if len(attacks) != 1 || len(attacks[0].Attackers) < 2 || attacks[0].Defender != "p0" {
		t.Fatalf("Tomik attack = %+v, want one attack at p0 with at least two attackers", attacks)
	}
	for _, a := range attacks[0].Attackers {
		if !strings.HasPrefix(a, "p1:") {
			t.Fatalf("Tomik attacker %q is not p1's", a)
		}
	}
	// Party Dude's cause is p0's own attack, with the chosen attacker on the board.
	dude := triggerRequirement(t, reg, "Party Dude", "trigger#0.2", "trigger.attacks-opponent")
	dudeAttack := step(dude, "attack")
	if len(dudeAttack) != 1 || dudeAttack[0].Seat != 0 || dudeAttack[0].Defender != "p1" || len(dudeAttack[0].Attackers) != 1 {
		t.Fatalf("Party Dude attack = %+v, want one p0 attacker at p1", dudeAttack)
	}
	atkName := strings.TrimPrefix(dudeAttack[0].Attackers[0], "p0:")
	if !inZone(dude.Scenario.Setup["p0"].Battlefield, atkName) {
		t.Fatalf("Party Dude attacker %q is not on p0's battlefield: %+v", atkName, dude.Scenario.Setup["p0"].Battlefield)
	}
	// Spider-Mobile's cause crews itself (activate@0) after p1's attack and
	// before the block.
	spider := triggerRequirement(t, reg, "Spider-Mobile", "trigger#0.1", "trigger.blocks-vehicle")
	spiderAttack := step(spider, "attack")
	spiderActivate := step(spider, "activate")
	spiderBlock := step(spider, "block")
	if len(spiderAttack) != 1 || spiderAttack[0].Seat != 1 {
		t.Fatalf("Spider-Mobile attack = %+v, want p1's attack", spiderAttack)
	}
	if len(spiderActivate) != 1 || spiderActivate[0].Seat != 0 || spiderActivate[0].Card != "p0:Spider-Mobile" {
		t.Fatalf("Spider-Mobile activate = %+v, want p0 crewing the source", spiderActivate)
	}
	if len(spiderBlock) != 1 || spiderBlock[0].Seat != 0 || spiderBlock[0].Blocks[0][0] != "p0:Spider-Mobile" {
		t.Fatalf("Spider-Mobile block = %+v, want p0's Vehicle blocking", spiderBlock)
	}
	// Gideon's cause activates a probe planeswalker for p1 during p1's main.
	gideon := triggerRequirement(t, reg, "Gideon the Oathless", "trigger#0.1", "trigger.ability-activated-opponent")
	gideonActivate := step(gideon, "activate")
	if len(gideonActivate) != 1 || gideonActivate[0].Seat != 1 {
		t.Fatalf("Gideon activate = %+v, want p1's loyalty activation", gideonActivate)
	}
	if len(gideon.Scenario.Setup["p1"].Battlefield) == 0 {
		t.Fatalf("Gideon has no planeswalker on p1's battlefield: %+v", gideon.Scenario.Setup["p1"])
	}
	// Unstable Glyphbridge's back-face trigger is served now
	// (agent-20261009T174731Z-42d5e0f4 read the dotless
	// `Player.Opponent+Active` clause): the cause passes to p1's main phase
	// and p1 casts there -- the opponent casts during its OWN turn, so no
	// extra pass is needed.
	glb := triggerRequirement(t, reg, "Unstable Glyphbridge", "trigger#1.0", "trigger.spell-cast-opponent-active")
	glbPasses := step(glb, "pass_to")
	if len(glbPasses) != 1 || glbPasses[0].Step != "main1" || glbPasses[0].Active != "p1" {
		t.Fatalf("Glyphbridge pass_to = %+v, want exactly main1@p1", glbPasses)
	}
	glbCasts := step(glb, "cast")
	if len(glbCasts) != 1 || glbCasts[0].Seat != 1 || !strings.HasPrefix(glbCasts[0].Card, "p1:") {
		t.Fatalf("Glyphbridge cast = %+v, want one p1 cast", glbCasts)
	}
	// The untap cause taps a probe with the tap spell and waits for p0's next
	// untap step.
	cal := triggerRequirement(t, reg, "The Millennium Calendar", "trigger#0.0", "trigger.untap-all")
	casts := step(cal, "cast")
	if len(casts) != 1 || len(casts[0].Targets) != 1 || casts[0].Targets[0] != "p0:"+bearsProbe {
		t.Fatalf("Millennium Calendar cast = %+v, want the tap spell at p0:%s", casts, bearsProbe)
	}
	if !inZone(cal.Scenario.Setup["p0"].Battlefield, bearsProbe) {
		t.Fatalf("Millennium Calendar has no probe on p0's battlefield: %+v", cal.Scenario.Setup["p0"])
	}
	passes := step(cal, "pass_to")
	if len(passes) != 1 || passes[0].Step != "upkeep" || passes[0].Active != "p0" {
		t.Fatalf("Millennium Calendar pass_to = %+v, want upkeep@p0", passes)
	}
	// The excess-damage cause is Shock at p1's 1-toughness probe.
	gal := triggerRequirement(t, reg, "Magmatic Galleon", "trigger#0.1", "trigger.excess-damage")
	galCasts := step(gal, "cast")
	if len(galCasts) != 1 || len(galCasts[0].Targets) != 1 || galCasts[0].Targets[0] != "p1:"+excessDamageProbe {
		t.Fatalf("Magmatic Galleon cast = %+v, want Shock at p1:%s", galCasts, excessDamageProbe)
	}
	// The Equipment cause attaches the source itself to a probe bearer.
	aegis := triggerRequirement(t, reg, "Assimilation Aegis", "trigger#0.1", "trigger.attached")
	attaches := step(aegis, "attach")
	if len(attaches) != 1 || attaches[0].Card != "p0:Assimilation Aegis" || attaches[0].AttachedTo != "p0:"+bearsProbe {
		t.Fatalf("Assimilation Aegis attach = %+v, want the source attached to %s", attaches, bearsProbe)
	}
}

// TestCombatRemainderNamedGaps: the shapes with no cause the recipes can
// build stay named gaps -- Eriette's TargetRelativeToSource$ Attached line
// (the engine's matcher never fires without a ValidTarget$) and the Static$
// True Attached lines.
func TestCombatRemainderNamedGaps(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Eriette, the Beguiler", "trigger#0.0"},
		{"Metamorphic Alteration", "trigger#0.0"},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.name)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Gap == "" {
				t.Fatalf("%s %s = %+v, want a gap", tc.name, tc.key, r)
			}
			if _, skip := GenerateB(reg, tc.name, r); skip == nil {
				t.Fatalf("%s %s generated an item", tc.name, tc.key)
			}
		}
		if !found {
			t.Fatalf("%s carries no requirement %s", tc.name, tc.key)
		}
	}
}
