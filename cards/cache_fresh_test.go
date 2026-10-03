package cards

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cardBody returns a one-line synthetic card script for name. The text is
// authored here; it is not a Forge file.
func cardBody(name, types string) string {
	return "Name:" + name + "\nTypes:" + types + "\nOracle:\n"
}

// seedCacheWith saves a cache holding exactly one card, cardName, at dir's
// cache path. It returns the cache path.
func seedCacheWith(t *testing.T, dir, cardName, types string) string {
	t.Helper()
	r := NewRegistry()
	c, err := ParseBytes(cardName+".txt", []byte(cardBody(cardName, types)))
	if err != nil {
		t.Fatal(err)
	}
	r.Add(c)
	cache := CachePath(dir)
	if err := r.Save(cache); err != nil {
		t.Fatal(err)
	}
	return cache
}

// writeOldLock writes dir/cards.lock and backdates it well before now, the
// steady state fetchRepo leaves behind once a fetch has completed.
func writeOldLock(t *testing.T, dir string) string {
	t.Helper()
	lock := lockPath(dir)
	if err := os.WriteFile(lock, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lock, past, past); err != nil {
		t.Fatal(err)
	}
	return lock
}

// TestCacheIsStaleWhenTheCorpusFolderIsNewer is the regression for the
// reported silent-old-corpus read: fetchRepo renames a freshly fetched
// cardsfolder into place BEFORE it rewrites cards.lock, so for that window the
// old lock is older than the cache while the new folder is newer than it. A
// rule that compares only the lock served the old corpus.
func TestCacheIsStaleWhenTheCorpusFolderIsNewer(t *testing.T) {
	dir := t.TempDir()
	// Steady state: a Mountain corpus on disk, an Island cache newer than it
	// and an old lock. A fresh cache wins.
	writeScript(t, dir, "mountain.txt", cardBody("Mountain", "Basic Land Mountain"))
	cache := seedCacheWith(t, dir, "Island", "Basic Land Island")
	lock := writeOldLock(t, dir)

	// Precondition 1: the cache really holds the OLD card. If it did not, the
	// absence of that card below would prove nothing.
	old, err := LoadRegistry(cache)
	if err != nil {
		t.Fatalf("precondition: cached registry unreadable: %v", err)
	}
	if _, ok := old.Lookup("Island"); !ok {
		t.Fatal("precondition: cache does not contain Island")
	}
	// Precondition 2: the lock really is older than the cache (fetchRepo's
	// window). If the lock were newer the old rule would already recompile.
	ci, _ := os.Stat(cache)
	li, _ := os.Stat(lock)
	if li.ModTime().After(ci.ModTime()) {
		t.Fatal("precondition: lock is not older than the cache")
	}
	if !cacheFresh(dir, cache) {
		t.Fatal("precondition: steady-state cache is not fresh")
	}

	// The fetchRepo window: the old folder is removed, a new one renamed in,
	// and it is newer than the cache; the lock is left old.
	if err := os.RemoveAll(CorpusDir(dir)); err != nil {
		t.Fatal(err)
	}
	writeScript(t, dir, "forest.txt", cardBody("Forest", "Basic Land Forest"))
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(CorpusDir(dir), future, future); err != nil {
		t.Fatal(err)
	}

	if cacheFresh(dir, cache) {
		t.Fatal("cache is fresh though the corpus folder is newer than it")
	}
	got, err := OpenCorpus(dir)
	if err != nil {
		t.Fatalf("OpenCorpus during mid-fetch window: %v", err)
	}
	if _, ok := got.Lookup("Forest"); !ok {
		t.Fatal("mid-fetch OpenCorpus did not return the NEW corpus card")
	}
	if _, ok := got.Lookup("Island"); ok {
		t.Fatal("mid-fetch OpenCorpus served the OLD cached card")
	}
}

