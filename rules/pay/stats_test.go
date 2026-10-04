package pay

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// A nil sink is inert, Merge sums (max for MaxNodes), and the text report is
// sorted so equal counters print identical bytes.
func TestPlanStatsMergeAndReport(t *testing.T) {
	t.Parallel()
	var nilSink *PlanStats
	nilSink.RecordBuild(false)
	nilSink.RecordOutcome(PlanOutcome{Reason: "insufficient", Nodes: 3})
	nilSink.RecordOffered(1)
	nilSink.RecordPlannedSubmission()
	nilSink.RecordFallback(FallbackCostChanged)

	a := &PlanStats{}
	a.RecordBuild(false)
	a.RecordOutcome(PlanOutcome{Reason: "unsupported", Detail: "cost:x"})
	a.RecordOutcome(PlanOutcome{Plan: &decision.PaymentPlan{}, Reason: "search_limit", Nodes: 9})
	a.RecordOffered(1)
	b := &PlanStats{}
	b.RecordBuild(true)
	b.RecordOutcome(PlanOutcome{Plan: &decision.PaymentPlan{}, Nodes: 4})
	b.RecordOutcome(PlanOutcome{Reason: "insufficient", Detail: "source:deferred", Nodes: 5})
	b.RecordOffered(1)
	b.RecordPlannedSubmission()
	b.RecordFallback(FallbackSourceChanged)
	b.RecordFallback(FallbackCostChanged)

	sum := &PlanStats{}
	sum.Merge(a)
	sum.Merge(b)
	sum.Merge(nil)
	want := PlanStats{
		DecisionsBuilt: 2, PoolDeclined: 1, Candidates: 4, ActionsOffered: 2, PlansOffered: 2,
		ByReason: map[string]int{"unsupported": 1, "search_limit": 1, "ready": 1, "insufficient": 1},
		ByDetail: map[string]int{"cost:x": 1, "source:deferred": 1},
		Nodes:    18, MaxNodes: 9, SearchLimitHits: 1, PlannedSubmissions: 1,
		Fallbacks: map[string]int{FallbackCostChanged: 1, FallbackSourceChanged: 1},
	}
	if !reflect.DeepEqual(*sum, want) {
		t.Fatalf("merged = %+v\nwant   %+v", *sum, want)
	}
	var first, second bytes.Buffer
	if err := sum.WriteText(&first); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		second.Reset()
		if err := sum.WriteText(&second); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatalf("report is not stable:\n%s\n---\n%s", first.String(), second.String())
		}
	}
	const wantText = `payment plan stats:
  decisions built:       2
  pool declined builds:  1
  cast candidates:       4
  actions offered:       2
  plans offered:         2
  search nodes:          18 (max 9)
  search_limit hits:     1
  planned submissions:   1
  outcomes by reason:
    insufficient                             1
    ready                                    1
    search_limit                             1
    unsupported                              1
  outcomes by detail:
    cost:x                                   1
    source:deferred                          1
  fallbacks by reason:
    cost_changed                             1
    source_changed                           1
`
	if first.String() != wantText {
		t.Fatalf("report =\n%s\nwant\n%s", first.String(), wantText)
	}
}
