package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestPerPlayerTargetAskRouting pins the mid-resolution per-player target
// ask: the engine records it as a KChoose whose options carry the
// target-controller groups, and its declined answer is one target skip per
// offered controller seat -- NOT the generic [choice_skip] the declinedChoice
// arm would emit.
func TestPerPlayerTargetAskRouting(t *testing.T) {
	kaya := rules.OracleDecision{
		Kind: "choose_n", Resume: "choice", Min: 0, Max: 1, Seat: 0,
		OptionRefs:   []string{"p1:Grizzly Bears"},
		OptionGroups: []string{"target-controller-1"},
	}
	if !perPlayerTargetAsk(kaya) {
		t.Fatal("precondition: the grouped ask is not recognised as a per-player target ask")
	}
	if got := perControllerTargetAnswers(kaya); len(got) != 1 ||
		got[0] != (XAnswer{0, "target", "[target_skip]"}) {
		t.Fatalf("declined ask answers = %+v, want one target skip", got)
	}
	// The generic declined effect choose (no groups) stays on the choice
	// queue.
	generic := kaya
	generic.OptionGroups = nil
	if perPlayerTargetAsk(generic) {
		t.Fatal("a group-less choose was recognised as a target ask")
	}
	// A partly answered ask emits the pick for its seat and skips the rest,
	// in seat order.
	partly := kaya
	partly.OptionRefs = []string{"p1:Grizzly Bears", "p2:Grizzly Bears"}
	partly.OptionGroups = []string{"target-controller-1", "target-controller-2"}
	partly.PickRefs = []string{"p2:Grizzly Bears"}
	got := perControllerTargetAnswers(partly)
	want := []XAnswer{{0, "target", "[target_skip]"}, {0, "target", "p2:Grizzly Bears"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("partly answered ask = %+v, want %+v", got, want)
	}
	// The routing owns the declined ask and emits the target queue token.
	r := newAnswerRouting([]rules.OracleDecision{kaya})
	as, owned := r.route(0)
	if !owned || len(as) != 1 || as[0].Kind != "target" || as[0].Value != "[target_skip]" {
		t.Fatalf("route = (%+v, %v), want one target skip owned", as, owned)
	}
}

// TestCurrentFaceTargetRef pins the transformed-permanent answer: a pick
// whose option label names a different object than its ref is answered by the
// ref's identity alias, while an ordinary "<name> (<seat>)" label keeps the
// name form.
func TestCurrentFaceTargetRef(t *testing.T) {
	jill := rules.OracleDecision{
		Kind: "target", Seat: 0,
		Picks:    []string{"Shiva, Warden of Ice (a)"},
		PickRefs: []string{"p0:Jill, Shiva's Dominant"},
	}
	if labelNamesRef(jill, 0) {
		t.Fatal("precondition: the transformed label names the ref's object")
	}
	if !currentFaceTargetRef(jill, 0) {
		t.Fatal("a transformed permanent's pick was not routed to its exact ref")
	}
	// The driver resolves the ref through its setup alias, so the routing
	// must emit the ref itself, not the front name.
	as := targetDecisionAnswers(newAnswerRouting([]rules.OracleDecision{jill}), 0)
	if len(as) != 1 || as[0].Kind != "target" || as[0].Value != "p0:Jill, Shiva's Dominant" {
		t.Fatalf("answers = %+v, want the exact ref", as)
	}
	ordinary := rules.OracleDecision{
		Kind: "target", Seat: 0,
		Picks:    []string{"Grizzly Bears (b)"},
		PickRefs: []string{"p1:Grizzly Bears"},
	}
	if !labelNamesRef(ordinary, 0) || currentFaceTargetRef(ordinary, 0) {
		t.Fatal("an ordinary <name> (<seat>) label was routed as a current-face ref")
	}
	if as := targetDecisionAnswers(newAnswerRouting([]rules.OracleDecision{ordinary}), 0); len(as) != 1 || as[0].Value != "Grizzly Bears" {
		t.Fatalf("ordinary answers = %+v, want the name form", as)
	}
	// An inexact ref is not bound by identity, so the name form stays.
	inexact := jill
	inexact.PickRefsInexact = []bool{true}
	if currentFaceTargetRef(inexact, 0) {
		t.Fatal("an inexact ref was routed to the identity alias")
	}
}

// TestTapOrUntapChoice pins the boolean XMage answer for a TapOrUntap
// election: option 0 is the state-changing choice (yes), option 1 the no-op.
func TestTapOrUntapChoice(t *testing.T) {
	d := rules.OracleDecision{Kind: "choose_n", Resume: "taporuntap", Seat: 0,
		Picks: []string{"Untap Iceberg Titan"}, PickIdx: []int{0}, PickKinds: []string{"untap"}}
	r := newAnswerRouting([]rules.OracleDecision{d})
	as, owned := r.route(0)
	if !owned || len(as) != 1 || as[0].Kind != "choice" || as[0].Value != "yes" {
		t.Fatalf("route = (%+v, %v), want choice yes", as, owned)
	}
	d.PickIdx = []int{1}
	if got := tapOrUntapChoice(d); got != "no" {
		t.Fatalf("no-op pick answer = %q, want no", got)
	}
}

// TestDeclinedChoiceStillOwnsGenericAsks is the control: the per-player
// routing must not swallow an ordinary declined choose.
func TestDeclinedChoiceStillOwnsGenericAsks(t *testing.T) {
	d := rules.OracleDecision{Kind: "choose_n", Resume: "choice", Min: 0, Max: 2, Seat: 0,
		OptionRefs: []string{"p0:Forest", "p0:Island"}}
	r := newAnswerRouting([]rules.OracleDecision{d})
	as, owned := r.route(0)
	if !owned || len(as) != 1 || as[0].Kind != "choice" || as[0].Value != "[choice_skip]" {
		t.Fatalf("generic declined ask route = (%+v, %v), want a choice skip", as, owned)
	}
	if strings.Contains(as[0].Value, "target") {
		t.Fatal("generic declined ask leaked to the target queue")
	}
}
