package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAsEntersChoiceReturnsPriorityToActivePlayer pins CR 117.3b after a
// permanent spell whose own "as this enters" replacement asks (Banner of
// Kinship's creature-type choice): the suspended resolution's completion
// resets priority to the active player, who can then cast a creature.
// Before, the next priority round went to the non-active player who had
// passed last, with that pass still counted.
func TestAsEntersChoiceReturnsPriorityToActivePlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	raw := `{"name":"banner-priority","setup":{"p0":{"battlefield":["Grizzly Bears","Savannah Lions"],"hand":["Banner of Kinship","Grizzly Bears"]}},
	 "steps":[{"op":"cast","seat":0,"card":"p0:Banner of Kinship","mana":"CCCCC"},{"op":"resolve","answers":[{"kind":"any","pick":["Bear"]}]},
	  {"op":"cast","seat":0,"card":"p0:Grizzly Bears#2","mana":"GC"},{"op":"resolve"}],
	 "expect":[{"card":"p0:Banner of Kinship","counters":{"FELLOWSHIP":1}},{"card":"p0:Grizzly Bears#2","zone":"battlefield","pt":"3/3"}]}`
	var sc oracleScenario
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatal(err)
	}
	if fails, tr, _ := runOracleScenario(reg, sc); len(fails) > 0 {
		t.Fatalf("%s\n  transcript:\n    %s", strings.Join(fails, "\n"), strings.Join(tr, "\n    "))
	}
}
