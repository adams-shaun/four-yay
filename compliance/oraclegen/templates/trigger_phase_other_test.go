package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestPhaseOtherRecipes: one real card per phase-other shape. Each item plays
// through gorge, stops on a pass_to checkpoint PassToSteps lists, and a
// snapshot at that checkpoint shows the card's trigger on the stack.
func TestPhaseOtherRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, wantStep, wantActive string
	}{
		{"Cautious Survivor", "trigger#0.0", "main2", ""},
		{"Scheming Silvertongue", "trigger#0.0", "main2", ""},
		{"Child of the Volcano", "trigger#0.0", "end", ""},
		{"Cheering Crowd", "trigger#0.0", "main1", "p0"},
		{"Super Intelligence", "trigger#0.0", "upkeep", "p0"},
		{"Sidequest: Play Blitzball", "trigger#0.1", "end-combat", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.PhaseOtherSub)
			if it.Template != tc.key || it.Card != tc.name || len(it.CR) != 1 || it.CR[0] != "603.2" {
				t.Fatalf("identity = (%s, %s, %v)", it.Card, it.Template, it.CR)
			}
			if !phaseOtherSourceInSetup(it.Scenario.Setup["p0"], tc.name) {
				t.Fatalf("precondition: trigger source %s is in neither p0's battlefield nor p0's hand: %+v / %v", tc.name, it.Scenario.Setup["p0"].Battlefield, it.Scenario.Setup["p0"].Hand)
			}
			if !phaseOtherAuraBearerOnBattlefield(it.Scenario.Setup["p0"].Battlefield, tc.name, it.Scenario.Steps) {
				t.Fatalf("precondition: the enchanted-creature bearer is not on p0's battlefield: %v", it.Scenario.Setup["p0"].Battlefield)
			}
			if causes, resolves := phaseOtherCauseCounts(it.Scenario.Steps); causes == 0 || resolves == 0 {
				t.Fatalf("precondition: want a cause and a resolve, got %d and %d: %+v", causes, resolves, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			// The cause's checkpoint is the first pass_to with the wanted
			// (step, active) pair. Replay everything up to it, no trailing
			// resolves, and require the trigger on the stack there.
			idx, checkpoint := phaseOtherCheckpoint(it.Scenario.Steps, tc.wantStep, tc.wantActive)
			if idx < 0 {
				t.Fatalf("no %q active %q checkpoint in %+v", tc.wantStep, tc.wantActive, it.Scenario.Steps)
			}
			if !phaseOtherCheckpointInPassToSteps(checkpoint) {
				t.Fatalf("checkpoint %q is not one PassToSteps lists: %v", checkpoint, PassToSteps())
			}
			res := runSteps(t, reg, it.Scenario, it.Scenario.Steps[:idx+1])
			if !stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("checkpoint %q did not put %s's ability on the stack", checkpoint, tc.name)
			}
		})
	}
}

// phaseOtherCheckpoint finds the first pass_to step matching step/active and
// returns its index in steps and the PassToSteps spelling ("step" or
// "step@active").
func phaseOtherCheckpoint(steps []oraclegen.Step, step, active string) (int, string) {
	for i, st := range steps {
		if st.Op != "pass_to" || st.Step != step || st.Active != active {
			continue
		}
		if active != "" {
			return i, step + "@" + active
		}
		return i, step
	}
	return -1, ""
}

func phaseOtherCheckpointInPassToSteps(checkpoint string) bool {
	for _, want := range PassToSteps() {
		if want == checkpoint {
			return true
		}
	}
	return false
}

// phaseOtherSourceInSetup reports whether the source is on p0's battlefield,
// or held in p0's hand for the shape whose cause casts it (an unattached Aura
// cannot be placed on the battlefield).
func phaseOtherSourceInSetup(seat oraclegen.Seat, name string) bool {
	for _, bf := range seat.Battlefield {
		if bf == name {
			return true
		}
	}
	for _, h := range seat.Hand {
		if h == name {
			return true
		}
	}
	return false
}

// phaseOtherAuraBearerOnBattlefield reports true for the Aura shape (the
// source is in p0's hand, cast onto Grizzly Bears) when the bearer is on the
// battlefield; it is vacuously true for every shape that places the source on
// the battlefield itself.
func phaseOtherAuraBearerOnBattlefield(battlefield []string, name string, steps []oraclegen.Step) bool {
	for _, bf := range battlefield {
		if bf == name {
			return true
		}
	}
	for _, st := range steps {
		if st.Op == "cast" && st.Card == "p0:"+name {
			for _, bf := range battlefield {
				if bf == bearsProbe {
					return true
				}
			}
			return false
		}
	}
	return true
}

// phaseOtherCauseCounts counts the cause steps (cast/attack, or the pass_to
// checkpoint itself for a shape with no cast or attack) and resolves.
func phaseOtherCauseCounts(steps []oraclegen.Step) (int, int) {
	causes, resolves := 0, 0
	for _, st := range steps {
		switch st.Op {
		case "cast", "attack", "pass_to":
			causes++
		case "resolve":
			resolves++
		}
	}
	return causes, resolves
}
