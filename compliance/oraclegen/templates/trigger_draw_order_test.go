package templates

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// drawOrderCards are the real "whenever you draw a card" cards whose
// Divination cause leaves two pending triggers behind a trigger-order
// decision. Each puts a +1/+1 counter on itself per card drawn.
var drawOrderCards = []string{"Ravenhill Flock", "The Astonishing Ant-Man", "Clinquant Skymage"}

// drawCauseSteps is the item's cause without its settle resolves.
func drawCauseSteps(sc oraclegen.Scenario) []oraclegen.Step {
	steps := sc.Steps
	for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
		steps = steps[:len(steps)-1]
	}
	return steps
}

// drawCheckpoint replays the cause, both passes and pass_to the next priority
// decision, and returns the result; its last snapshot is the checkpoint
// immediately after that priority step.
func drawCheckpoint(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario) rules.OracleResult {
	t.Helper()
	steps := append(append([]oraclegen.Step(nil), drawCauseSteps(sc)...),
		oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass_to", Decision: "priority"})
	res := runSteps(t, reg, sc, steps)
	if len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("checkpoint replay: fails=%v snapshots=%d", res.Fails, len(res.Snapshots))
	}
	return res
}

func abilitiesFrom(snap rules.OracleSnapshot, name string) int {
	n := 0
	for _, e := range snap.Stack {
		if e.Kind == "ability" && strings.Contains(strings.ToLower(e.Source), strings.ToLower(name)) {
			n++
		}
	}
	return n
}

func counterOn(snap rules.OracleSnapshot, name, kind string) (int32, bool) {
	for _, p := range snap.Permanents {
		if p.Name == name && p.Controller == 0 {
			return p.Counters[kind], true
		}
	}
	return 0, false
}

// TestTriggerDrawOrderCoverage: each card's trigger#0.0 is classified
// trigger.drawn, sits on p0's battlefield with a Divination cause and library
// to draw from, and generates a playable item.
func TestTriggerDrawOrderCoverage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range drawOrderCards {
		t.Run(name, func(t *testing.T) {
			it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.drawn")
			if !inZone(it.Scenario.Setup["p0"].Battlefield, name) {
				t.Fatalf("precondition: %s not on p0's battlefield: %v", name, it.Scenario.Setup["p0"].Battlefield)
			}
			if !inZone(it.Scenario.Setup["p0"].Hand, "Divination") {
				t.Fatalf("precondition: no Divination cause in p0's hand: %v", it.Scenario.Setup["p0"].Hand)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			first := res.Snapshots[0]
			if lib := first.Players[0].LibraryCount; lib < 2 {
				t.Fatalf("precondition: library has %d cards, Divination draws two", lib)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			if len(last.Stack) != 0 {
				t.Fatalf("final stack not empty: %+v", last.Stack)
			}
		})
	}
}

// TestTriggerDrawOrderProbe: at the checkpoint right after the priority step
// the controller's trigger-order ask has been answered and both abilities are
// on the stack with nothing resolved; resolving them puts two counters on the
// source. Clinquant Skymage is measured the same way.
func TestTriggerDrawOrderProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range drawOrderCards {
		t.Run(name, func(t *testing.T) {
			it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.drawn")
			res := drawCheckpoint(t, reg, it.Scenario)
			at := res.Snapshots[len(res.Snapshots)-1]
			if got := abilitiesFrom(at, name); got != 2 {
				t.Fatalf("abilities from %s at the priority checkpoint = %d, want 2 (stack %+v)", name, got, at.Stack)
			}
			order := 0
			for _, d := range res.Decisions {
				if d.GorgeKind == "trigger_order" || d.Kind == "order" {
					order++
					if d.Options != 2 || len(d.Picks) != 2 {
						t.Fatalf("order decision options=%d picks=%v, want 2 and 2", d.Options, d.Picks)
					}
				}
			}
			if order != 1 {
				t.Fatalf("answered order decisions = %d, want 1: %+v", order, res.Decisions)
			}
			if n, ok := counterOn(at, name, "P1P1"); !ok || n != 0 {
				t.Fatalf("at the checkpoint %s on bf=%v with %d counters, want on bf with 0", name, ok, n)
			}
			steps := append(append([]oraclegen.Step(nil), drawCauseSteps(it.Scenario)...),
				oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
				oraclegen.Step{Op: "pass_to", Decision: "priority"},
				oraclegen.Step{Op: "resolve"}, oraclegen.Step{Op: "resolve"})
			done := runSteps(t, reg, it.Scenario, steps)
			if len(done.Fails) != 0 {
				t.Fatalf("resolve replay fails: %v", done.Fails)
			}
			end := done.Snapshots[len(done.Snapshots)-1]
			if n, _ := counterOn(end, name, "P1P1"); n != 2 {
				t.Fatalf("%s has %d +1/+1 counters after resolving, want 2", name, n)
			}
			if len(end.Stack) != 0 {
				t.Fatalf("stack after resolving = %+v", end.Stack)
			}
		})
	}
}

// eruditeWizardItemSHA is sha256 of Erudite Wizard trigger#0.0's whole item
// JSON, captured from main before the draw checkpoint existed.
const eruditeWizardItemSHA = "e694f68658176450b3233fa4b24268ffa3a1f744d49fb458a24bac052a139054"

// TestTriggerDrawOrderIdentity: the checkpoint is private to the probe. The
// already-served single-trigger recipe keeps its item byte for byte, its
// priority checkpoint does not drain its one ability, and the newly served
// items emit the same cause and resolve as every other drawn item.
func TestTriggerDrawOrderIdentity(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := triggerRequirement(t, reg, "Erudite Wizard", "trigger#0.0", "trigger.drawn")
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(b)); got != eruditeWizardItemSHA {
		t.Fatalf("Erudite Wizard item changed: sha %s, want %s\n%s", got, eruditeWizardItemSHA, b)
	}
	res := drawCheckpoint(t, reg, it.Scenario)
	if got := abilitiesFrom(res.Snapshots[len(res.Snapshots)-1], "Erudite Wizard"); got != 1 {
		t.Fatalf("Erudite Wizard abilities at the priority checkpoint = %d, want 1", got)
	}
	for _, name := range drawOrderCards {
		it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.drawn")
		steps := it.Scenario.Steps
		if len(steps) != 2 || steps[0].Op != "cast" || steps[0].Card != "p0:Divination" || steps[1].Op != "resolve" {
			t.Fatalf("%s emitted steps = %+v, want [cast p0:Divination, resolve]", name, steps)
		}
	}
}
