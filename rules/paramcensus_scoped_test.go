package rules

import "testing"

func TestParamCensusScopesPumpZoneToEffectAPI(t *testing.T) {
	t.Parallel()
	if ignoredParamKeys["PumpZone"] != "" {
		t.Fatal("PumpZone must not be key-global: api:Pump and api:PumpAll consume it")
	}
	if !ignoredParam("api:Effect", "PumpZone") {
		t.Fatal("PumpZone on api:Effect should be classified inert")
	}
	for _, prim := range []string{
		"api:Pump", "api:PumpAll", "stat:Continuous", "trig:Phase", "repl:Moved", "api:Other",
	} {
		if ignoredParam(prim, "PumpZone") {
			t.Errorf("PumpZone unexpectedly ignored for sibling primitive %q", prim)
		}
	}
}
