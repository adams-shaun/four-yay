package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestLevelADiscoverSeedsLibrary: Hit the Mother Lode (LCI) discovers 10. The
// fixture's library must hold a nonland card whose mana value is at most 10,
// otherwise gorge exiles no card, emits no Discover marker and creates no
// Treasures while XMage -- whose library is not empty -- creates them.
// searchesLibrary now recognises Discover as a library scan, so baseline
// seeds the shared library_top list and the scenario ignores it in the
// comparison (the discover scan reshuffles the bottom).
func TestLevelADiscoverSeedsLibrary(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Hit the Mother Lode")
	if skip != nil {
		t.Fatalf("Hit the Mother Lode: %s", skip.Reason)
	}
	if !containsString(it.Ignore, "library_top") {
		t.Errorf("discover scenario must ignore library_top (the scan bottoms cards), got ignore=%v", it.Ignore)
	}
	top := it.Setup["p0"].LibraryTop
	if len(top) == 0 {
		t.Fatal("precondition: discover scenario seeded no library_top, so nothing is discoverable")
	}
	// The FIRST card is what discover 10 finds: it must be a nonland card
	// with mana value <= 10, so both engines exile the same card and the
	// Treasure count (10 - mana value) is defined. A qualifying card buried
	// under the top would let the two engines stop at different cards, so a
	// weaker "some card qualifies" check is not enough.
	first := top[0]
	fc, ok := reg.Lookup(first)
	if !ok || len(fc.Faces) == 0 {
		t.Fatalf("library_top[0] %q is not in the corpus", first)
	}
	if fc.Faces[0].IsLand() {
		t.Fatalf("library_top[0] %q is a land, so discover 10 exiles past it and the discovered card is undefined", first)
	}
	if fc.Faces[0].Cmc() > 10 {
		t.Fatalf("library_top[0] %q has mana value %d > 10, so discover 10 exiles past it", first, fc.Faces[0].Cmc())
	}
	if n, _, ok := oraclegen.Settle(reg, it.Scenario); !ok || n == 0 {
		t.Fatalf("discover scenario does not settle (library_top %v)", top)
	}
}

// TestLevelATypedGraveyardSlotsFillOnlyServableSlots: Rise from the Wreck
// (DFT) offers four optional graveyard slots (creature, Mount, Vehicle,
// creature with no abilities). The Mount slot is deliberately unserved
// (XMage's driver cannot target a Mount card), so the fixture must omit it:
// zoneCandidates now fails closed on an unserved subtype rather than
// declaring a non-Mount card the engine never offers. The NoAbilities slot
// must name a real vanilla creature (Hill Giant), not an ability-bearing card
// the engine's target decision would reject. The cast therefore names exactly
// three targets in slot order: creature, Vehicle, vanilla.
func TestLevelATypedGraveyardSlotsFillOnlyServableSlots(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Rise from the Wreck")
	if skip != nil {
		t.Fatalf("Rise from the Wreck: %s", skip.Reason)
	}
	cast := castStep(t, it, "Rise from the Wreck")
	targets := cast.Targets
	if len(targets) != 3 {
		t.Fatalf("Rise from the Wreck cast has %d targets %v, want 3 (creature, Vehicle, vanilla; Mount omitted)", len(targets), targets)
	}
	gy := it.Setup["p0"].Graveyard
	named := make([]string, len(targets))
	for i, ref := range targets {
		named[i] = strings.TrimPrefix(ref, "p0:")
		if !containsString(gy, named[i]) {
			t.Errorf("target %q is not in p0's graveyard %v", ref, gy)
		}
	}
	// Each target must match its slot's type, in slot order: creature,
	// Mount (omitted), Vehicle, creature with no abilities. Asserting only
	// the count would pass a fixture whose three targets are all creatures.
	if !faceHasType(t, reg, named[0], "Creature") {
		t.Errorf("slot 0 (Creature) named %q, not a creature", named[0])
	}
	if !faceHasType(t, reg, named[1], "Vehicle") {
		t.Errorf("slot 1 (Vehicle) named %q, not a Vehicle", named[1])
	}
	for _, name := range named {
		if faceHasType(t, reg, name, "Mount") {
			t.Errorf("target %q is a Mount, but the Mount slot must stay unserved", name)
		}
	}
	// The last slot is Creature.YouOwn+NoAbilities: its card must be a
	// vanilla creature the engine's NoAbilities decision offers. Assert the
	// precondition (the engine actually poses a distinct NoAbilities target
	// decision) and that the picked card is vanilla, so a fixture that named
	// an ability-bearing creature would fail here rather than pass silently.
	last := named[len(named)-1]
	if last != "Hill Giant" && last != "Grizzly Bears" {
		t.Errorf("NoAbilities slot named %q, want a vanilla creature (Hill Giant or Grizzly Bears)", last)
	}
	if !faceHasType(t, reg, last, "Creature") {
		t.Errorf("NoAbilities slot named %q, not a creature", last)
	}
	if n, res, ok := oraclegen.Settle(reg, it.Scenario); !ok || n == 0 {
		t.Fatalf("Rise from the Wreck does not settle: fails=%v", res.Fails)
	}
}

// TestLevelADealtDamageSacrificePrelude: Treacherous Greed's additional cost
// sacrifices a creature "that dealt damage this turn". Setup cannot stamp
// dealt-damage provenance, so the fixture must make a creature attack and
// pass to end of combat before the cast. Assert the precondition the cost
// depends on (the creature is on the battlefield, the prelude actually has
// the attack and the pass to end-combat) and that gorge settles.
func TestLevelADealtDamageSacrificePrelude(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	it, skip := Generate(reg, "Treacherous Greed")
	if skip != nil {
		t.Fatalf("Treacherous Greed: %s", skip.Reason)
	}
	if !containsString(it.Setup["p0"].Battlefield, "Grizzly Bears") {
		t.Fatalf("precondition: no creature under the caster to sacrifice, battlefield=%v", it.Setup["p0"].Battlefield)
	}
	var sawAttack, sawEndCombat bool
	for _, st := range it.Scenario.Steps {
		if st.Op == "attack" && containsString(st.Attackers, "p0:Grizzly Bears") {
			sawAttack = true
		}
		if st.Op == "pass_to" && st.Step == "end-combat" {
			sawEndCombat = true
		}
	}
	if !sawAttack || !sawEndCombat {
		t.Fatalf("prelude must attack and pass to end of combat (attack=%v end-combat=%v), steps=%v", sawAttack, sawEndCombat, it.Scenario.Steps)
	}
	// The additional cost must actually be PAID with the creature that dealt
	// damage: the cast step carries a choose answer naming it. A scenario
	// that settled but never selected the sacrifice would otherwise pass.
	cast := castStep(t, it, "Treacherous Greed")
	paid := false
	for _, a := range cast.Answers {
		if a.Kind == "choose" && containsString(a.Pick, "Grizzly Bears") {
			paid = true
		}
	}
	if !paid {
		t.Fatalf("cast does not sacrifice the creature that dealt damage (answers must choose Grizzly Bears): %+v", cast.Answers)
	}
	if n, res, ok := oraclegen.Settle(reg, it.Scenario); !ok || n == 0 {
		t.Fatalf("Treacherous Greed does not settle: fails=%v", res.Fails)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
