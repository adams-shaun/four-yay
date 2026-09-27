package cards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCompilerFingerprintIsPopulatedAndHex pins the precondition every cache
// path depends on: the embedded source set was non-empty (so the fingerprint
// varies per build) and the value is 32 lowercase hex characters, the shape
// cachePathFor splices into a filename.
func TestCompilerFingerprintIsPopulatedAndHex(t *testing.T) {
	if len(CompilerFingerprint) != 32 {
		t.Fatalf("CompilerFingerprint = %q (len %d), want 32 hex chars", CompilerFingerprint, len(CompilerFingerprint))
	}
	for _, c := range CompilerFingerprint {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("CompilerFingerprint = %q has non-hex rune %q", CompilerFingerprint, c)
		}
	}
	if !fingerprintSourcesPresent() {
		t.Fatal("no compiler sources embedded; CompilerFingerprint would be identical across builds")
	}
}

// TestFingerprintCachesAreDistinctPerCompiler proves the core contract: two
// builds with different compiler fingerprints open the SAME corpus directory
// yet load and save DIFFERENT cache files, and neither reads the other's IR.
//
// The first fingerprint's cache is seeded from a registry holding "Island",
// which is absent from the folder. If the second fingerprint read that cache
// (the pre-fix behaviour, where both used ir.gob.gz) it would return Island;
// instead it must compile the folder and return Mountain.
func TestFingerprintCachesAreDistinctPerCompiler(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")

	const fpA, fpB = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// Seed fingerprint A's cache with a card that is NOT in the folder. This
	// is what a different parser's compiled IR looks like to B.
	seeded := NewRegistry()
	island, _ := ParseBytes("island.txt", []byte("Name:Island\nTypes:Basic Land Island\nOracle:\n"))
	seeded.Add(island)
	pathA := cachePathFor(dir, fpA)
	if err := seeded.Save(pathA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathA); err != nil {
		t.Fatalf("precondition: fingerprint A cache was not written: %v", err)
	}

	// Fingerprint B must not see A's Island; it compiles the folder afresh.
	gotB, err := openCorpus(dir, fpB)
	if err != nil {
		t.Fatalf("openCorpus(fpB): %v", err)
	}
	if _, ok := gotB.Lookup("Island"); ok {
		t.Fatal("fingerprint B read fingerprint A's cache (Island present)")
	}
	if _, ok := gotB.Lookup("Mountain"); !ok {
		t.Fatal("fingerprint B did not compile the folder (Mountain absent)")
	}
	pathB := cachePathFor(dir, fpB)
	if pathA == pathB {
		t.Fatalf("distinct fingerprints share one cache path: %s", pathA)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Fatalf("fingerprint B cache was not written separately: %v", err)
	}

	// Reopening A must still return its own cached Island, proving B's save did
	// not clobber A's file.
	gotA, err := openCorpus(dir, fpA)
	if err != nil {
		t.Fatalf("openCorpus(fpA): %v", err)
	}
	if _, ok := gotA.Lookup("Island"); !ok {
		t.Fatal("fingerprint A's own cache was lost or overwritten by B")
	}
	if _, ok := gotA.Lookup("Mountain"); ok {
		t.Fatal("fingerprint A recompiled the folder instead of using its own cache")
	}

	// The two files must differ on disk.
	aData, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	bData, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if string(aData) == string(bData) {
		t.Fatal("two fingerprints produced byte-identical cache files")
	}
}

// TestCachePruningKeepsNewestBoundsSiblingGrowth proves the housekeeping cap:
// once more than maxSiblingCaches fingerprint files exist, a save prunes the
// oldest down to the cap and never removes the cache it just wrote.
func TestCachePruningKeepsNewestBoundsSiblingGrowth(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")

	// Six fingerprints, oldest first, each a valid cache file.
	fps := []string{"0000", "1111", "2222", "3333", "4444", "5555"}
	for i, fp := range fps {
		r := NewRegistry()
		c, _ := ParseBytes(fp+".txt", []byte("Name:Card"+fp+"\nTypes:Sorcery\nOracle:\n"))
		r.Add(c)
		if err := r.Save(cachePathFor(dir, fp)); err != nil {
			t.Fatal(err)
		}
		// Ensure distinct, ordered mtimes so "oldest" is well defined.
		mt := time.Unix(int64(1_700_000_000+i), 0)
		if err := os.Chtimes(cachePathFor(dir, fp), mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	// A seventh save prunes back to the cap.
	const fpNew = "9999"
	if _, err := openCorpus(dir, fpNew); err != nil {
		t.Fatal(err)
	}
	newPath := cachePathFor(dir, fpNew)
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("newly written cache missing: %v", err)
	}
	left, err := filepath.Glob(filepath.Join(dir, "ir-*.gob.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > maxSiblingCaches {
		t.Fatalf("prune left %d sibling caches, want <= %d: %v", len(left), maxSiblingCaches, left)
	}
	// The just-written cache must survive; that is the only guarantee a
	// concurrent reader of it can rely on.
	found := false
	for _, p := range left {
		if p == newPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("prune removed the cache it was called to keep: %v", left)
	}
}

// TestCachedPathsFollowTheOpenedDirectory pins the removed-worktree case: a
// cache compiled through one corpus path and loaded through another (the
// shared .cards opened via a different worktree's symlink, or the compiling
// worktree since removed) must name script files under the directory this
// process opened, or host's feedback capture cannot read a token back.
func TestCachedPathsFollowTheOpenedDirectory(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	writeScript(t, first, "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")
	tokens := filepath.Join(first, "tokenscripts")
	if err := os.MkdirAll(tokens, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tokens, "c_1_1_thopter.txt"),
		[]byte("Name:Thopter Token\nTypes:Artifact Creature Thopter\nPT:1/1\nOracle:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openCorpus(first, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(base, "second")
	if err := os.Rename(first, second); err != nil {
		t.Fatal(err)
	}
	r, err := openCorpus(second, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tokens) == 0 {
		t.Fatal("no tokens compiled; the fixture token script did not parse")
	}
	check := func(what, p string) {
		t.Helper()
		if !strings.HasPrefix(p, second+string(filepath.Separator)) {
			t.Errorf("%s path %q does not follow the opened directory %q", what, p, second)
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s path unreadable: %v", what, err)
		}
	}
	for _, c := range r.Cards {
		check("card", c.Path)
	}
	for stem, c := range r.Tokens {
		check("token "+stem, c.Path)
	}
}
