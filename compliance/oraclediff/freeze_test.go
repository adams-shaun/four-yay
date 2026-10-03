package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func shockResult() rules.OracleResult {
	before := snap("setup")
	after := snap("step 1 (resolve)")
	after.Players[0].Graveyard = []string{"Shock"}
	after.Players[1].Graveyard = []string{"Grizzly Bears"}
	after.Permanents = nil
	return rules.OracleResult{Snapshots: []rules.OracleSnapshot{before, after}}
}

// TestFreezeKeepsOnlyChangedFields: the expectation names the fields the
// scenario moved, so a change to anything else does not stale the verdict
// (section 11.3 C3), while a change to a frozen field does.
func TestFreezeKeepsOnlyChangedFields(t *testing.T) {
	res := shockResult()
	fz := Freeze(res)
	got := map[string]string{}
	for _, f := range fz {
		got[f.At+" "+f.Field] = f.Value
	}
	want := map[string]string{
		" checkpoints":                  "setup | step 1 (resolve)",
		"step 1 (resolve) p0.graveyard": "[Shock]",
		"step 1 (resolve) p1.graveyard": "[Grizzly Bears]",
		"step 1 (resolve) permanents-":  "c1 o1 Grizzly Bears [bear creature] {G} 2/2",
	}
	if len(got) != len(want) {
		t.Fatalf("frozen %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if ok, why := Meets(fz, res); !ok {
		t.Fatalf("own result: %s", why)
	}

	// An untouched field moving in every checkpoint (a library-size change,
	// a fixture's rendering) leaves the verdict standing.
	moved := shockResult()
	for i := range moved.Snapshots {
		moved.Snapshots[i].Players[1].LibraryCount = 30
		moved.Snapshots[i].Players[0].Life = 19
	}
	if ok, why := Meets(fz, moved); !ok {
		t.Fatalf("an unfrozen field staled the verdict: %s", why)
	}

	// A frozen field, a new step failure or another checkpoint sequence
	// does not.
	wrong := shockResult()
	wrong.Snapshots[1].Players[1].Graveyard = nil
	if ok, _ := Meets(fz, wrong); ok {
		t.Error("a frozen field changed and Meets passed")
	}
	failed := shockResult()
	failed.Fails = []string{"step 0 (cast): refused"}
	if ok, _ := Meets(fz, failed); ok {
		t.Error("a new step failure passed")
	}
	short := shockResult()
	short.Snapshots = short.Snapshots[:1]
	if ok, _ := Meets(fz, short); ok {
		t.Error("a missing checkpoint passed")
	}
}
