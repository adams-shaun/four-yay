package cards

import (
	"testing"
	"unsafe"
)

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
	paramKeyCount - 1, paramKeyCount / 2, paramKeyCount / 3, ParamKey(int(paramKeyCount) * 2 / 3),
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

// TestParamKeyRoomRemains: the vocabulary keeps at least a mask word of
// headroom under its budget, so W4's typed-parameter work can add keys
// without first re-planning the representation.
func TestParamKeyRoomRemains(t *testing.T) {
	if unsafe.Sizeof(ParamKey(0)) != 2 {
		t.Fatalf("ParamKey is %d bytes, want uint16", unsafe.Sizeof(ParamKey(0)))
	}
	if int(paramKeyCount) >= paramKeyCap-64 {
		t.Fatalf("vocabulary %d keys is within 64 of the %d-key budget", paramKeyCount, paramKeyCap)
	}
	if paramMaskWords*64 < int(paramKeyCount) {
		t.Fatalf("%d mask words cannot hold %d keys", paramMaskWords, paramKeyCount)
	}
	t.Logf("%d of %d keys used, %d mask words", paramKeyCount-1, paramKeyCap, paramMaskWords)
}

// TestParamSetSizeTracksVocabulary pins a compiled node's ParamSet to its
// header (src, n, vals) plus one mask word and one uint16 rank per 64 declared
// keys: the per-node price of the vocabulary, paid on ~85k corpus nodes.
func TestParamSetSizeTracksVocabulary(t *testing.T) {
	want := 8 + 8 + 24 + 8*paramMaskWords + (2*paramMaskWords+7)/8*8
	if got := int(unsafe.Sizeof(ParamSet{})); got != want {
		t.Fatalf("sizeof(ParamSet) = %d, want %d for %d mask words", got, want, paramMaskWords)
	}
}

// TestParamSetOutOfVocabularyKeyIsAbsent: a ParamKey past the declared
// vocabulary reads absent through a bound set, never a panic or a neighbour's
// value.
func TestParamSetOutOfVocabularyKeyIsAbsent(t *testing.T) {
	m := map[string]string{"Cost": "1", "Zone": "Hand"}
	sa := &SA{Params: m}
	sa.ps = newParamSet(m)
	for _, k := range []ParamKey{paramKeyCount, ParamKey(paramMaskWords * 64), ParamKey(paramMaskWords*64 + int(PKCost)), 0xffff} {
		if v, ok := sa.ps.get(k); ok || v != "" {
			t.Fatalf("key %d past the vocabulary read (%q,%v)", k, v, ok)
		}
	}
	if sa.ParamStr(PKCost) != "1" || sa.ParamStr(PKZone) != "Hand" {
		t.Fatal("in-vocabulary reads broken")
	}
}

// TestParamReadsDoNotAllocate: the compiled reads are allocation-free.
func TestParamReadsDoNotAllocate(t *testing.T) {
	m := map[string]string{}
	for i, k := range benchParamKeys {
		if i%2 == 0 {
			m[k.String()] = k.String()
		}
	}
	sa := &SA{Params: m}
	sa.ps = newParamSet(m)
	mask := ParamMaskOf(PKCost, PKZone)
	n := 0
	if a := testing.AllocsPerRun(100, func() {
		for _, k := range benchParamKeys {
			v, ok := sa.Param(k)
			if ok && sa.HasParam(k) && sa.MayHaveAnyParam(mask) {
				n += len(v) + len(sa.ParamStr(k))
			}
		}
	}); a != 0 {
		t.Fatalf("compiled reads allocate %v per run", a)
	}
	if n == 0 {
		t.Fatal("no reads answered")
	}
}

// BenchmarkParamLookup reads ParamStr/HasParam/Param over a deterministic
// population of bound sets: "hot" re-reads one set (the L1 case), "sweep"
// walks 4096 sets of 1-10 keys drawn across the whole vocabulary (the cache
// pressure of a rules pass over a board's abilities). The metric is ns per
// single read.
func BenchmarkParamLookup(b *testing.B) {
	const nsets = 4096
	var sas []*SA
	x := uint32(2463534242)
	next := func() uint32 { x ^= x << 13; x ^= x >> 17; x ^= x << 5; return x }
	for i := 0; i < nsets; i++ {
		m := map[string]string{}
		for j, n := 0, 1+int(next()%10); j < n; j++ {
			k := ParamKey(1 + next()%uint32(paramKeyCount-1))
			m[k.String()] = k.String()
		}
		sa := &SA{Params: m}
		sa.ps = newParamSet(m)
		sas = append(sas, sa)
	}
	reads := benchParamKeys[:]
	for _, c := range []struct {
		name string
		sets []*SA
	}{{"hot", sas[:1]}, {"sweep", sas}} {
		for _, op := range []struct {
			name string
			f    func(*SA, ParamKey) int
		}{
			{"ParamStr", func(sa *SA, k ParamKey) int { return len(sa.ParamStr(k)) }},
			{"HasParam", func(sa *SA, k ParamKey) int {
				if sa.HasParam(k) {
					return 1
				}
				return 0
			}},
			{"Param", func(sa *SA, k ParamKey) int {
				v, ok := sa.Param(k)
				if ok {
					return len(v) + 1
				}
				return 0
			}},
		} {
			b.Run(c.name+"/"+op.name, func(b *testing.B) {
				b.ReportAllocs()
				n := 0
				per := len(c.sets) * len(reads)
				iters := b.N/per + 1
				b.ResetTimer()
				for i := 0; i < iters; i++ {
					for _, sa := range c.sets {
						for _, k := range reads {
							n += op.f(sa, k)
						}
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(iters*per), "ns/read")
				if n < 0 {
					b.Fatal()
				}
			})
		}
	}
}
