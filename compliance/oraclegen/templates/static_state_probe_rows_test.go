package templates

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// Tests for the level-B static-row fixtures this ticket added (level-B class
// G7 "static effect unobservable", rows file g7c-static-fixtures): the
// CantBlockBy blocker-filter search's Glimmer probe and its alive-lands
// fixture for a characteristic-defining P/T, and the equip fixture's
// "modified" host. Each test runs the row's own scenario, so a fix that
// stops replaying fails loudly.

// TestCantBlockByGlimmerProbeServes serves Cynical Loner's "can't be blocked
// by Glimmers": the search now fields a Glimmer blocker beside a vanilla
// one, and the scenario asserts the split (the Glimmer may block the vanilla
// attacker but not the card; the vanilla blocker may block the card).
func TestCantBlockByGlimmerProbeServes(t *testing.T) {
	reg := loadGenRegistry(t)
	gl, ok := reg.Lookup("Enduring Innocence")
	if !ok || len(gl.Faces) == 0 {
		t.Fatal("precondition: the Glimmer blocker probe is not in the corpus")
	}
	if !slices.Contains(gl.Faces[0].Types, "Glimmer") {
		t.Fatal("precondition: the Glimmer blocker probe is not a Glimmer")
	}
	if slices.Contains(filterBlockerProbes, "Enduring Innocence") {
		t.Fatal("the Glimmer probe belongs in the after-vanilla split list, not the vanilla order")
	}
	it := staticItemFor(t, reg, "Cynical Loner", "static#0.0", "static.cant-block-by-blocker-filter")
	p1, ok := it.Scenario.Setup["p1"]
	if !ok || !slices.Contains(p1.Battlefield, "Enduring Innocence") {
		t.Fatalf("scenario never fields the Glimmer blocker: %+v", p1)
	}
	got := map[string]bool{}
	last := it.Steps[len(it.Steps)-1]
	for _, e := range last.Expect {
		if e.CanBlock == nil || e.Want == nil {
			continue
		}
		got[e.CanBlock.Blocker+"->"+e.CanBlock.Attacker] = *e.Want
	}
	if !got["p1:Enduring Innocence->"+cardAt(0, largeAttackerProbe)] {
		t.Fatalf("the Glimmer blocker is not asserted against the vanilla attacker: %v", got)
	}
	if v, ok := got["p1:Enduring Innocence->p0:Cynical Loner"]; !ok || v {
		t.Fatalf("the Glimmer blocker is not asserted unable to block the card: %v", got)
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("the served scenario does not replay: %v", res.Fails)
	}
}

// TestCantBlockByAliveLandsServes serves Sandman's power<=2 blocker filter:
// the card's P/T is a characteristic-defining count of lands you control, so
// the fixture keeps it alive with Wastes on p0's battlefield and the row's
// blocker pair asserts the split.
func TestCantBlockByAliveLandsServes(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Sandman, Shifting Scoundrel")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("precondition: Sandman is not in the corpus")
	}
	if c.Faces[0].PT != "*/*" {
		t.Fatalf("precondition: Sandman's P/T is not variable: %q", c.Faces[0].PT)
	}
	if n := cantBlockByAliveLands(c.Faces[0]); n < 1 {
		t.Fatal("precondition: the face's characteristic-defining P/T does not count lands")
	}
	it := staticItemFor(t, reg, "Sandman, Shifting Scoundrel", "static#0.1", "static.cant-block-by-blocker-filter")
	p0, ok := it.Scenario.Setup["p0"]
	if !ok || !slices.Contains(p0.Battlefield, "Wastes") {
		t.Fatalf("scenario leaves the characteristic-defining card without lands: %+v", p0)
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("the served scenario does not replay: %v", res.Fails)
	}
}

// TestStaticModifiedEquipFixtureServes serves Skyward Spider's "has flying
// as long as it's modified": the equip fixture now also answers the
// "modified" affected word, attaching the Equipment to the card itself and
// carrying its +2/+0 as the compared spec's own shift.
func TestStaticModifiedEquipFixtureServes(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Skyward Spider")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("precondition: Skyward Spider is not in the corpus")
	}
	st := c.Faces[0].Statics[0]
	if !st.HasParam(cards.PKAddKeyword) {
		t.Fatal("precondition: Skyward Spider's static grants no keyword")
	}
	if fx, ok := staticEquipFixture(reg, st); !ok || fx.attach != "self" {
		t.Fatalf("precondition: the equip fixture does not host the card itself: %+v %v", fx, ok)
	}
	it := staticItemFor(t, reg, "Skyward Spider", "static#0.0", "static.continuous")
	found := false
	for _, s := range it.Steps {
		if s.Op == "attach" && s.AttachedTo == "p0:Skyward Spider" {
			found = true
		}
	}
	if !found {
		t.Fatal("the scenario never attaches the Equipment to the card")
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("the served scenario does not replay: %v", res.Fails)
	}
}
