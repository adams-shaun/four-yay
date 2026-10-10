package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestXAnswersManaColourOptionAmounts: a single-unit mana_colour ask whose
// offered colour span exceeds one colour (Muerra, Trash Tactician's Add R /
// Add G option pair) is XMage's multi-amount distribution with one message
// per OFFERED colour, in the engine's option order -- so the amounts are per
// option, 1 for the picked one and 0 for each unpicked one. A WUBRG-5 list
// would fill the first messages with the wrong colours' zeros (agent
// 20261009T041408Z, cluster C2; HYPOTHESIS: the message order is the
// engine's option order).
func TestXAnswersManaColourOptionAmounts(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "mana_color",
		Options: 2, Min: 1, Max: 1,
		Picks:       []string{"Add R"},
		PickIdx:     []int{0},
		PickRefs:    []string{"p0:Muerra, Trash Tactician"},
		PickKinds:   []string{"mana"},
		ManaColours: []string{"R", "G"},
	}
	// Precondition: the ask really spans more than one colour option, so the
	// new routing and a single-colour ask are distinguishable.
	if len(d.ManaColours) <= 1 {
		t.Fatalf("precondition: the ask offers %v colour options", d.ManaColours)
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)[0]
	want := []XAnswer{{0, "amount", "1"}, {0, "amount", "0"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("answer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestXAnswersManaColourOptionAmountsUnpickedOption: the second option
// picked keeps its own position's amount, never the first message's.
func TestXAnswersManaColourOptionAmountsUnpickedOption(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "mana_color",
		Options: 2, Min: 1, Max: 1,
		Picks:       []string{"Add G"},
		PickIdx:     []int{1},
		PickRefs:    []string{"p0:Muerra, Trash Tactician"},
		PickKinds:   []string{"mana"},
		ManaColours: []string{"R", "G"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)[0]
	want := []XAnswer{{0, "amount", "0"}, {0, "amount", "1"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("answer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestXAnswersSingleColourManaAskUnchanged: a single-colour mana_colour ask
// (one offered colour) is still the ordinary colour choice XMage's colour
// dialog consumes; the multi-amount routing must not touch it.
func TestXAnswersSingleColourManaAskUnchanged(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "mana_color",
		Options: 1, Min: 1, Max: 1,
		Picks:     []string{"Add R"},
		PickIdx:   []int{0},
		PickKinds: []string{"mana"},
	}
	out := XAnswers([]rules.OracleDecision{d}, 1, nil)
	// xanswers returns nil when no ask is scripted.
	var got []XAnswer
	if len(out) > 0 {
		got = out[0]
	}
	for _, a := range got {
		if a.Kind == "amount" {
			t.Fatalf("single-colour ask routed to the multi-amount dialog: %+v", got)
		}
	}
	// The forced one-option ask XMage never poses emits nothing (the
	// pre-existing behaviour this ticket leaves alone).
	if len(got) != 0 {
		t.Fatalf("single-colour ask answers changed shape: %+v", got)
	}
}

// TestCombatDamageAnswersTrampleAssignment: a 6/5 trample attacker blocked
// by a 2/2 has its deterministic assignment scripted as one amount answer
// per blocker, leading the pass_to step that crosses combat damage (Ojer
// Kaslem's row; agent 20261009T041408Z, cluster C2).
func TestCombatDamageAnswersTrampleAssignment(t *testing.T) {
	sc := Scenario{Steps: []Step{
		{Op: "attack", Seat: 0, Attackers: []string{"p0:Ojer Kaslem, Deepest Growth"}, Defender: "p1"},
		{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Grizzly Bears", "p0:Ojer Kaslem, Deepest Growth"}}},
		{Op: "pass_to", Seat: 0, Step: "main2"},
		{Op: "resolve"},
	}}
	// Precondition: the block step really blocks the attacker named in the
	// attack step, and the snapshot carries the P/T the assignment reads.
	if sc.Steps[1].Blocks[0][1] != sc.Steps[0].Attackers[0] {
		t.Fatalf("precondition: block %v does not name attacker %v",
			sc.Steps[1].Blocks[0][1], sc.Steps[0].Attackers[0])
	}
	res := rules.OracleResult{Decisions: []rules.OracleDecision{{
		Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1,
		Picks: []string{"Wastes"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Wastes#27"}, PickKinds: []string{"dig"}, Resume: "dig",
	}}, Snapshots: []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{}},
		{Permanents: []rules.OracleSnapPerm{
			{Ref: "p0:Ojer Kaslem, Deepest Growth", PT: "6/5", Keywords: []string{"Trample"}},
			{Ref: "p1:Grizzly Bears", PT: "2/2"},
		}},
		{Permanents: []rules.OracleSnapPerm{
			{Ref: "p0:Ojer Kaslem, Deepest Growth", PT: "6/5", Keywords: []string{"Trample"}},
			{Ref: "p1:Grizzly Bears", PT: "2/2"},
		}},
		{Permanents: []rules.OracleSnapPerm{}},
	}}
	// The WIRE, not a hand-built slice: XAnswersForScenario is the only path
	// that serves rows (gen_composite.go wires the prepend into it), so the
	// test asserts its return. If the wire is reverted, the decision stream
	// at step 2 loses its leading assignment answers and this test fails.
	out := XAnswersForScenario(res, sc, nil, nil)
	if len(out) != len(sc.Steps) {
		t.Fatalf("wired answers cover %d steps, want %d", len(out), len(sc.Steps))
	}
	// The dialog's messages are the blockers only; the unassigned 4 tramples
	// to the defender by itself and is never an option. The assignment
	// answers lead the step's own scripted answer (the Dig's land pick).
	if got := out[2]; len(got) != 2 || got[0] != (XAnswer{0, "amount", "2"}) {
		t.Fatalf("trample assignment answers = %+v, want [amount 2] leading step 2", out[2])
	}
	if out[2][1] != (XAnswer{0, "choice", "Wastes"}) {
		t.Fatalf("the step's own answers no longer follow the assignment: %+v", out[2])
	}
}

// TestCombatDamageAnswersNoDialogWhenFullyBlocked: a single blocker that
// absorbs all the damage leaves no dialog on either side, so no answers are
// synthesized.
func TestCombatDamageAnswersNoDialogWhenFullyBlocked(t *testing.T) {
	sc := Scenario{Steps: []Step{
		{Op: "attack", Seat: 0, Attackers: []string{"p0:Grizzly Bears"}, Defender: "p1"},
		{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Grizzly Bears", "p0:Grizzly Bears"}}},
		{Op: "pass_to", Seat: 0, Step: "main2"},
		{Op: "resolve"},
	}}
	res := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{}},
		{Permanents: []rules.OracleSnapPerm{
			{Ref: "p0:Grizzly Bears", PT: "2/2", Keywords: []string{"Trample"}},
			{Ref: "p1:Grizzly Bears", PT: "2/2"},
		}},
		{Permanents: []rules.OracleSnapPerm{}},
		{Permanents: []rules.OracleSnapPerm{}},
	}}
	out := make([][]XAnswer, 4)
	XAnswersForScenario(res, sc, nil, nil)
	prependCombatDamageAnswers(res, sc, out)
	for i, as := range out {
		if len(as) != 0 {
			t.Fatalf("step %d carries %+v; a fully blocked trample attacker poses no dialog", i, as)
		}
	}
}

// TestCombatDamageAnswersNoneWithoutTrample: without trample the engine
// poses its own CR 510.1c division or auto-assigns; the synthesis must stay
// out of both.
func TestCombatDamageAnswersNoneWithoutTrample(t *testing.T) {
	sc := Scenario{Steps: []Step{
		{Op: "attack", Seat: 0, Attackers: []string{"p0:Craw Wurm"}, Defender: "p1"},
		{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Grizzly Bears", "p0:Craw Wurm"}}},
		{Op: "pass_to", Seat: 0, Step: "main2"},
		{Op: "resolve"},
	}}
	res := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{}},
		{Permanents: []rules.OracleSnapPerm{
			{Ref: "p0:Craw Wurm", PT: "6/4"},
			{Ref: "p1:Grizzly Bears", PT: "2/2"},
		}},
		{Permanents: []rules.OracleSnapPerm{}},
		{Permanents: []rules.OracleSnapPerm{}},
	}}
	out := make([][]XAnswer, 4)
	XAnswersForScenario(res, sc, nil, nil)
	prependCombatDamageAnswers(res, sc, out)
	for i, as := range out {
		if len(as) != 0 {
			t.Fatalf("step %d carries %+v; a non-trample attacker is not synthesized", i, as)
		}
	}
}
