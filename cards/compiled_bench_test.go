package cards

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var (
	benchmarkBool     bool
	benchmarkFace     *Face
	benchmarkRegistry *Registry
	benchmarkSA       *SA
	benchmarkSAs      []*SA
)

func benchmarkRegistryPath(b *testing.B) string {
	b.Helper()
	path := CachePath(filepath.Join("..", ".cards"))
	if _, err := os.Stat(path); err != nil {
		b.Skipf("stat corpus cache: %v", err)
	}
	return path
}

func BenchmarkLoadRegistry(b *testing.B) {
	path := benchmarkRegistryPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	var err error
	for range b.N {
		benchmarkRegistry, err = LoadRegistry(path)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	// Price one live registry independently of the benchmark loop. The second
	// collection settles finalizers and keeps the registry alive until after
	// both readings so the custom metrics describe retained, not transient,
	// memory.
	benchmarkRegistry = nil
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	live, err := LoadRegistry(path)
	if err != nil {
		b.Fatal(err)
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(live)
	benchmarkRegistry = live
	b.ReportMetric(float64(after.HeapAlloc), "heapalloc-B")
	b.ReportMetric(float64(after.HeapObjects), "heapobjects")
	if after.HeapAlloc >= before.HeapAlloc {
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc), "retained-B")
	}
	if after.HeapObjects >= before.HeapObjects {
		b.ReportMetric(float64(after.HeapObjects-before.HeapObjects), "retained-objects")
	}
}

func BenchmarkCompileMetadata(b *testing.B) {
	path := benchmarkRegistryPath(b)
	r, err := LoadRegistry(path)
	if err != nil {
		b.Fatal(err)
	}
	r.invalidateCatalog()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := r.CompileMetadata(); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkRegistry = r
}

func benchmarkBoundFace(b *testing.B, face *Face) *Face {
	b.Helper()
	r := NewRegistry()
	r.Add(&Card{Faces: []*Face{face}})
	if err := r.CompileMetadata(); err != nil {
		b.Fatal(err)
	}
	benchmarkRegistry = r
	return face
}

func BenchmarkFaceTypeQueries(b *testing.B) {
	f := benchmarkBoundFace(b, &Face{Types: []string{"Legendary", "Creature", "Human", "Wizard"}})
	benchmarkFace = f
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkBool = f.IsCreature() && !f.IsLand()
	}
}

func BenchmarkFaceKeywordQueries(b *testing.B) {
	f := benchmarkBoundFace(b, &Face{Keywords: []string{"Flying", "Kicker:2", "Trample"}})
	benchmarkFace = f
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkBool = f.HasKeyword("Kicker") && !f.HasKeyword("Madness")
	}
}

func BenchmarkFaceAbilityQueries(b *testing.B) {
	spell := &SA{Kind: "SP", API: "DealDamage"}
	manaW := &SA{Kind: "AB", API: "Mana"}
	manaU := &SA{Kind: "AB", API: "Mana"}
	f := benchmarkBoundFace(b, &Face{Abilities: []*SA{
		{Kind: "AB", API: "Draw"},
		spell,
		manaW,
		{Kind: "DB", API: "GainLife"},
		manaU,
	}})
	benchmarkFace = f
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkSA = f.SpellAbility()
		benchmarkSAs = f.ManaAbilities()
	}
	if benchmarkSA != spell || len(benchmarkSAs) != 2 || benchmarkSAs[0] != manaW || benchmarkSAs[1] != manaU {
		b.Fatal("ability query benchmark returned the wrong abilities")
	}
}
