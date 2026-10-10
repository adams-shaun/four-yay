package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Source-type-qualified ability targets (ticket agent-20261009T162200Z-851f90cb):
// Echo, Perceptive Prodigy's `ValidTgts$ Card.Creature` and Scientist Supreme
// of A.I.M.'s bare `ValidTgts$ Artifact` judge a pending ability's SOURCE card
// type (CR 113.7a), end to end through the oracle runner.

// TestOracleAbilityOnStackSourceTypeCopies: the copier targets a pending
// ability of the matching source type through the pN:ability ref, the ask
// OFFERED it, and the copy resolves alongside the original (p1 pays twice).
func TestOracleAbilityOnStackSourceTypeCopies(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		name, scenario, copier, probe string
		wantStack                     int
	}{
		{
			name:   "creature source (Echo copies a Prodigal Pyromancer ping)",
			copier: "p0:Echo, Perceptive Prodigy", probe: "p0:Prodigal Pyromancer",
			scenario: `{"name":"echo-copies-creature-ability","cr":["113.7a","707.10"],
"why":"a Card.Creature ability target is judged against the pending ability's creature source",
"setup":{"p0":{"battlefield":["Prodigal Pyromancer","Echo, Perceptive Prodigy"]}},
"steps":[
 {"op":"activate","seat":0,"card":"p0:Prodigal Pyromancer","targets":["p1"]},
 {"op":"activate","seat":0,"card":"p0:Echo, Perceptive Prodigy","mana":"C","targets":["p0:ability:Prodigal Pyromancer"]},
 {"op":"resolve","seat":0},{"op":"resolve","seat":0},{"op":"resolve","seat":0}]}`,
		},
		{
			name:   "artifact source (Scientist Supreme copies a Razortip Whip ping)",
			copier: "p0:Scientist Supreme of A.I.M.", probe: "p0:Razortip Whip",
			scenario: `{"name":"scientist-copies-artifact-ability","cr":["113.7a","707.10"],
"why":"an Artifact ability target is judged against the pending ability's artifact source",
"setup":{"p0":{"battlefield":["Razortip Whip","Scientist Supreme of A.I.M."]}},
"steps":[
 {"op":"activate","seat":0,"card":"p0:Razortip Whip","mana":"C","targets":["p1"]},
 {"op":"activate","seat":0,"card":"p0:Scientist Supreme of A.I.M.","targets":["p0:ability:Razortip Whip"]},
 {"op":"resolve","seat":0},{"op":"resolve","seat":0},{"op":"resolve","seat":0}]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := RunOracleScenarioJSON(reg, []byte(c.scenario))
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Fails) != 0 {
				t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
			}
			// Precondition: both permanents sit untapped on p0's battlefield.
			for _, ref := range []string{c.copier, c.probe} {
				if p, ok := snapPerm(res.Snapshots[0], ref); !ok || p.Tapped {
					t.Fatalf("precondition: %s = %+v, want an untapped permanent on the battlefield", ref, p)
				}
			}
			// The probe's ability is PENDING when the copier is activated.
			pending := res.Snapshots[1].Stack
			if len(pending) != 1 || pending[0].Kind != "ability" || pending[0].Source != c.probe {
				t.Fatalf("precondition: stack after the prelude = %+v, want exactly the pending %s ability", pending, c.probe)
			}
			ref := "p0:ability:" + strings.TrimPrefix(c.probe, "p0:")
			offered := false
			for _, d := range res.Decisions {
				if d.Step != 1 || d.Kind != "target" {
					continue
				}
				for _, r := range d.OptionRefs {
					if r == ref {
						offered = true
					}
				}
				if len(d.PickRefs) != 1 || d.PickRefs[0] != ref {
					t.Fatalf("copier target ask picked %v, want exactly %s", d.PickRefs, ref)
				}
			}
			if !offered {
				t.Fatalf("the copier's ask never offered %s; decisions: %+v", ref, res.Decisions)
			}
			// Original + copy both resolved: p1 took the 1-damage ping twice.
			final := res.Snapshots[len(res.Snapshots)-1]
			if life := final.Players[1].Life; life != 18 {
				t.Fatalf("p1 life = %d, want 18 (the ping resolved twice)", life)
			}
			if len(final.Stack) != 0 {
				t.Fatalf("final stack = %+v, want empty", final.Stack)
			}
		})
	}
}
