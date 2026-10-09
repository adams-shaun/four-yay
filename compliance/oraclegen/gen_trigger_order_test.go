package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// triggerOrderDecision is a two-trigger order ask whose sources are named
// objects, the shape gorge records for a controller's simultaneous triggers.
func triggerOrderDecision(step int, refs ...string) rules.OracleDecision {
	d := rules.OracleDecision{Step: step, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Options: len(refs), Min: len(refs), Max: len(refs), PickIdx: make([]int, len(refs)),
		PickKinds: make([]string, len(refs))}
	for k, ref := range refs {
		d.Picks = append(d.Picks, "Source: When this enters, do a thing.")
		d.PickRefs = append(d.PickRefs, ref)
		d.PickKinds[k] = "trigger"
		d.PickIdx[k] = k
	}
	return d
}

// TestTriggerOrderNamesSourceWhenAlone pins the Adrenaline Jockey/trigger#0.1
// drift fix: a step whose only scripted answers are a trigger order's picks
// names each pick's source object (XMage's chooseTriggeredAbility matches a
// choice against the ability's rule text or the source's NAME; gorge's
// TriggerDescription$ is XMage's wording only by coincidence, so the text
// form misses and XMage's fallback player orders the stack itself). The last
// pick is dropped: XMage pushes the last remaining ability without an ask.
func TestTriggerOrderNamesSourceWhenAlone(t *testing.T) {
	d := triggerOrderDecision(0, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	d.Picks[0] = "Adrenaline Jockey: Whenever you activate an exhaust ability, put a +1/+1 counter on this creature."
	d.Picks[1] = "Rangers' Refueler: Whenever you activate an exhaust ability, draw a card."
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{0, "choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (source names, last pick dropped)", got, want)
	}
}

// TestTriggerOrderSameSourceDuplicatesStripsOrdinal pins the "#2" ordinal
// strip: XMage matches the source's plain name, so a second same-named
// source's pick answers with that name too.
func TestTriggerOrderSameSourceDuplicatesStripsOrdinal(t *testing.T) {
	d := triggerOrderDecision(0, "p0:Lifecreed Duo#2", "p0:Bartz and Boko")
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{0, "choice", "Lifecreed Duo"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v", got, want)
	}
}

// TestTriggerOrderSourcelessFallsBackToText pins the sourceless fallback: a
// pick whose ref is not a scenario ref (PickRefs carries the label) keeps the
// old rule-text answer.
func TestTriggerOrderSourcelessFallsBackToText(t *testing.T) {
	d := triggerOrderDecision(0, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	d.PickRefs[0] = "casualty copy trigger"
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{0, "choice", "When this enters, do a thing."}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v", got, want)
	}
}

// TestTriggerOrderSharedStepKeepsInertText pins the demotion: when the step
// also scripts other asks, the order's answers stay the old inert rule text.
// A plain name is consumable by any later makeChoose XMage poses, so a
// leftover one (the order ask XMage posed at another point than gorge
// recorded) derails them; the text form is not consumable. Measured:
// Baron Strucker/static#0.0 replays agree only with the text form.
func TestTriggerOrderSharedStepKeepsInertText(t *testing.T) {
	order := triggerOrderDecision(1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	// A target ask at the same step shares the answer list with the order.
	target := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#9"},
		PickIdx: []int{0}, PickKinds: []string{"search"}}
	got := XAnswers([]rules.OracleDecision{target, order}, 2, nil)
	want := [][]XAnswer{
		nil,
		{{0, "target", "Wastes"}, {0, "choice", "When this enters, do a thing."}, {0, "choice", "When this enters, do a thing."}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (shared step keeps the inert text form)", got, want)
	}
}

// TestTriggerOrderSpansKeepDecisionOrder pins the in-place demotion: the
// demoted text answers sit exactly where the name answers would have been, so
// the answer queue keeps gorge's decision order.
func TestTriggerOrderSpansKeepDecisionOrder(t *testing.T) {
	target := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#9"},
		PickIdx: []int{0}, PickKinds: []string{"search"}}
	order := triggerOrderDecision(1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	got := XAnswers([]rules.OracleDecision{order, target}, 2, nil)
	want := [][]XAnswer{
		nil,
		{{0, "choice", "When this enters, do a thing."}, {0, "choice", "When this enters, do a thing."}, {0, "target", "Wastes"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v", got, want)
	}
}
