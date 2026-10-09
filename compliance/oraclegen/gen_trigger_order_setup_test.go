package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// setupWith lists setup battlefield permanents per seat, the shape a scenario
// records for the seeded permanents XMage places in build().
func setupWith(seat string, names ...string) map[string]Seat {
	return map[string]Seat{seat: {Battlefield: names}}
}

// TestTriggerOrderSetupPosedQueuesNameAsSetupChoice pins the retiming: a
// trigger-order decision the runner recorded before step zero whose picks all
// name setup-placed permanents is posed by XMage during setup placement, so
// its name answers must be queued as setup_choice in xmage_answers[0] (the
// driver reads that slot before build()) or XMage's chooseTriggeredAbility
// falls to the pending-list fallback. The last pick is dropped: XMage pushes
// the last remaining ability without an ask.
func TestTriggerOrderSetupPosedQueuesNameAsSetupChoice(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	// Precondition: the sources differ (a same-source order cannot be named)
	// and both are setup permanents, so the setup membership is what the
	// predicate must decide on.
	if !triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", d.PickRefs)
	}
	if !triggerOrderSetupPosed(d, setup) {
		t.Fatalf("precondition: %v must be setup-posed", d.PickRefs)
	}
	got := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, setup)
	want := [][]XAnswer{{{0, "setup_choice", "Adrenaline Jockey"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (setup-posed order queues the name before build)", got, want)
	}
}

// TestTriggerOrderSetupPosedReachesXAnswersForScenario pins the wiring: the
// scenario-aware entry point the templates call must pass the scenario's setup
// through, or the retiming never fires in generated scenarios.
func TestTriggerOrderSetupPosedReachesXAnswersForScenario(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	sc := Scenario{
		Name:  "gen1-trigger#0.0",
		Setup: setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler"),
	}
	if len(sc.Setup["p0"].Battlefield) != 2 {
		t.Fatalf("precondition: setup must place both sources, got %v", sc.Setup)
	}
	got := xanswersForScenario(rules.OracleResult{Decisions: []rules.OracleDecision{d}}, sc, nil, nil)
	want := [][]XAnswer{{{0, "setup_choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersForScenario = %#v, want %#v (setup threaded to the retiming)", got, want)
	}
}

// TestTriggerOrderSetupPosedRequiresSetupSources pins the predicate's second
// half: a before-step-zero order whose picks are NOT setup permanents keeps
// the old behaviour (no answers). A wrong predicate here would queue a name
// for an ask XMage poses at its own step and derail the step's dialogs.
func TestTriggerOrderSetupPosedRequiresSetupSources(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	setup := setupWith("p0", "Grizzly Bears")
	// Precondition: the sources differ, so ONLY the setup membership can make
	// the predicate false.
	if !triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", d.PickRefs)
	}
	if triggerOrderSetupPosed(d, setup) {
		t.Fatalf("precondition: %v must not be setup-posed", d.PickRefs)
	}
	if got := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, setup); got != nil {
		t.Fatalf("xanswersSetup = %#v, want nil (non-setup sources keep the old drop)", got)
	}
}

// TestTriggerOrderSetupSameSourceStaysUnqueued pins the same-source exclusion
// for setup-posed orders: a name matches both abilities' source, and the inert
// rule text is not XMage's rule wording, so queuing either as setup_choice
// only derails the as-enters dialogs sharing the queue (measured: Ashling,
// Rekindled and Gathering Stone setup answers HARNESS or diverge).
func TestTriggerOrderSetupSameSourceStaysUnqueued(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Thundertrap Trainer", "p0:Thundertrap Trainer")
	setup := setupWith("p0", "Thundertrap Trainer")
	// Precondition: the setup DOES place the source, so only the same-source
	// guard can keep the answers out.
	if len(setup["p0"].Battlefield) != 1 || setup["p0"].Battlefield[0] != "Thundertrap Trainer" {
		t.Fatalf("precondition: setup must place the shared source, got %v", setup)
	}
	if triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: picks must share one source name, refs %v", d.PickRefs)
	}
	if got := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, setup); got != nil {
		t.Fatalf("xanswersSetup = %#v, want nil (a same-source setup order stays unqueued)", got)
	}
}

// TestTriggerOrderSetupQueueFollowsAsEntersChoice pins the shared-queue order:
// when a setup permanent also poses an as-enters colour/type choice, that
// answer must precede the order names -- XMage consumes the placement dialog
// during build() and the ordering ask only at the first priority. The order is
// recorded FIRST here, so a decision-order append would invert the queue.
func TestTriggerOrderSetupQueueFollowsAsEntersChoice(t *testing.T) {
	order := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	color := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Options: 1, Min: 1, Max: 1, Picks: []string{"White"}, PickIdx: []int{0},
		PickKinds: []string{"color"}}
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	if !IsSetupChoice(color) {
		t.Fatalf("precondition: %+v must be an as-enters setup choice", color)
	}
	got := xanswersSetup([]rules.OracleDecision{order, color}, 1, nil, nil, setup)
	want := [][]XAnswer{{{0, "setup_choice", "White"}, {0, "setup_choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (as-enters choice leads the pre-build queue)", got, want)
	}
}

// TestTriggerOrderGameplaySharedStepStaysDemoted pins that the setup retiming
// does not leak into a gameplay step: a shared-step order at step 1 -- even
// when its sources are setup permanents -- keeps the inert text form, because
// XMage poses that ask at its own step and a leftover name would be consumed
// by the step's other dialogs (measured: Baron Strucker/static#0.0 HARNESSes
// under the name form; the demoted text form agrees).
func TestTriggerOrderGameplaySharedStepStaysDemoted(t *testing.T) {
	order := triggerOrderDecision(1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	target := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#9"},
		PickIdx: []int{0}, PickKinds: []string{"search"}}
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	// Precondition: the order's sources ARE setup permanents, so only the
	// gameplay step (not the setup membership) keeps the demotion.
	if order.Step < 0 {
		t.Fatalf("precondition: the order must be recorded at a gameplay step")
	}
	if !triggerOrderNamesDistinct(order) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", order.PickRefs)
	}
	got := xanswersSetup([]rules.OracleDecision{target, order}, 2, nil, nil, setup)
	want := [][]XAnswer{
		nil,
		{{0, "target", "Wastes"}, {0, "choice", "When this enters, do a thing."}, {0, "choice", "When this enters, do a thing."}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (a gameplay shared step keeps the inert text form)", got, want)
	}
}
