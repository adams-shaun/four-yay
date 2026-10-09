package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The mana stage's ability_index selection must win over its label fallback.
// Itlimoc, Cradle of the Sun offers two mana abilities whose wheel labels are
// IDENTICAL ("Add G" -- the "{T}: Add {G}." ability and the "{T}: Add {G} for
// each creature you control" one, whose Amount$ X pips render as one G). With
// one selection pass, option 0's label matched the requested label before the
// loop reached option 1's Ability match, so a step naming ability_index 1
// activated ability 0: with no creatures on the battlefield the X ability
// produces nothing, but the scenario measured a floating G and diverged from
// XMage (level-B D6 row, Growing Rites of Itlimoc/activate#1.1).
//
// Scenario copied from the generated level-B item (LCI), scenario alone.
const manaStageItlimocScenario = `{"name":"mana-stage-index-1","cr":["602.2"],"why":"ability_index must select its own wheel option","setup":{"p0":{"battlefield":["Growing Rites of Itlimoc"],"back_face":["Growing Rites of Itlimoc"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Growing Rites of Itlimoc","ability_index":1}]}`

const manaStageItlimocFirstScenario = `{"name":"mana-stage-index-0","cr":["602.2"],"why":"ability_index 0 still activates the first ability","setup":{"p0":{"battlefield":["Growing Rites of Itlimoc"],"back_face":["Growing Rites of Itlimoc"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Growing Rites of Itlimoc","ability_index":0}]}`

func TestOracleManaStageAbilityIndexWinsOverLabel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(manaStageItlimocScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("activate failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: the transformed land is on the battlefield with no
	// creatures anywhere, so the X ability's count is zero.
	setup := res.Snapshots[0]
	if _, ok := snapPerm(setup, "p0:Growing Rites of Itlimoc"); !ok {
		t.Fatal("precondition: p0's Growing Rites of Itlimoc missing from the battlefield")
	}
	for _, p := range setup.Players {
		for _, name := range append(p.Hand, append(p.Graveyard, p.Exile...)...) {
			_ = name
		}
		if len(p.Hand) != 0 || len(p.Graveyard) != 0 || len(p.Exile) != 0 {
			t.Fatalf("precondition: p%d holds unexpected cards (hand %v graveyard %v exile %v)", p.Seat, p.Hand, p.Graveyard, p.Exile)
		}
	}
	// Precondition: the wheel offered both abilities, and the runner picked
	// the one ability_index 1 named.
	found := false
	for _, d := range res.Decisions {
		if d.GorgeKind != "choose" || d.Step != 0 {
			continue
		}
		found = true
		if d.Options != 2 {
			t.Fatalf("precondition: mana wheel offered %d options, want 2 (both Itlimoc abilities)", d.Options)
		}
		if len(d.PickIdx) != 1 || d.PickIdx[0] != 1 {
			t.Fatalf("mana stage picked %v, want [1]: ability_index 1 did not select its own option", d.PickIdx)
		}
	}
	if !found {
		t.Fatalf("precondition: no mana wheel decision recorded\n%s", strings.Join(res.Transcript, "\n"))
	}
	if len(res.Snapshots) < 2 {
		t.Fatalf("no activate checkpoint: %d snapshots", len(res.Snapshots))
	}
	act := res.Snapshots[1]
	if act.Players[0].Pool != "" {
		t.Fatalf("step-0 pool = %q, want \"\": the X ability (zero creatures) must produce nothing, not the sibling {T}: Add {G} ability's G", act.Players[0].Pool)
	}
}

// The sibling ability_index 0 scenario keeps activating "{T}: Add {G}." --
// the two-pass selection must not have traded one wrong pick for another,
// and the two pools differing is the comparison this file's fix assertion
// rides on.
func TestOracleManaStageAbilityIndexZeroStillFirst(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(manaStageItlimocFirstScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("activate failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if len(res.Snapshots) < 2 {
		t.Fatalf("no activate checkpoint: %d snapshots", len(res.Snapshots))
	}
	for _, d := range res.Decisions {
		if d.GorgeKind == "choose_n" && d.Step == 0 {
			if len(d.PickIdx) != 1 || d.PickIdx[0] != 0 {
				t.Fatalf("ability_index 0 picked %v, want [0]", d.PickIdx)
			}
		}
	}
	if p := res.Snapshots[1].Players[0].Pool; p != "G" {
		t.Fatalf("step-0 pool = %q, want \"G\"", p)
	}
}
