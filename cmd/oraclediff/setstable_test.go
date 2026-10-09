package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/adopt"
	"github.com/adams-shaun/gorge/compliance/gate"
)

func TestDistinctProblemCardsCountsCards(t *testing.T) {
	// Precondition: the problems below name two cards, one of them twice,
	// so the distinct count (2) differs from the problem count (3).
	probs := []gate.Problem{
		{Card: "Ember Hauler", Reason: "activate#0.0: gorge_wrong"},
		{Card: "Ember Hauler", Reason: "static#1.0: expectation unmet"},
		{Card: "Doom Blade", Reason: "no verdict"},
	}
	if n := adopt.DistinctProblemCards(probs); n != 2 {
		t.Fatalf("DistinctProblemCards = %d, want 2 (Ember Hauler fails two requirements and is one card)", n)
	}
	if n := adopt.DistinctProblemCards(nil); n != 0 {
		t.Fatalf("DistinctProblemCards(nil) = %d, want 0", n)
	}
}

func TestSetStatusJSONCarriesProblemCards(t *testing.T) {
	// The status child returns SetStatus lines as JSON; ProblemCards must
	// survive the round trip or the table reads 0 problems for every set.
	in := adopt.SetStatus{Set: "FDN", Cards: 305, Outstanding: 3, ProblemCards: 2}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"problem_cards":2`) {
		t.Fatalf("marshalled %s: problem_cards missing", b)
	}
	var out adopt.SetStatus
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ProblemCards != 2 {
		t.Fatalf("round trip lost ProblemCards: got %d, want 2", out.ProblemCards)
	}
}

func testCensus() *adopt.Census {
	return &adopt.Census{Config: &adopt.Config{Formats: []adopt.Format{
		{Name: "Standard"}, {Name: "Commander"},
	}}}
}

func TestSetsTable(t *testing.T) {
	cs := testCensus()
	rows := []adopt.SetStatus{
		// BLB: clean at A, one problem card at B (a card failing two
		// requirements, so 2 problems on 1 card of 305).
		{Set: "BLB", Released: "2024-08-02", Formats: []string{"Standard"},
			Cards: 305, Outstanding: 2, ProblemCards: 1, Buckets: map[string]int{adopt.BucketGorgeWrong: 1, adopt.BucketExpectation: 1}},
		// FDN: clean at both levels.
		{Set: "FDN", Released: "2024-11-15", Formats: []string{"Standard"},
			Cards: 305},
		// Commander set: ranks after every Standard set regardless of release.
		{Set: "SPM", Released: "2025-06-06", Formats: []string{"Commander"},
			Cards: 65},
	}
	b := []adopt.SetStatus{
		// BLB at B: clean, so the notes carry no "A:" prefix but the
		// fraction still shows 304/305 (a single regression reads).
		{Set: "BLB", Released: "2024-08-02", Formats: []string{"Standard"}, Cards: 305},
		{Set: "FDN", Released: "2024-11-15", Formats: []string{"Standard"}, Cards: 305},
		{Set: "SPM", Released: "2025-06-06", Formats: []string{"Commander"}, Cards: 65},
	}
	var buf bytes.Buffer
	writeSetsTable(&buf, cs, rows, b, "abc1234")
	got := buf.String()
	want := `| FDN | YES | 305/305 | — |
`
	if !strings.Contains(got, want) {
		t.Fatalf("clean set row wrong:\n%s", got)
	}
	if !strings.Contains(got, "| BLB | 304/305 | 305/305 | A: 1 gorge wrong, 1 expectation unmet |\n") {
		t.Fatalf("problem set row wrong:\n%s", got)
	}
	if strings.Index(got, "| FDN |") > strings.Index(got, "| BLB |") {
		t.Fatalf("newer Standard set should rank first:\n%s", got)
	}
	if strings.Index(got, "| SPM |") < strings.Index(got, "| FDN |") {
		t.Fatalf("Commander set should rank after Standard sets:\n%s", got)
	}
	// Determinism: the same rows render byte-identically.
	var again bytes.Buffer
	writeSetsTable(&again, cs, rows, b, "abc1234")
	if !bytes.Equal(buf.Bytes(), again.Bytes()) {
		t.Fatal("two renders of the same rows differ")
	}
}

func TestSetsTableBothLevelsDirtyAndUnmeasured(t *testing.T) {
	cs := testCensus()
	both := []adopt.SetStatus{
		{Set: "TMT", Released: "2025-02-14", Formats: []string{"Standard"},
			Cards: 100, Outstanding: 5, ProblemCards: 4,
			Buckets: map[string]int{adopt.BucketGorgeWrong: 3, adopt.BucketXMageLacks: 1, adopt.BucketDiverge: 1}},
	}
	var buf bytes.Buffer
	writeSetsTable(&buf, cs, both, both, "abc1234")
	got := buf.String()
	// A dirty level A prefixes the notes; the B summary follows.
	if !strings.Contains(got, "| TMT | 96/100 | 96/100 | A: 3 gorge wrong, 1 xmage lacks (hand scenario), 1 diverge; 3 gorge wrong, 1 xmage lacks (hand scenario), 1 diverge |\n") {
		t.Fatalf("both-levels-dirty row wrong:\n%s", got)
	}
	// A level that was not measured renders em dash in its column.
	var onlyB bytes.Buffer
	writeSetsTable(&onlyB, cs, nil, both, "abc1234")
	if !strings.Contains(onlyB.String(), "| TMT | — | 96/100 |") {
		t.Fatalf("unmeasured level column wrong:\n%s", onlyB.String())
	}
}

func TestBucketSummaryCapsAtFour(t *testing.T) {
	buckets := map[string]int{}
	for i, b := range adopt.Buckets {
		buckets[b] = i + 1 // later buckets are larger, so the LAST four win
	}
	got := bucketSummary(buckets)
	want := "13 level not built, 12 expectation unmet, 11 stale verdict, 10 review pending, +9 more"
	if got != want {
		t.Fatalf("bucketSummary = %q, want %q", got, want)
	}
}

func TestParseLevels(t *testing.T) {
	if ls, err := parseLevels("A,B"); err != nil || len(ls) != 2 || ls[0] != "A" || ls[1] != "B" {
		t.Fatalf("parseLevels(A,B) = %v, %v", ls, err)
	}
	if ls, err := parseLevels("B,B"); err != nil || len(ls) != 1 {
		t.Fatalf("parseLevels(B,B) = %v, %v, want one B", ls, err)
	}
	if _, err := parseLevels("C"); err == nil {
		t.Fatal("parseLevels(C) should fail")
	}
	if _, err := parseLevels(""); err == nil {
		t.Fatal("parseLevels(\"\") should fail")
	}
}
