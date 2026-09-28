package cards

import "testing"

// TestParamSetsMatchMaps pins every corpus static's and trigger's compiled
// ParamSet to its Params map for every vocabulary key.
func TestParamSetsMatchMaps(t *testing.T) {
	reg := compiledCorpus(t)
	check := func(where string, get func(ParamKey) (string, bool), m map[string]string) {
		for k := ParamKey(1); k < paramKeyCount; k++ {
			v, ok := get(k)
			wv, wok := m[paramKeyNames[k]]
			if v != wv || ok != wok {
				t.Fatalf("%s: %s = (%q,%v), map (%q,%v)", where, k, v, ok, wv, wok)
			}
		}
	}
	bound := 0
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.ps.bound(st.Params) {
					bound++
				}
				check(f.Name+" static", st.Param, st.Params)
			}
			for _, tr := range f.Triggers {
				if tr.ps.bound(tr.Params) {
					bound++
				}
				check(f.Name+" trigger", tr.Param, tr.Params)
				for sa := tr.Effect; sa != nil; sa = sa.Sub {
					check(f.Name+" trigger body", sa.Param, sa.Params)
				}
			}
			for _, rp := range f.Repls {
				check(f.Name+" replacement", rp.Param, rp.Params)
			}
			for _, a := range f.Abilities {
				for sa, d := a, 0; sa != nil && d < 32; sa, d = sa.Sub, d+1 {
					if sa.ps.bound(sa.Params) {
						bound++
					}
					check(f.Name+" ability", sa.Param, sa.Params)
				}
			}
		}
	}
	if bound == 0 {
		t.Fatal("no corpus static or trigger has a bound ParamSet")
	}
}

// TestParamSetFallsBackOnReplacedMap: a node copy whose Params was replaced
// or resized reads the map, never the stale compiled set.
func TestParamSetFallsBackOnReplacedMap(t *testing.T) {
	st := Static{Mode: "Continuous", Params: map[string]string{"Affected": "Creature", "AddPower": "1"}}
	st.ps = newParamSet(st.Params)
	if v := st.ParamStr(PKAddPower); v != "1" || st.HasParam(PKAddToughness) || st.MayHaveAnyParam(ParamMaskOf(PKSetPower)) {
		t.Fatal("bound set answers wrong")
	}
	cp := st
	cp.Params = map[string]string{"Affected": "Creature", "AddToughness": "2"}
	if cp.HasParam(PKAddPower) || cp.ParamStr(PKAddToughness) != "2" {
		t.Fatal("replaced map answered from the stale set")
	}
	delete(st.Params, "AddPower")
	if st.HasParam(PKAddPower) {
		t.Fatal("resized map answered from the stale set")
	}
}
