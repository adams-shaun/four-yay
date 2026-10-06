package templates

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Check the cast checkpoint itself, not just the final empty stack: a reversed
// cast also ends with an empty stack, and a surplus pool can hide a bad price.
func TestCostStaticCastCheckpoints(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range costStaticCards {
		t.Run(name, func(t *testing.T) {
			it := staticCostItem(t, reg, name)
			probe := probeCast(t, it)
			spell := strings.TrimPrefix(probe.Card, "p0:")
			index := -1
			for i, st := range it.Steps {
				if st.Op == "cast" {
					index = i
				}
			}
			if index < 0 || probe.Mana == "" {
				t.Fatal("precondition: no priced probe cast")
			}
			res := runSteps(t, reg, it.Scenario, it.Steps)
			if len(res.Fails) != 0 || len(res.Snapshots) != len(it.Steps)+1 {
				t.Fatalf("scenario failed: %v; snapshots=%d", res.Fails, len(res.Snapshots))
			}
			before, after := res.Snapshots[index], res.Snapshots[index+1]
			if len(before.Players) != 2 || len(after.Players) != 2 ||
				before.Players[0].Seat != 0 || after.Players[0].Seat != 0 {
				t.Fatal("precondition: missing p0 checkpoint")
			}
			if !slices.Contains(before.Players[0].Hand, spell) || before.Players[0].Pool != "" {
				t.Fatalf("precondition: probe must be in hand with no prior mana: %+v", before.Players[0])
			}
			if spell != name {
				found := false
				for _, p := range before.Permanents {
					found = found || (p.Name == name && p.Controller == 0)
				}
				if !found {
					t.Fatal("precondition: other-spell reducer absent from p0 battlefield")
				}
			}
			if slices.Contains(after.Players[0].Hand, spell) {
				t.Fatal("probe stayed in hand after cast")
			}
			found := false
			for _, st := range after.Stack {
				found = found || (st.Source == probe.Card && st.Kind == "spell" && st.Controller == 0)
			}
			if !found {
				t.Fatalf("probe not on stack after cast: %+v", after.Stack)
			}
			if pool := after.Players[0].Pool; pool != "" {
				t.Errorf("p0.pool after exactly-priced %s cast = %q, want empty", spell, pool)
			}
		})
	}
}
