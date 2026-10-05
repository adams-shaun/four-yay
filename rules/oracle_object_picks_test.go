package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestOracleTranscriptRecordsSelectedObjectRefs(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Decisions {
		for _, ref := range d.ObjectPicks {
			if !strings.HasPrefix(ref, "p") || !strings.Contains(ref, "Grizzly Bears") {
				t.Fatalf("object pick is not a selected bears identity: %+v", d)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("scenario selected a Grizzly Bears target but transcript recorded no object identity")
	}
}
