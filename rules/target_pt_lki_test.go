package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestMovedTargetPowerReadsLastKnownInformation pins CR 608.2h on a
// resolution's own departed target: Swords to Plowshares' "its controller
// gains life equal to its power" reads the Giant Growth-pumped power the
// creature last had on the battlefield (5), not the printed 2 of the card
// now in exile. The P/T is captured at the departure boundary
// (Ctx.TargetPTLKI), the twin of the target-counters look-back.
func TestMovedTargetPowerReadsLastKnownInformation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	raw := `{"name":"stp","setup":{"p0":{"battlefield":["Grizzly Bears"],"hand":["Giant Growth","Swords to Plowshares"]}},
	 "steps":[{"op":"cast","seat":0,"card":"p0:Giant Growth","mana":"G","targets":["p0:Grizzly Bears"]},{"op":"resolve"},
	  {"op":"cast","seat":0,"card":"p0:Swords to Plowshares","mana":"W","targets":["p0:Grizzly Bears"]},{"op":"resolve"}],
	 "expect":[{"card":"p0:Grizzly Bears","zone":"exile"},{"life":{"p0":25}}]}`
	var sc oracleScenario
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatal(err)
	}
	if fails, tr, _ := runOracleScenario(reg, sc); len(fails) > 0 {
		t.Fatalf("%s\n  transcript:\n    %s", strings.Join(fails, "\n"), strings.Join(tr, "\n    "))
	}
}
