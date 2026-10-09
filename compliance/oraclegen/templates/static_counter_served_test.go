package templates

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticCounterGatesServed: counter-gated static rows the setup-counters
// fixture (ticket cli-20261009T031409Z-a76de35a) moved off the named
// "needs counters on the affected permanent" skip. Each subtest pins the
// effect the scenario's comparison rests on: the fixture counters are really
// on the board in the setup the two engines share, and the static's grant
// reaches the countered permanent gorge-side (and never crosses the
// controller line, or the comparison itself would diverge).
func TestStaticCounterGatesServed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	t.Run("Vigorbloom Vanguard grants vigilance on the countered probe", func(t *testing.T) {
		const name = "Vigorbloom Vanguard"
		it, res := generateServed(t, reg, name, "static#0.0")
		if it.Scenario.Setup["p0"].Counters["Grizzly Bears"]["P1P1"] != 1 {
			t.Fatalf("precondition: p0 setup counters %v, want one P1P1 on the Bears",
				it.Scenario.Setup["p0"].Counters)
		}
		if it.Scenario.Setup["p1"].Counters["Grizzly Bears"]["P1P1"] != 1 {
			t.Fatalf("precondition: p1 setup counters %v, want one P1P1 on the Bears",
				it.Scenario.Setup["p1"].Counters)
		}
		final := res.Snapshots[len(res.Snapshots)-1]
		bear, ok := permanent(final, 0, "Grizzly Bears")
		if !ok {
			t.Fatal("precondition: p0's countered Bears are not on the battlefield")
		}
		if !slices.Contains(bear.Keywords, "Vigilance") {
			t.Fatalf("the static's vigilance grant did not land on the countered Bears: %v", bear.Keywords)
		}
		opp, ok := permanent(final, 1, "Grizzly Bears")
		if !ok {
			t.Fatal("precondition: p1's countered Bears are not on the battlefield")
		}
		if slices.Contains(opp.Keywords, "Vigilance") {
			t.Fatalf("the YouCtrl static's grant crossed the controller line: %v", opp.Keywords)
		}
	})

	t.Run("Debris Field Crusher is placed holding its station gate", func(t *testing.T) {
		const name = "Debris Field Crusher"
		it, res := generateServed(t, reg, name, "static#0.0")
		p0 := it.Scenario.Setup["p0"]
		if !slices.Contains(p0.Battlefield, name) {
			t.Fatalf("precondition: the ship is not placed on the battlefield: %v", p0.Battlefield)
		}
		if p0.Counters[name]["CHARGE"] < 8 {
			t.Fatalf("precondition: the ship holds %v counters, want the gate's 8 charge", p0.Counters[name])
		}
		final := res.Snapshots[len(res.Snapshots)-1]
		ship, ok := permanent(final, 0, name)
		if !ok {
			t.Fatal("precondition: the ship is not on the battlefield at the final checkpoint")
		}
		if !slices.Contains(ship.Types, "Creature") || ship.PT == "" {
			t.Fatalf("the station static's AddType$ Creature did not land: types %v pt %q", ship.Types, ship.PT)
		}
		if !slices.Contains(ship.Keywords, "Flying") {
			t.Fatalf("the station static's Flying grant did not land: keywords %v", ship.Keywords)
		}
	})

	t.Run("Innkeeper's Talent grants ward on the countered permanents", func(t *testing.T) {
		const name = "Innkeeper's Talent"
		it, res := generateServed(t, reg, name, "static#0.0")
		if it.Scenario.Setup["p0"].Counters["Grizzly Bears"]["P1P1"] != 1 {
			t.Fatalf("precondition: p0 setup counters %v, want one P1P1 on the Bears",
				it.Scenario.Setup["p0"].Counters)
		}
		if it.Scenario.Setup["p1"].Counters["Grizzly Bears"]["P1P1"] != 1 {
			t.Fatalf("precondition: p1 setup counters %v, want one P1P1 on the Bears",
				it.Scenario.Setup["p1"].Counters)
		}
		final := res.Snapshots[len(res.Snapshots)-1]
		bear, ok := permanent(final, 0, "Grizzly Bears")
		if !ok {
			t.Fatal("precondition: p0's countered Bears are not on the battlefield")
		}
		if !slices.ContainsFunc(bear.Keywords, func(k string) bool { return strings.HasPrefix(k, "Ward") }) {
			t.Fatalf("the class-2 ward grant did not land on the countered Bears: %v", bear.Keywords)
		}
		opp, ok := permanent(final, 1, "Grizzly Bears")
		if !ok {
			t.Fatal("precondition: p1's countered Bears are not on the battlefield")
		}
		if len(opp.Keywords) != 0 {
			t.Fatalf("the YouCtrl static's grant crossed the controller line: %v", opp.Keywords)
		}
	})
}
