package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestSelfLTBRecipes generates the self leaves-the-battlefield items and
// checks, with the generator's own settle-and-look check, that the trigger is
// on the stack after the cause. The probe each card needs is asserted, so a
// recipe that stopped choosing it (or a card that fires some other way) fails.
func TestSelfLTBRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, castProbe string }{
		{"City Pigeon", "trigger#0.0", "p0:Unsummon"},
		{"Featherbrained Filcher", "trigger#0.0", "p0:Unsummon"},
		{"Cryogen Relic", "trigger#0.1", "p0:Shatter"},
		{"Greed's Gambit", "trigger#0.2", "p0:Disenchant"},
		{"Syr Vondam, Sunstar Exemplar", "trigger#0.1", "p0:Murder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, ltbSelfSub)
			cast := false
			for _, st := range it.Scenario.Steps {
				cast = cast || (st.Op == "cast" && st.Card == tc.castProbe)
			}
			if !cast {
				t.Fatalf("precondition: scenario never casts %s: %+v", tc.castProbe, it.Scenario.Steps)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			slot := tc.key[len("trigger#0."):]
			if !stackedAfterCause(t, reg, it.Scenario, tc.name, slot) {
				t.Fatalf("%s trigger slot %s never reaches the stack", tc.name, slot)
			}
		})
	}
}

// TestSelfLTBPowerGate: Syr Vondam is a 2/2 and the trigger's filter is
// powerGE4, so the fixture carries two +1/+1 counters on the card itself; with
// them removed the trigger does not reach the stack.
func TestSelfLTBPowerGate(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Syr Vondam, Sunstar Exemplar"
	it := triggerRequirement(t, reg, name, "trigger#0.1", ltbSelfSub)
	p0 := it.Scenario.Setup["p0"]
	if got := p0.Counters[name]["P1P1"]; got != 2 {
		t.Fatalf("P1P1 counters on %s = %d (setup %+v), want 2", name, got, p0)
	}
	if !stackedAfterCause(t, reg, it.Scenario, name, "1") {
		t.Fatalf("precondition: gated trigger does not reach the stack with its counters")
	}
	bare := it.Scenario
	setup := map[string]oraclegen.Seat{}
	for k, v := range it.Scenario.Setup {
		setup[k] = v
	}
	noCounters := p0
	noCounters.Counters = nil
	setup["p0"] = noCounters
	bare.Setup = setup
	if stackedAfterCause(t, reg, bare, name, "1") {
		t.Fatalf("the powerGE4 trigger fired without the counters: the test does not exercise the gate")
	}
}
