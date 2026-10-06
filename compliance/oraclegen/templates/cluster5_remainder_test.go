package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestGracefulTakedownFixtureAndResolution locks the legal board shape and
// the damage result. The opponent's 2/2 is a real target, and the enchanted
// creature (pumped by its Aura) plus the other chosen creature each deal damage
// to it, enough to destroy it (Oracle text; CR 120.3e, then CR 704.5g moves the
// lethal-damaged creature to its owner's graveyard).
func TestGracefulTakedownFixtureAndResolution(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Graceful Takedown")
	if skip != nil {
		t.Fatalf("Graceful Takedown: %s", skip.Reason)
	}

	var bearer string
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" && st.Card != "p0:Graceful Takedown" && len(st.Targets) == 1 {
			bearer = st.Targets[0]
		}
	}
	if bearer == "" || !strings.HasPrefix(bearer, "p0:") || !containsName(it.Setup["p0"].Battlefield, strings.TrimPrefix(bearer, "p0:")) {
		t.Fatalf("Aura prelude target %q is not on p0 battlefield %v", bearer, it.Setup["p0"].Battlefield)
	}
	cast := castStep(t, it, "Graceful Takedown")
	if len(cast.Targets) != 3 || cast.Targets[0] != bearer || !strings.HasPrefix(cast.Targets[2], "p1:") {
		t.Fatalf("Graceful Takedown targets %v; want enchanted p0 creature, optional p0 creature, and p1 creature", cast.Targets)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("Graceful Takedown scenario did not resolve cleanly: ok=%v failures=%v", ok, res.Fails)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if !containsName(final.Players[1].Graveyard, "Grizzly Bears") {
		t.Fatalf("Graceful Takedown failed to destroy its opponent's targeted Grizzly Bears: p1 graveyard=%v", final.Players[1].Graveyard)
	}
}

// TestSorceressSchemesMixedZoneFixture proves the OR-zone selector produces
// a castable card matching one of the spell's supported graveyard or exile alternatives.
func TestSorceressSchemesMixedZoneFixture(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Sorceress's Schemes")
	if skip != nil {
		t.Fatalf("Sorceress's Schemes: %s", skip.Reason)
	}
	cast := castStep(t, it, "Sorceress's Schemes")
	if len(cast.Targets) != 1 || !strings.HasPrefix(cast.Targets[0], "p0:") {
		t.Fatalf("Sorceress's Schemes targets=%v; want one card owned by p0", cast.Targets)
	}
	target := strings.TrimPrefix(cast.Targets[0], "p0:")
	if !containsName(it.Setup["p0"].Graveyard, target) && !containsName(it.Setup["p0"].Exile, target) {
		t.Fatalf("target %q is absent from p0 graveyard %v and exile %v", target, it.Setup["p0"].Graveyard, it.Setup["p0"].Exile)
	}
	if !faceHasType(t, reg, target, "Instant") && !faceHasType(t, reg, target, "Sorcery") {
		t.Fatalf("target %q is neither an instant nor a sorcery", target)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("Sorceress's Schemes scenario did not resolve cleanly: ok=%v failures=%v", ok, res.Fails)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if !containsName(final.Players[0].Hand, target) || !strings.Contains(final.Players[0].Pool, "R") {
		t.Fatalf("Sorceress's Schemes result: hand=%v pool=%q; want returned %s and red mana", final.Players[0].Hand, final.Players[0].Pool, target)
	}
}
