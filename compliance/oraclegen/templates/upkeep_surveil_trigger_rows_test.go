package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The six stored `diverge` verdict rows whose detail read `step 1 (resolve)
// p0.graveyard: gorge "[]", xmage "[Wastes]"` (ticket
// agent-20261009T115712Z-b072e2c1, stage 2 of 50ed60e1f). Each is a Phase
// upkeep trigger whose chain is `DB$ Surveil`; the trigger#0.0 scenario walks
// turn 1's upkeep in setup and stops at the same upkeep with a pass_to, so the
// trigger resolves a second time at step 1. The cached XMage reference
// graveyarded the looked-at card there while gorge kept it on top.
//
// gorge now answers the step-time ask with the empty kept set, and the
// generator scripts XMage's selection by name: a bare [choice_skip] would be
// read as keep-all, the opposite outcome.
func TestUpkeepSurveilTriggerRowsGraveyardAtResolve(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{
		"Broodheart Engine", "Mindwhisker", "Morcant's Eyes",
		"Essence Anchor", "Grave Researcher", "Eye of Jace",
	} {
		t.Run(name, func(t *testing.T) {
			it, res := generateServed(t, reg, name, "trigger#0.0")

			// Precondition: the scenario really revisits the upkeep, and the
			// surveil ask is posed at that step (not only during setup).
			if len(it.Steps) != 2 || it.Steps[0].Op != "pass_to" || it.Steps[0].Step != "upkeep" || it.Steps[1].Op != "resolve" {
				t.Fatalf("precondition: %s steps = %+v, want [pass_to upkeep, resolve]", name, it.Steps)
			}
			var posed *int
			for i := range res.Decisions {
				d := res.Decisions[i]
				if d.GorgeKind == "arrange" && d.Step == 1 {
					posed = &i
				}
			}
			if posed == nil {
				t.Fatalf("precondition: %s posed no arrange ask at step 1: %+v", name, res.Decisions)
			}
			d := res.Decisions[*posed]
			if d.Via != "resolve fallback" || len(d.PickIdx) != 0 || d.ArrangeKind != "graveyard" {
				t.Fatalf("%s step-1 arrange = via %q picks %v kind %q, want the empty graveyard fallback", name, d.Via, d.PickIdx, d.ArrangeKind)
			}
			if !reflect.DeepEqual(d.ArrangeLabels, []string{"Wastes"}) {
				t.Fatalf("precondition: %s looked at %v, want [Wastes]", name, d.ArrangeLabels)
			}

			// The XMage script: one name selection, nothing else, on step 1;
			// the setup bucket stays empty (the setup keep-top is XMage's own
			// default and the stored setup checkpoint agrees).
			want := []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "Wastes"}}
			if len(it.XAnswers) != 2 {
				t.Fatalf("%s xanswers buckets = %d, want 2: %+v", name, len(it.XAnswers), it.XAnswers)
			}
			if len(it.XAnswers[0]) != 0 {
				t.Errorf("%s setup bucket = %+v, want empty", name, it.XAnswers[0])
			}
			if !reflect.DeepEqual(it.XAnswers[1], want) {
				t.Errorf("%s step-1 xanswers = %+v, want %+v", name, it.XAnswers[1], want)
			}

			// The gorge snapshots (setup, step 0 pass_to, step 1 resolve): the
			// card stayed on top through setup and went to the graveyard at
			// the resolve checkpoint.
			if len(res.Snapshots) != 3 {
				t.Fatalf("%s snapshots = %d, want setup + 2 steps", name, len(res.Snapshots))
			}
			setup, final := res.Snapshots[0].Players[0], res.Snapshots[2].Players[0]
			if got, base := countName(final.Graveyard, "Wastes"), countName(setup.Graveyard, "Wastes"); got != base+1 {
				t.Errorf("%s p0 Wastes in graveyard = %d (setup %d), want one more at step 1", name, got, base)
			}
			if final.LibraryCount != setup.LibraryCount-1 {
				t.Errorf("%s library_count = %d (setup %d), want one fewer", name, final.LibraryCount, setup.LibraryCount)
			}
		})
	}
}
