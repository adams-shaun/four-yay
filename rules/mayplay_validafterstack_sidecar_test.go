package rules

import "testing"

// TestMayPlayValidAfterStackLibraryTopRunner: Glarb, Calamity's Augur's
// printed `MayPlay$ True | ValidAfterStack$ Spell.cmcGE4` library-top grant
// must offer a mana-value-4 spell and withhold a mana-value-1 one in a REAL
// game. The hand-built engines of mayplay_validafterstack_test.go carry no
// compiled-predicate sidecar, so they offered the cast while the runner's
// engine (which compiles every spec text on the board's cards) refused it:
// the sidecar answered a definite No for `Spell.<...>` off the stack and
// ignored the AsStack override. The precondition is the Shock control: the
// same board offers Glarb's own ability, so the checkpoint is live.
func TestMayPlayValidAfterStackLibraryTopRunner(t *testing.T) {
	for _, tc := range []struct {
		top, mana string
		want      string
	}{
		{"Hill Giant", "CCCR", "true"},
		{"Shock", "R", "false"},
	} {
		raw := `{"name":"glarb-top-` + tc.top + `","setup":{"p0":{"battlefield":["Glarb, Calamity's Augur"],"library_top":["` + tc.top + `"]},"p1":{}},"steps":[` +
			`{"op":"pass_to","seat":0,"step":"main1","decision":"priority"},` +
			`{"op":"mana","seat":0,"mana":"` + tc.mana + `"},` +
			`{"op":"pass_to","seat":0,"decision":"priority","expect":[` +
			`{"offered":{"seat":0,"kind":"activate","card":"p0:Glarb, Calamity's Augur","label":"Glarb, Calamity's Augur: Surveil 2."},"want":true},` +
			`{"offered":{"seat":0,"kind":"cast","card":"p0:` + tc.top + `"},"want":` + tc.want + `}]}]}`
		runFixtureScenario(t, raw)
	}
}
