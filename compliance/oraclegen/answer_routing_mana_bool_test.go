package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// These two shapes are the measured driver failures "Choice key [...] not
// found in [White, Blue, Black, Red, Green]": XMage's TestPlayer queues every
// answer on ONE FIFO choice queue, so an answer whose ask never fires is
// popped -- and thrown on -- by the next dialog. Both routings here are
// structural (decision kind + neighbour), never a card name.

// bertaYes is Berta, Wise Extrapolator/trigger#0.0's measured CR 603.5
// optional-trigger boolean (rules/trigger_queue.go askOptionalAtResolution):
// gorge asks "apply the effect?" because the Forge script marks the trigger
// optional, but XMage's OneOrMoreCountersAddedTriggeredAbility defaults
// optional=false, so XMage poses only the colour dialog its mana effect runs.
func bertaYes(step int) rules.OracleDecision {
	return rules.OracleDecision{Step: step, Seat: 0, Kind: "yesno", Options: 2,
		Picks:   []string{"Yes — Berta, Wise Extrapolator: Whenever one or more +1/+1 counters are put on NICKNAME, add one mana of any color."},
		PickIdx: []int{0}, PickRefs: []string{"p0:Berta, Wise Extrapolator"}, PickKinds: []string{"yes"},
		Resume: "optional", Via: "resolve fallback", GorgeKind: "trigger_optional", Min: 1, Max: 1}
}

// manaColourPick is a mana ability's colour ask (resume "mana_color"): the
// XMage dialog is ChoiceColor, whose keys are the colour names.
func manaColourPick(step int) rules.OracleDecision {
	return rules.OracleDecision{Step: step, Seat: 0, Kind: "choose_n", Options: 5,
		Picks: []string{"Add W"}, PickIdx: []int{0}, PickRefs: []string{"p0:Berta, Wise Extrapolator"},
		PickKinds: []string{"mana"}, Resume: "mana_color", Via: "answer", GorgeKind: "choose", Min: 1, Max: 1}
}

// ashlingOrder is Ashling, Rekindled/trigger#1.1's measured trigger-order
// decision: two triggers off ONE source (Ashling, Rimebound), so the picks'
// source names are not distinct and the span is the inert rule-text form.
func ashlingOrder(step int) rules.OracleDecision {
	return rules.OracleDecision{Step: step, Seat: 0, Kind: "order", GorgeKind: "trigger_order", Options: 2,
		Picks: []string{
			"Ashling, Rimebound: Whenever this creature transforms into CARDNAME and at the beginning of your first main phase, add two mana of any one color. Spend this mana only to cast spells with mana value 4 or greater.",
			"Ashling, Rimebound: At the beginning of your first main phase, you may pay {R}. If you do, transform NICKNAME.",
		},
		PickIdx: []int{0, 1}, PickRefs: []string{"p0:Ashling, Rekindled", "p0:Ashling, Rekindled"},
		PickKinds: []string{"trigger", "trigger"}, Via: "resolve fallback", Min: 2, Max: 2}
}

// ashlingDecline is the may-pay {R} transform trigger's decline, recorded
// between the span and the colour pick (forcedSingleOption drops it from the
// stream, as on main).
func ashlingDecline(step int) rules.OracleDecision {
	return rules.OracleDecision{Step: step, Seat: 0, Kind: "choose_n", Options: 1,
		Picks: []string{"Do not pay"}, PickIdx: []int{0}, PickRefs: []string{"p0:Ashling, Rekindled"},
		PickKinds: []string{"trigger_cost_decline"}, Via: "answer", GorgeKind: "choose", Min: 1, Max: 1}
}

