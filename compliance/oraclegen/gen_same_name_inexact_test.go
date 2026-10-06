package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// A same-kind same-name pick whose ref is rank-derived (a card that was in
// the library/hand at setup, or entered mid-game by rank) has NO sound exact
// answer: gorge ranks it by ObjID across all zones, while XMage's
// registerAliases ranks by zone traversal battlefield->hand->graveyard->
// exile->library, and for a shuffled library the two disagree. Emitting the
// ref there is worse than a bare name -- it can silently bind a DIFFERENT
// physical card. The generator must therefore refuse the alias for such a
// pick (ClassifySameName.Unanswerable) and fall back to the bare label,
// which the same-name census counts as Unproven, never as resolved.
//
// The exact-ref case is the positive control: the same shape with an exact
// (setup battlefield) ref DOES carry the alias. Without the provenance bit
// the two are indistinguishable, which is the defect this pins.
func TestXAnswersRankDerivedSameNamePickHasNoAlias(t *testing.T) {
	exact := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#2"},
		PickIdx: []int{1}, PickKinds: []string{"card"},
		OptionRefs:      []string{"p0:Wastes", "p0:Wastes#2"},
		PickRefsInexact: []bool{true},
	}
	// Precondition: this really is a same-kind ambiguity (two distinct
	// cards of one name), not an unambiguous pick that trivially falls back.
	if p := ClassifySameName(exact, 0); !p.Ambiguous || p.Distinct != 2 {
		t.Fatalf("fixture must be an ambiguous two-card same-name pick: %+v", p)
	}
	if p := ClassifySameName(exact, 0); !p.Unanswerable {
		t.Fatalf("a rank-derived ref must be Unanswerable, got %+v", p)
	}
	if p := ClassifySameName(exact, 0); p.Alias != "" {
		t.Fatalf("a rank-derived ref must not carry an alias, got %q", p.Alias)
	}
	got := XAnswers([]rules.OracleDecision{exact}, 1, nil)
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("answers = %#v, want one", got)
	}
	if got[0][0].Value != "Wastes" {
		t.Fatalf("rank-derived answer = %q, want the bare label %q (no mis-bindable ref)", got[0][0].Value, "Wastes")
	}

	// Positive control: the identical decision marked exact still emits the
	// exact ref, so the test would fail if the provenance bit were ignored.
	exact.PickRefsInexact = []bool{false}
	if p := ClassifySameName(exact, 0); p.Unanswerable || p.Alias != "@p0:Wastes#2" {
		t.Fatalf("an exact ref must still emit its alias, got %+v", p)
	}
	if got := XAnswers([]rules.OracleDecision{exact}, 1, nil); got[0][0].Value != "@p0:Wastes#2" {
		t.Fatalf("exact-ref answer = %q, want %q", got[0][0].Value, "@p0:Wastes#2")
	}
}

// The copy marker is independent of ref exactness: a unique token-or-card
// split resolves by isCopy() and never needs an alias, so an inexact ref
// still gets its marker. This is the Joo Dee class -- the original among a
// same-named token copy -- and it must keep working when the pick's ref is
// only rank-derived.
func TestXAnswersCopyMarkerSurvivesInexactRef(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Joo Dee, One of Many"}, PickRefs: []string{"p0:Joo Dee, One of Many"},
		PickIdx: []int{0}, PickKinds: []string{"card"},
		OptionRefs:      []string{"p0:Joo Dee, One of Many", "p0:token:Joo Dee, One of Many#1"},
		PickRefsInexact: []bool{true},
	}
	if !ClassifySameName(d, 0).Ambiguous {
		t.Fatalf("fixture must be an ambiguous token-or-card pick: %+v", d)
	}
	if got := XAnswers([]rules.OracleDecision{d}, 1, nil); got[0][0].Value != "Joo Dee, One of Many[no copy]" {
		t.Fatalf("inexact original answer = %q, want %q", got[0][0].Value, "Joo Dee, One of Many[no copy]")
	}
}
