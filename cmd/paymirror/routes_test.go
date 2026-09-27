package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/paymirror"
)

func TestEquivalentReportWithMismatchingRouteIsFinding(t *testing.T) {
	report := &paymirror.Report{Card: "K'rrik", Routes: []paymirror.RouteResult{
		{Route: paymirror.RouteFloat, Status: paymirror.Equivalent},
		{Route: paymirror.RouteBase, Status: paymirror.Mismatch, Signature: "state_differs"},
	}}
	if status, _ := report.Verdict(); status != paymirror.Equivalent {
		t.Fatalf("fixture verdict = %s, want equivalent", status)
	}
	if !hasRouteMismatch(report) {
		t.Fatal("route mismatch was not selected for findings")
	}
	var got bytes.Buffer
	if err := writeReportFinding(json.NewEncoder(&got), paymirror.GameSpec{Seed: 2011, Commander: true}, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), `"status":"mismatch"`) || !strings.Contains(got.String(), `"signature":"state_differs"`) {
		t.Fatalf("finding omitted mismatching route: %s", got.String())
	}
	summary := paymirror.NewSummary()
	summary.AddRouteMismatchExample(paymirror.RouteBase, "seed=2011 seq=7 card=K'rrik")
	var rendered bytes.Buffer
	summary.WriteRouteMismatchExamples(&rendered)
	if !strings.Contains(rendered.String(), "base_option_window: seed=2011 seq=7 card=K'rrik\n") {
		t.Fatalf("route mismatch example missing from summary: %q", rendered.String())
	}
}

func TestEquivalentReportWithHorizonPanicIsFinding(t *testing.T) {
	report := &paymirror.Report{Card: "Ad Nauseam", Routes: []paymirror.RouteResult{
		{Route: paymirror.RouteFloat, Status: paymirror.Equivalent, Resolved: "skipped:panic:livelock",
			HorizonPanic: "both: livelock detected\n  at rules.(*livelockWatcher).abort rules/livelock.go:363"},
	}}
	if status, _ := report.Verdict(); status != paymirror.Equivalent {
		t.Fatalf("fixture verdict = %s, want equivalent", status)
	}
	if hasRouteMismatch(report) || !hasHorizonPanic(report) {
		t.Fatal("horizon panic was not selected for findings")
	}
	var got bytes.Buffer
	if err := writeReportFinding(json.NewEncoder(&got), paymirror.GameSpec{Seed: 1014}, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), `"horizon_panic":"both: livelock detected\n  at rules.(*livelockWatcher).abort`) {
		t.Fatalf("finding omitted the horizon panic: %s", got.String())
	}
}
