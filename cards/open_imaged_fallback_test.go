package cards

import (
	"os"
	"testing"
)

// TestOpenCorpusServesImagedRegistryWhenCacheSaveFails pins the read-only
// .cards path: when the recompiled registry cannot be written back (here,
// because the cache path is a directory so Save's final rename fails for any
// uid), openCorpus must still return the IMAGED registry, built in memory
// from the eager one, rather than the eager registry. The eager registry has
// a Catalog too, so the assertion is specifically the lazy/imaged property
// (lazy != nil), which only the imaged route sets.
func TestOpenCorpusServesImagedRegistryWhenCacheSaveFails(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")

	// A fresh fingerprint: no sibling cache can be read, so openCorpus must
	// compile and then attempt the write-back. Making the cache path a
	// directory makes Save fail regardless of uid (chmod is untrustworthy
	// under a root gate), reproducing the read-only-corpus fall-through.
	const fp = "0123456789abcdef0123456789abcdef"
	cache := cachePathFor(dir, fp)
	if err := os.Mkdir(cache, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := openCorpus(dir, fp)
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: the failure actually happened -- Save cannot have written
	// the cache through a directory, so this exercised the fall-through.
	if fi, statErr := os.Stat(cache); statErr != nil || !fi.IsDir() {
		t.Fatalf("precondition failed: cache path is not the blocking directory (err=%v)", statErr)
	}
	if r.lazy == nil {
		t.Fatal("openCorpus returned an eager registry on a cache-write failure: lazy == nil")
	}
	c, ok := r.Lookup("Mountain")
	if !ok {
		t.Fatal("imaged registry does not resolve Mountain")
	}
	if c == nil || len(c.Faces) == 0 || c.Faces[0].Name != "Mountain" {
		t.Fatalf("materialized card is wrong: %+v", c)
	}
	if r.MaterializedCount() == 0 {
		t.Fatal("Lookup did not materialize the card on the imaged registry")
	}
}
