package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// (cli-20261006T132128Z) Level B: the setup checkpoint is turn 1's FIRST
// precombat-main priority, the exact point XMage's ScenarioReplay pauses at
// for its "setup" snapshot (ScenarioReplay.java runCode("setup", TURN, MAIN,
// ...)). That means:
//
//   - a permanent's "at the beginning of your first main phase" trigger is
//     still ON THE STACK at setup -- the driver must not resolve it under the
//     fallback first (the old empty-stack exit condition did);
//   - a Saga placed by setup has already taken the CR 505.4/703.4f
//     precombat-main turn-based lore counter, so it shows two lore counters
//     (the entry one plus the main-phase one), not one.
//
// Both cards below are real corpus cards whose Main1 behaviour is choice-free
// (Gardenize's Main1 trigger adds one {G} per CHARGE counter; Urza's Saga's
// chapter II animates itself), so the checkpoint is deterministic.
const gardenizeSetupMain1 = `{
  "name": "setup-main1-trigger",
  "setup": {
    "p0": {"battlefield": ["Gardenize"], "counters": {"Gardenize": {"CHARGE": 1}}},
    "p1": {"battlefield": ["Grizzly Bears"]}
  },
  "steps": []
}`

const sagaSetupMain1 = `{
  "name": "setup-main1-saga",
  "setup": {
    "p0": {"battlefield": ["Urza's Saga"]},
    "p1": {"battlefield": ["Grizzly Bears"]}
  },
  "steps": []
}`

func runSetupCheckpoint(t *testing.T, scenario string) *oracleRun {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	if len(run.snaps) == 0 {
		t.Fatal("no setup snapshot")
	}
	return run
}

// A Main1-trigger permanent placed by setup is still on the stack at the
// setup checkpoint, and its effect has NOT been applied by the fallback: the
// {G} from Gardenize's charge counter is absent from the pool. If the trigger
// had resolved during setup the pool would read "G".
func TestOracleSetupMain1TriggerStaysOnStack(t *testing.T) {
	run := runSetupCheckpoint(t, gardenizeSetupMain1)
	setup := run.snaps[0]

	// Precondition: the trigger's source is on the battlefield, so the
	// Main1 trigger really is in play. Without this a vacuous stack assertion
	// would pass on a board with no Gardenize.
	if _, ok := snapPerm(setup, "p0:Gardenize"); !ok {
		t.Fatalf("precondition: Gardenize is not on the battlefield at setup\n%+v", setup.Permanents)
	}
	var onStack bool
	for _, s := range setup.Stack {
		if s.Source == "p0:Gardenize" {
			onStack = true
		}
	}
	if !onStack {
		t.Fatalf("setup stack = %+v, want Gardenize's Main1 ability still on it (XMage's setup snapshot has it)", setup.Stack)
	}
	if got := setup.Players[0].Pool; got != "" {
		t.Fatalf("setup p0 pool = %q, want empty: the Main1 trigger must not have resolved during setup", got)
	}
}

// A Saga placed by setup has taken the precombat-main lore counter, so it
// shows LORE=2 at the setup checkpoint (Urza's Saga's chapter II, from the
// entry and main-phase counters, is what is queued).
func TestOracleSetupSagaTakesMain1LoreCounter(t *testing.T) {
	run := runSetupCheckpoint(t, sagaSetupMain1)
	setup := run.snaps[0]

	saga, ok := snapPerm(setup, "p0:Urza's Saga")
	if !ok {
		t.Fatalf("precondition: Urza's Saga is not on the battlefield at setup\n%+v", setup.Permanents)
	}
	if got := saga.Counters["LORE"]; got != 2 {
		t.Fatalf("setup Urza's Saga lore = %d (%v), want 2 (entry counter + CR 505.4/703.4f precombat-main counter)", got, saga.Counters)
	}
	// The precombat-main counter's chapter (II) is a triggered ability, so
	// it too is still on the stack at the checkpoint, as in XMage.
	var onStack bool
	for _, s := range setup.Stack {
		if s.Source == "p0:Urza's Saga" {
			onStack = true
		}
	}
	if !onStack {
		t.Fatalf("setup stack = %+v, want Urza's Saga's chapter ability still on it", setup.Stack)
	}
}
