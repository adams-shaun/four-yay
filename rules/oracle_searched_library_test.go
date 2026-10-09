package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const oracleSearchedLibraryScenario = `{"name":"searched-library-opponent","cr":["603.10"],"why":"p1's tutor search fires Wan Shi Tong's row",
 "setup":{"p0":{"battlefield":["Wan Shi Tong, Librarian"]},"p1":{"hand":["Demonic Tutor"]}},
 "steps":[{"op":"pass_to","step":"main1","active":"p1"},{"op":"cast","seat":1,"card":"p1:Demonic Tutor","mana":"CB"},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass_to","decision":"priority"}]}`

func TestOracleSearchedLibraryOpponentFires(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(oracleSearchedLibraryScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("precondition: scenario did not play cleanly: %v", res.Fails)
	}
	onStack := false
	for i, s := range res.Snapshots {
		for _, e := range s.Stack {
			t.Logf("snap %d stack: %+v", i, e)
			if e.Kind == "ability" && e.Trigger == "1" && strings.Contains(strings.ToLower(e.Source), "wan shi tong") {
				onStack = true
			}
		}
	}
	if !onStack {
		t.Fatalf("Wan Shi Tong's SearchedLibrary trigger never appeared on the stack")
	}
}
