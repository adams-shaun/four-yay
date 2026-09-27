package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchSeedIsAPureFunctionOfTableSeedAndIndex(t *testing.T) {
	t.Parallel()
	if MatchSeed(1, 1) != MatchSeed(1, 1) {
		t.Fatal("not deterministic")
	}
	seen := map[uint64]bool{}
	for k := 1; k <= 1000; k++ {
		s := MatchSeed(42, k)
		if seen[s] {
			t.Fatalf("seed collision at k=%d", k)
		}
		seen[s] = true
	}
	if MatchSeed(1, 1) == MatchSeed(2, 1) || MatchSeed(1, 1) == MatchSeed(1, 2) {
		t.Fatal("seed does not depend on both inputs")
	}
	// Pinned so a table's history cannot silently change under a refactor.
	if got := MatchSeed(0, 1); got != 0xe220a8397b1dcdaf {
		t.Fatalf("MatchSeed(0,1) = %#x; if the formula changed on purpose, update this pin and the sidecar goldens", got)
	}
}

func TestNextGameIDReservesDroppedOnDemandLogDirectories(t *testing.T) {
	dir := t.TempDir()
	// g2 is not registered: it models an on-demand table dropped on restart.
	// Its persisted match log must never be overwritten by a new g2/match 1.
	if err := os.Mkdir(filepath.Join(dir, "g2"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{Dir: dir, LoadDeck: testOptions(t).LoadDeck})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if got := NextGameID(r); got != "g3" {
		t.Fatalf("next id %q, want g3 after retained g2 log", got)
	}
}

func TestSeededShuffleIsAPureFunctionOfTheSeed(t *testing.T) {
	t.Parallel()
	pool := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	// Same seed, same order (and the input slice is not modified).
	once := SeededShuffle(42, pool)
	for i := 0; i < 5; i++ {
		got := SeededShuffle(42, pool)
		if len(got) != len(pool) {
			t.Fatalf("length %d, want %d", len(got), len(pool))
		}
		for j := range got {
			if got[j] != once[j] {
				t.Fatalf("same seed gave a different order at %d: %v vs %v", j, got, once)
			}
		}
	}
	for i := range pool {
		if pool[i] != []string{"a", "b", "c", "d", "e", "f", "g", "h"}[i] {
			t.Fatalf("input slice was modified: %v", pool)
		}
	}
	// It is a permutation (no duplicates, same membership).
	seen := map[string]bool{}
	for _, s := range once {
		if seen[s] {
			t.Fatalf("duplicate %q in %v", s, once)
		}
		seen[s] = true
	}
	if len(seen) != len(pool) {
		t.Fatalf("permutation missing an element: %v", once)
	}
	// Different seeds overwhelmingly give different orders (the tiny
	// chance of a collision across 8 elements is far below flake level,
	// but pin the determinism anyway rather than assert inequality).
	if SeededShuffle(1, pool)[0] == SeededShuffle(2, pool)[0] && SeededShuffle(1, pool)[0] == SeededShuffle(3, pool)[0] {
		t.Log("note: the leading element coincides across three seeds (not a failure, just observed)")
	}
}

func TestNextGameIDSkipsExistingGameTables(t *testing.T) {
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if got := NextGameID(r); got != "g1" {
		t.Fatalf("empty registry next id %q, want g1", got)
	}
	// Add t1 plus g1 and g2; the id must skip past g2, ignoring t1.
	for _, id := range []TableID{"t1", "g1", "g2"} {
		cfg := fourSeatTable(id, false)
		if err := r.AddTable(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if got := NextGameID(r); got != "g3" {
		t.Fatalf("next id %q, want g3", got)
	}
}
