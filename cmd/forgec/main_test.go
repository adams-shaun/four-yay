package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// reportScript writes one synthetic card script into dir/cardsfolder. The
// text is authored here; it is not a Forge file.
func reportScript(t *testing.T, dir, name, body string) {
	t.Helper()
	folder := cards.CorpusDir(dir)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReportRegistryFallsBackToFreshCompileWithoutCache(t *testing.T) {
	dir := t.TempDir()
	reportScript(t, dir, "plains.txt", "Name:Plains\nTypes:Basic Land Plains\nOracle:\n")
	r, err := loadReportRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Lookup("Plains"); !ok {
		t.Fatal("fallback compile lacks the folder's card")
	}
}

func TestLoadReportRegistryPrefersAFreshCache(t *testing.T) {
	dir := t.TempDir()
	reportScript(t, dir, "plains.txt", "Name:Plains\nTypes:Basic Land Plains\nOracle:\n")
	// A cache holding a different card: fresh cache wins over the folder,
	// proving the helper returned the cache without recompiling.
	r := cards.NewRegistry()
	c, _ := cards.ParseBytes("swamp.txt", []byte("Name:Swamp\nTypes:Basic Land Swamp\nOracle:\n"))
	r.Add(c)
	cache := cards.CachePath(dir)
	if err := r.Save(cache); err != nil {
		t.Fatal(err)
	}
	got, err := loadReportRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Lookup("Swamp"); !ok {
		t.Fatal("fresh cache was not used")
	}
	if _, ok := got.Lookup("Plains"); ok {
		t.Fatal("helper recompiled from the folder instead of using the cache")
	}
}
