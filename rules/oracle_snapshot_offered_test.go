package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestOracleSnapshotOfferedAtPriorityCheckpoint(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"offered-bolt","cr":["601.2"],"why":"snapshot offered observation","setup":{"p0":{"hand":["Lightning Bolt"],"battlefield":["Mountain","Mogg Fanatic"]}},"steps":[{"op":"mana","seat":0,"mana":"R"}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) != 2 || res.Snapshots[1].Checkpoint != "step 0 (mana)" {
		t.Fatalf("precondition: checkpoints = %+v", res.Snapshots)
	}
	bolt, activate := false, false
	for _, o := range res.Snapshots[1].Offered {
		bolt = bolt || (o.Source == "p0:Lightning Bolt" && o.Kind == "cast")
		activate = activate || (o.Source == "p0:Mogg Fanatic" && o.Kind == "activate")
	}
	if !bolt || !activate {
		t.Fatalf("precondition: offered actions = %+v, want Bolt cast and Mogg activate", res.Snapshots[1].Offered)
	}
}
