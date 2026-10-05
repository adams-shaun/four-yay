package compliance

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMergeVerdictsPreservesRulingOnlyForSameDivergence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		newDetail  string
		wantRuling bool
	}{
		{name: "same signature at another checkpoint", newDetail: `step 2 p1.life: gorge "20", xmage "17"`, wantRuling: true},
		{name: "changed values", newDetail: `step 1 p1.life: gorge "20", xmage "16"`, wantRuling: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old := VerdictRow{
				Card: "Tarnation Vista", Template: "scenario", Status: StatusXMageWrong,
				ScenarioSHA: "old-scenario", Detail: `step 1 p1.life: gorge "20", xmage "17"`,
				Frozen: []Frozen{{At: "step 1", Field: "p1.life", Value: "20"}},
				Ruling: "XMage omits the Cave subtype", Review: "confirmed",
			}
			if err := MergeVerdicts(dir, []VerdictRow{old}); err != nil {
				t.Fatal(err)
			}
			newRow := VerdictRow{
				Card: old.Card, Template: old.Template, Status: StatusDiverge,
				ScenarioSHA: "new-scenario", Detail: tc.newDetail,
			}

			readOut, writeOut, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdout
			os.Stdout = writeOut
			err = MergeVerdicts(dir, []VerdictRow{newRow})
			os.Stdout = previous
			_ = writeOut.Close()
			output, readErr := io.ReadAll(readOut)
			_ = readOut.Close()
			if err != nil {
				t.Fatal(err)
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			rows, err := LoadVerdicts(dir)
			if err != nil {
				t.Fatal(err)
			}
			got := rows[old.Card][old.Template]
			if (got.Ruling != "") != tc.wantRuling {
				t.Fatalf("ruling = %q, want preserved=%v", got.Ruling, tc.wantRuling)
			}
			if tc.wantRuling {
				if got.Status != StatusXMageWrong || got.Review != "confirmed" || len(got.Frozen) != 1 {
					t.Fatalf("classification/expectation not preserved: %+v", got)
				}
				if got.ScenarioSHA != newRow.ScenarioSHA {
					t.Fatalf("new verdict row not written: scenario_sha=%q", got.ScenarioSHA)
				}
			} else {
				if got.Status != StatusDiverge || got.Review != "" {
					t.Fatalf("changed divergence retained classification: %+v", got)
				}
				if !strings.Contains(string(output), "dropped ruling for Tarnation Vista (scenario): divergence changed") {
					t.Fatalf("missing dropped-ruling report: %q", output)
				}
			}
		})
	}
}