// TestCacheIsStaleWhenTheTokensFolderIsNewer pins the token-script folder as a
// second corpus input for the staleness rule: a fetch that lands only a newer
// tokenscripts (or one whose folder mtime moves) must invalidate the cache
// too, not just cardsfolder.
func TestCacheIsStaleWhenTheTokensFolderIsNewer(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mountain.txt", cardBody("Mountain", "Basic Land Mountain"))
	cache := seedCacheWith(t, dir, "Island", "Basic Land Island")
	writeOldLock(t, dir)
	if !cacheFresh(dir, cache) {
		t.Fatal("precondition: steady-state cache is not fresh")
	}

	// A token-script folder newer than the cache. The folder's presence is
	// what feeds the rule (its card content does not change the card cache),
	// so assert the helper directly alongside the reader.
	if err := os.MkdirAll(TokensDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(TokensDir(dir), "r_1_1_goblin.txt")
	if err := os.WriteFile(tok, []byte(cardBody("Goblin Token", "Token Creature Goblin")), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(TokensDir(dir), future, future); err != nil {
		t.Fatal(err)
	}
	// Precondition: the folder really is newer than the cache.
	ci, _ := os.Stat(cache)
	ti, _ := os.Stat(TokensDir(dir))
	if !ti.ModTime().After(ci.ModTime()) {
		t.Fatal("precondition: tokens folder is not newer than the cache")
	}

	if cacheFresh(dir, cache) {
		t.Fatal("cache is fresh though the tokens folder is newer than it")
	}
	// The reader must recompile from the folder (Mountain), not serve the
	// older cached card (Island).
	got, err := OpenCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Lookup("Mountain"); !ok {
		t.Fatal("token-stale OpenCorpus did not recompile from the corpus folder")
	}
	if _, ok := got.Lookup("Island"); ok {
		t.Fatal("token-stale OpenCorpus served the OLD cached card")
	}
}

// TestCorpusAbsentWhileLockIsOldIsNotServedFromCache covers the
// RemoveAll-before-Rename half of fetchRepo's window: with cardsfolder gone
// the cache cannot be validated, so the reader must fail loudly rather than
// return a coherent registry of the previous corpus.
func TestCorpusAbsentWhileLockIsOldIsNotServedFromCache(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mountain.txt", cardBody("Mountain", "Basic Land Mountain"))
	cache := seedCacheWith(t, dir, "Island", "Basic Land Island")
	writeOldLock(t, dir)
	if !cacheFresh(dir, cache) {
		t.Fatal("precondition: steady-state cache is not fresh")
	}

	if err := os.RemoveAll(CorpusDir(dir)); err != nil {
		t.Fatal(err)
	}
	if cacheFresh(dir, cache) {
		t.Fatal("cache is fresh while the corpus folder is absent")
	}
	got, err := OpenCorpus(dir)
	if err == nil {
		if _, ok := got.Lookup("Island"); ok {
			t.Fatal("OpenCorpus served the OLD cached card with no corpus on disk")
		}
		t.Fatal("OpenCorpus returned a registry with no corpus on disk")
	}
}

// TestSubsetSegmentFollowsTheCorpusFolderRule pins the segment path
// (segFresh/refreshSegments) to the same rule: a segment newer than the cache
// but older than the corpus folder must be rebuilt, so OpenCorpusSubset cannot
// go stale the way OpenCorpus could.
func TestSubsetSegmentFollowsTheCorpusFolderRule(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mountain.txt", cardBody("Mountain", "Basic Land Mountain"))
	writeScript(t, dir, "island.txt", cardBody("Island", "Basic Land Island"))
	cache := seedCacheWith(t, dir, "Mountain", "Basic Land Mountain")
	writeOldLock(t, dir)

	// Build the segment in the steady state, then move the corpus folder
	// newer than both cache and segment (fetchRepo's rename).
	if _, err := OpenCorpusSubset(dir, []string{"Mountain"}); err != nil {
		t.Fatal(err)
	}
	seg := SegmentPath(cache)
	if !segFresh(dir, cache, seg) {
		t.Fatal("precondition: freshly built segment is not fresh")
	}
	if err := os.RemoveAll(CorpusDir(dir)); err != nil {
		t.Fatal(err)
	}
	writeScript(t, dir, "forest.txt", cardBody("Forest", "Basic Land Forest"))
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(CorpusDir(dir), future, future); err != nil {
		t.Fatal(err)
	}
	if segFresh(dir, cache, seg) {
		t.Fatal("segment is fresh though the corpus folder is newer than it")
	}
	got, err := OpenCorpusSubset(dir, []string{"Forest"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Lookup("Forest"); !ok {
		t.Fatal("subset open did not see the new corpus card")
	}
}
