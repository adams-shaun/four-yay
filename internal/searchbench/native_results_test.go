package searchbench

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func TestSameRecordedActionIgnoresPaymentPlan(t *testing.T) {
	want := decision.Intent{Seq: 7, Player: 0, Payment: &decision.PaymentSelection{ActionID: "cast-a", Plan: decision.PaymentPlan{ID: "first"}}}
	got := decision.Intent{Seq: 7, Player: 0, Payment: &decision.PaymentSelection{ActionID: "cast-a", Plan: decision.PaymentPlan{ID: "second"}}}
	if !SameRecordedAction(got, want) {
		t.Fatal("equivalent payment actions did not match")
	}
	got.Payment.ActionID = "cast-b"
	if SameRecordedAction(got, want) {
		t.Fatal("different cast actions matched")
	}
}

func TestSummarizeNativeRunsExcludesSkipped(t *testing.T) {
	rows := []NativeRunResult{
		{GameID: "g", SourceKind: "spell", SourceCard: "A", Arm: ArmPIMC1, Sims: 4, Completed: 4, MatchRecorded: true},
		{GameID: "h", SourceKind: "attack", Arm: ArmPIMC1, Skipped: 1},
	}
	s, arm, err := SummarizeNativeRuns(rows)
	if err != nil || arm != ArmPIMC1 || s.Rows != 2 || s.Searched != 1 || s.Matched != 1 || s.Skipped != 1 {
		t.Fatalf("summary=%+v arm=%q err=%v", s, arm, err)
	}
	if score, ok := s.Agreement(); !ok || score != 1 {
		t.Fatalf("agreement=%v,%v", score, ok)
	}
}
