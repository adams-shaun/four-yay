package oraclegen

import (
	"encoding/json"
	"testing"
)

func TestPassToStepActiveRoundTrips(t *testing.T) {
	want := Step{Op: "pass_to", Step: "upkeep", Active: "p0"}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Step
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Op != want.Op || got.Step != want.Step || got.Active != want.Active {
		t.Fatalf("step round trip = %+v, want %+v", got, want)
	}
}
