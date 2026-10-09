package rules

// levelb_stack_timing_test.go pins the gorge side of the level-B census
// stack-content class (ticket cli-20261009T105445Z, D8b rows file): the two
// multi-row mechanism families this ticket triaged whose gorge side is
// correct and whose divergences are XMage-side timing, plus the one gorge
// side this ticket fixed (Skewer Slinger, rules/trigger_blocks_blocker_test.go).
//
// - Starfield Vocalist's static#0.0 probe: gorge doubles the probe creature's
//   ETB trigger exactly once, and the doubled pair is placed on the stack
//   only after the controller's trigger-order answer (CR 603.3b, Ruling U1)
//   -- the XMage side places them before asking, which is what its "pass"
//   checkpoint records.
// - Ferocification's trigger#0.0 probe: the "At the beginning of combat on
//   your turn" ability is on the stack when its controller first receives
//   priority in that step (CR 603.3/117.5).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStarfieldVocalistProbeDoublesETBTrigger runs the generated static#0.0
// probe shape: the doubling static (Panharmonicon family) is in play, the
// probe creature's ETB trigger fires once for the entering creature and the
// static doubles it, so the pass_to priority ask sees TWO instances of the
// same ability (the effect the scenario compares against XMage).
func TestStarfieldVocalistProbeDoublesETBTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := `{"name":"starfield-static-probe","cr":["603.2d"],"why":"level-B doubling probe",` +
		`"setup":{"p0":{"battlefield":["Starfield Vocalist","Soul Warden"],"hand":["Savannah Lions"]},"p1":{"battlefield":["Grizzly Bears"]}},` +
		`"steps":[` +
		`{"op":"cast","seat":0,"card":"p0:Savannah Lions","mana":"W"},` +
		`{"op":"pass","seat":0},` +
		`{"op":"pass","seat":1},` +
		`{"op":"pass_to","seat":0,"decision":"priority"}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: the probe creature's spell was really cast.
	if n := len(res.Snapshots); n != 5 {
		t.Fatalf("snapshots = %d, want 5", n)
	}
	var castSpell bool
	for _, s := range res.Snapshots[1].Stack {
		if s.Kind == "spell" && s.Source == "p0:Savannah Lions" {
			castSpell = true
		}
	}
	if !castSpell {
		t.Fatalf("cast checkpoint stack = %+v, want the Savannah Lions spell", res.Snapshots[1].Stack)
	}
	// The final priority ask carries both doubled instances; the placement
	// happened after the controller's order answer inside the pass_to.
	last := res.Snapshots[4]
	var abilities []string
	for _, s := range last.Stack {
		if s.Kind == "ability" {
			abilities = append(abilities, s.Source)
		}
	}
	if len(abilities) != 2 || abilities[0] != "p0:Soul Warden" || abilities[1] != "p0:Soul Warden" {
		t.Fatalf("pass_to stack abilities = %v, want two p0:Soul Warden instances", abilities)
	}
	var order bool
	for _, d := range res.Decisions {
		if d.Kind == "order" {
			order = true
		}
	}
	if !order {
		t.Fatal("no trigger-order decision was posed for the doubled pair")
	}
}

// TestFerocificationBeginCombatTriggerOnStack pins the beginning-of-combat
// placement: at the controller's first priority ask in the begin-combat step
// the trigger is already on the stack, unresolved.
func TestFerocificationBeginCombatTriggerOnStack(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := `{"name":"ferocification-begin-combat","cr":["511.1"],"why":"level-B begin-combat trigger probe",` +
		`"setup":{"p0":{"battlefield":["Ferocification"]},"p1":{"battlefield":["Grizzly Bears"]}},` +
		`"steps":[` +
		`{"op":"pass_to","seat":0,"step":"begin-combat"},` +
		`{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := res.Snapshots[1].Step; got != "begin-combat" {
		t.Fatalf("snapshot step = %q, want begin-combat", got)
	}
	var abilities []string
	for _, s := range res.Snapshots[1].Stack {
		if s.Kind == "ability" {
			abilities = append(abilities, s.Source)
		}
	}
	if len(abilities) != 1 || abilities[0] != "p0:Ferocification" {
		t.Fatalf("begin-combat stack abilities = %v, want [p0:Ferocification]", abilities)
	}
	// The trigger is unresolved: the mode ask is posed only when it resolves,
	// during the scenario's resolve step.
	var mode bool
	for _, d := range res.Decisions {
		if d.Kind == "mode" {
			mode = true
		}
	}
	if !mode {
		t.Fatal("the begin-combat Charm's mode ask was never posed")
	}
}
