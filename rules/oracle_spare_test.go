package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleSparePoolIsInvisible: RunOracleScenarioJSON recycles engine
// storage across calls (oracleSparePool). A scenario played on recycled
// arrays -- after a different scenario dirtied them -- must report exactly
// what a fresh-array run (runOracleScenario, no Spare) reports.
func TestOracleSparePoolIsInvisible(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(cutInTwoTargetScenario))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true
	fails, transcript, run := runOracleScenario(reg, sc)
	want := OracleResult{Fails: fails, Transcript: transcript,
		Snapshots: append([]OracleSnapshot{}, run.snaps...), Decisions: append([]OracleDecision{}, run.decisions...)}
	for i := 0; i < 3; i++ {
		if _, err := RunOracleScenarioJSON(reg, []byte(castAbilityIndexScenario)); err != nil {
			t.Fatal(err)
		}
		got, err := RunOracleScenarioJSON(reg, []byte(cutInTwoTargetScenario))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d on recycled storage differs from a fresh run:\ngot  %+v\nwant %+v", i, got, want)
		}
	}
}