// TestOptionalManaTriggerBooleanDropsAheadOfColourPick: the yesno whose
// ability resolves straight into the following mana colour ask is mandatory
// in XMage, so it scripts NOTHING and the colour pick leads the queue. A lone
// optional-trigger boolean (no colour ask follows) and a placement ask
// (resume "", askTriggerOptional) keep the generic yes/no script.
func TestOptionalManaTriggerBooleanDropsAheadOfColourPick(t *testing.T) {
	yes, col := bertaYes(1), manaColourPick(1)
	// Precondition: the fixture really is the measured pair -- an at-resolution
	// optional-trigger boolean (pick kind yes) immediately followed by a mana
	// colour pick of the same seat and step.
	if yes.GorgeKind != "trigger_optional" || yes.Resume != "optional" || pickKind(yes, 0) != "yes" {
		t.Fatalf("fixture lost its trigger_optional shape: %+v", yes)
	}
	if manaHoistAnswer(col) == "" {
		t.Fatalf("fixture lost its mana colour pick: %+v", col)
	}
	got := XAnswers([]rules.OracleDecision{yes, col}, 2, nil)
	want := [][]XAnswer{nil, {{0, "choice", "White"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (the boolean must script nothing; the colour pick leads)", got, want)
	}

	// A lone optional-trigger boolean still answers XMage's chooseUse: the
	// routing must not drop a boolean whose ask genuinely fires.
	got = XAnswers([]rules.OracleDecision{yes}, 2, nil)
	want = [][]XAnswer{nil, {{0, "choice", "yes"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lone boolean = %#v, want %#v", got, want)
	}

	// A placement ask (askTriggerOptional, no ResumeKind) is a different
	// decision; even before a colour pick it keeps the yes/no script.
	placed := yes
	placed.Resume = ""
	got = XAnswers([]rules.OracleDecision{placed, col}, 2, nil)
	want = [][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "White"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placement boolean = %#v, want %#v", got, want)
	}
}

// TestManaColourPickLeadsTriggerOrderSpan: a same-source trigger-order span
// whose step later asks a mana colour pick scripts the colour answer alone --
// XMage's mana trigger fires while the span's inert rule texts sit at the
// queue head and its colour dialog pops the first one. A span with no colour
// ask in its step keeps the inert text form, and a distinct-source span (the
// steering name form) is untouched by the hoist.
func TestManaColourPickLeadsTriggerOrderSpan(t *testing.T) {
	order, decline, col := ashlingOrder(2), ashlingDecline(2), manaColourPick(2)
	// Precondition: the span really is the same-source (inert text) form, and
	// the step's later decision really is a mana colour pick.
	if triggerOrderNamesDistinct(order) {
		t.Fatal("fixture must be the same-source span whose picks cannot be steered by name")
	}
	if manaHoistAnswer(col) == "" {
		t.Fatalf("fixture lost its mana colour pick: %+v", col)
	}
	got := XAnswers([]rules.OracleDecision{order, decline, col}, 3, nil)
	want := [][]XAnswer{nil, nil, {{0, "choice", "White"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (colour leads, inert texts dropped)", got, want)
	}

	// Without the colour ask the step keeps the measured inert text form.
	got = XAnswers([]rules.OracleDecision{order, decline}, 3, nil)
	want = [][]XAnswer{nil, nil, triggerOrderTextAnswers(order)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("span alone = %#v, want the inert texts %#v", got, want)
	}

	// A distinct-source span is not hoisted: its step keeps the shared-step
	// demotion semantics (the name form is rewritten back to the inert text
	// whenever the step scripts other asks, pre-existing behaviour).
	distinct := order
	distinct.PickRefs = []string{"p0:Ashling, Rimebound", "p0:Ashling, Rekindled"}
	if !triggerOrderNamesDistinct(distinct) {
		t.Fatal("fixture must have distinct source names")
	}
	got = XAnswers([]rules.OracleDecision{distinct, decline, col}, 3, nil)
	want = [][]XAnswer{nil, nil, append(triggerOrderTextAnswers(distinct), XAnswer{0, "choice", "White"})}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("distinct-source span = %#v, want %#v", got, want)
	}
}
