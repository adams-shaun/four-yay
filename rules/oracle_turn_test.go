package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestOracleScenarioDefaultTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(`{"setup":{"p0":{},"p1":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 || res.Snapshots[0].Turn != 1 || res.Snapshots[0].Step != "main1" {
		t.Fatalf("default setup checkpoint = %+v, want turn 1 main1", res.Snapshots)
	}
}

func TestOracleScenarioRequestedTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(`{"turn":4,"setup":{"p0":{},"p1":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 || res.Snapshots[0].Turn != 4 || res.Snapshots[0].Step != "main1" {
		t.Fatalf("requested setup checkpoint = %+v, want turn 4 main1", res.Snapshots)
	}
}

func TestOracleScenarioInvalidTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, turn := range []string{"0", "-1", "101"} {
		t.Run(turn, func(t *testing.T) {
			res, err := RunOracleScenarioJSON(reg, []byte(`{"turn":`+turn+`,"setup":{"p0":{},"p1":{}}}`))
			if err != nil || len(res.Fails) != 1 || !strings.Contains(res.Fails[0], "invalid scenario turn") {
				t.Fatalf("result = %+v, error = %v; want invalid scenario turn", res, err)
			}
		})
	}
}
