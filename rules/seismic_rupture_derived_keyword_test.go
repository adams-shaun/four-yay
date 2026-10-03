package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestDamageAllFilterSeesGrantedFlying pins a resolving effect's keyword
// filter reading the layer-derived keywords (rules' EffectiveKeywords table):
// Seismic Rupture's "each creature without flying" spares a Grizzly Bears
// that Ajani, Caller of the Pride's -3 gave flying, and still hits an
// unmodified creature.
func TestDamageAllFilterSeesGrantedFlying(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	raw := `{"name":"rupture","setup":{"p0":{"battlefield":["Grizzly Bears","Ajani, Caller of the Pride"],"hand":["Seismic Rupture"]},"p1":{"battlefield":["Hill Giant"]}},
	 "steps":[{"op":"activate","seat":0,"card":"p0:Ajani, Caller of the Pride","targets":["p0:Grizzly Bears"],"ability":"double strike"},{"op":"resolve"},
	  {"op":"cast","seat":0,"card":"p0:Seismic Rupture","mana":"RCC"},{"op":"resolve"}],
	 "expect":[{"card":"p0:Grizzly Bears","zone":"battlefield","damage":0},{"card":"p1:Hill Giant","damage":2}]}`
	var sc oracleScenario
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatal(err)
	}
	if fails, tr, _ := runOracleScenario(reg, sc); len(fails) > 0 {
		t.Fatalf("%s\n  transcript:\n    %s", strings.Join(fails, "\n"), strings.Join(tr, "\n    "))
	}
}
