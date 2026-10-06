package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestLevelBScenarioUsesXMageSpelling(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	reg, err := loadReg(filepath.Join(".cards"))
	if err != nil {
		t.Fatal(err)
	}
	const card = "Dáin Ironfoot"
	manifest := compliance.Manifest{Code: "TINY", Cards: []compliance.ManifestCard{{Name: card}}}
	out := filepath.Join(t.TempDir(), "b.jsonl")
	if err := genManifest(reg, manifest, out, "B"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 4<<20)
	found := false
	for s.Scan() {
		var it oraclegen.Item
		if err := json.Unmarshal(s.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		if it.Template == "cast-resolve" || it.Card != card {
			continue
		}
		found = true
		if it.XMageName != "Dain Ironfoot" {
			t.Fatalf("level-B %s card=%q XMageName = %q, want Dain Ironfoot", it.Template, it.Card, it.XMageName)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Dáin Ironfoot generated no level-B scenario")
	}
}
