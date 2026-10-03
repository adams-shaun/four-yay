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

// benchParamKeys mixes present and absent keys from every mask word, so the
// rank lookup is exercised across words and on both branches.
var benchParamKeys = [...]ParamKey{
	PKActivation, PKAffected, PKCost, PKDefined, PKOrigin, PKSubAbility,
	PKValidCard, PKValidTgts, PKAmount, PKDuration, PKTargetMax, PKType,
	paramKeyCount - 1, paramKeyCount / 2, paramKeyCount / 3, paramKeyCount * 2 / 3,
}

// BenchmarkParamStr reads a bound ParamSet through ParamStr: the compiled
// hot-path read (mask test plus popcount rank, no map probe).
func BenchmarkParamStr(b *testing.B) {
	m := map[string]string{}
	for i, k := range benchParamKeys {
		if i%2 == 0 {
			m[k.String()] = k.String()
		}
	}
	sa := &SA{Params: m}
	sa.ps = newParamSet(m)
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for i := 0; i < b.N; i++ {
		for _, k := range benchParamKeys {
			n += len(sa.ParamStr(k))
		}
	}
	if n == 0 {
		b.Fatal("no reads answered")
	}
}

// TestParamSetRanksAcrossWords pins the per-word rank prefix: for subsets
// that leave every mask word empty, full or sparse in turn, each vocabulary
// key reads back exactly its map value and every other key reads absent.
func TestParamSetRanksAcrossWords(t *testing.T) {
	for _, keep := range []func(ParamKey) bool{
		func(ParamKey) bool { return true },
		func(k ParamKey) bool { return k%2 == 1 },
		func(k ParamKey) bool { return k%7 == 3 },
		func(k ParamKey) bool { return k>>6 != 0 },
		func(k ParamKey) bool { return k>>6 == 1 || k == paramKeyCount-1 },
		func(k ParamKey) bool { return k == 1 || k == paramKeyCount-1 },
	} {
		m := map[string]string{"NotAVocabularyKey": "x"}
		for k := ParamKey(1); k < paramKeyCount; k++ {
			if keep(k) {
				m[k.String()] = "v" + k.String()
			}
		}
		ps := newParamSet(m)
		var all ParamMask
		for k := ParamKey(1); k < paramKeyCount; k++ {
			all[k>>6] |= 1 << (k & 63)
			v, ok := ps.get(k)
			if want := keep(k); ok != want || (want && v != "v"+k.String()) || (!want && v != "") {
				t.Fatalf("%s: got (%q,%v), want present=%v", k, v, ok, want)
			}
		}
		if got := paramMayHaveAny(ps, m, all); got != (len(m) > 1) {
			t.Fatalf("MayHaveAny(all) = %v with %d keys", got, len(m)-1)
		}
	}
}
