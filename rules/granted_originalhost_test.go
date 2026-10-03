package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestGrantedAbilityOriginalHostNamesTheGrantor pins Forge's OriginalHost in
// an Equipment-granted activated ability: the cost part (Sac<1/OriginalHost>,
// tapXType<1/OriginalHost>) and the resolution referent (DamageSource$ /
// Defined$ OriginalHost) both name the GRANTING Equipment, never the
// equipped creature the ability is activated from. Before the bind the cost
// parts matched nothing and neither ability was offered.
func TestGrantedAbilityOriginalHostNamesTheGrantor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for name, raw := range map[string]string{
		// Blazing Torch: "{T}, Sacrifice Blazing Torch: Blazing Torch deals 2
		// damage to any target" -- the Sac part pays with the Torch and the
		// damage source is the Torch.
		"blazing-torch": `{"name":"blazing-torch","setup":{"p0":{"battlefield":["Blazing Torch","Grizzly Bears"]}},
		 "steps":[{"op":"activate","seat":0,"card":"p0:Blazing Torch","mana":"C","targets":["p0:Grizzly Bears"],"ability":"Equip"},{"op":"resolve"},
		  {"op":"activate","seat":0,"card":"p0:Grizzly Bears","targets":["p1"],"ability":"damage"},{"op":"resolve"}],
		 "expect":[{"life":{"p1":18}},{"card":"p0:Blazing Torch","zone":"graveyard"},{"card":"p0:Grizzly Bears","zone":"battlefield","tapped":true}]}`,
		// Fishing Pole: "{1}, {T}, Tap Fishing Pole: Put a bait counter on
		// Fishing Pole" -- the tap part taps the Pole and the counter lands
		// on the Pole, not the Bears.
		"fishing-pole": `{"name":"fishing-pole","setup":{"p0":{"battlefield":["Fishing Pole","Grizzly Bears"]}},
		 "steps":[{"op":"activate","seat":0,"card":"p0:Fishing Pole","mana":"CC","targets":["p0:Grizzly Bears"],"ability":"Equip"},{"op":"resolve"},
		  {"op":"activate","seat":0,"card":"p0:Grizzly Bears","mana":"C","ability":"bait"},{"op":"resolve"}],
		 "expect":[{"card":"p0:Fishing Pole","tapped":true,"counters":{"bait":1}},{"card":"p0:Grizzly Bears","tapped":true,"counters":{"bait":0}}]}`,
	} {
		var sc oracleScenario
		if err := json.Unmarshal([]byte(raw), &sc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fails, tr, _ := runOracleScenario(reg, sc); len(fails) > 0 {
			t.Errorf("%s:\n  %s\n  transcript:\n    %s", name, strings.Join(fails, "\n  "), strings.Join(tr, "\n    "))
		}
	}
}
