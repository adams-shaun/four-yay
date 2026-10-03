package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchbench"
)

func TestManifestValidate(t *testing.T) {
	sha := strings.Repeat("a", 64)
	m := searchbench.Manifest{Kind: searchbench.ManifestKind, SchemaVersion: searchbench.ManifestSchemaVersion,
		Dataset:   searchbench.Dataset{Name: "test", License: "CC BY 4.0", URI: "https://example.invalid", SHA256: sha},
		Corpus:    searchbench.Corpus{ForgeRef: strings.Repeat("b", 40), CompilerFingerprint: "test"},
		Selection: searchbench.Selection{Dev: 0, Test: 0, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2},
	}
	if err := m.Seal(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"manifest", "validate", "-in", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "valid gorge-searchbench-manifest") || !strings.Contains(out.String(), m.Digest) {
		t.Fatalf("output %q", out.String())
	}
}

func TestUsageRejectsEverythingButManifestValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"manifest"}, {"manifest", "validate"}, {"run", "-in", "x"}, {"manifest", "validate", "-in", "x", "extra"}, {"analyze"}} {
		if err := run(args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("run(%q) = %v, want usage", args, err)
		}
	}
}

func TestSourceAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.csv")
	if err := os.WriteFile(path, []byte("expansion,event_type,user_game_win_rate_bucket,user_n_games_bucket\nFDN,PremierDraft,0.6,100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"source", "audit", "-in", path}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "eligible=1") || !strings.Contains(got, "fdn_premier=1") {
		t.Fatalf("audit output %q", got)
	}
}

func TestRunName(t *testing.T) {
	for _, c := range []struct {
		arm  searchbench.SearchArm
		sims int
		d    float64
		u    azmcts.DiscountUnit
		leaf string
		seed uint64
		opp  bool
		want string
	}{
		{searchbench.ArmNoSearch, 0, 1, azmcts.DiscountPly, "heuristic", 0, false, "no-search"},
		{searchbench.ArmPIMC4, 1000, 1, azmcts.DiscountPly, "heuristic", 0, false, "pimc-4-b1000"},
		{searchbench.ArmClairvoyant, 1000, 0.99, azmcts.DiscountPly, "heuristic", 0, false, "clairvoyant-mcts-b1000-d0.99"},
		{searchbench.ArmClairvoyant, 300, 0.99, azmcts.DiscountAction, "heuristic", 0, true, "clairvoyant-mcts-b300-d0.99-action-opp"},
		{searchbench.ArmISMCTS, 300, 0.9, azmcts.DiscountAction, "x.ckpt", 2, false, "is-mcts-b300-d0.9-action-net-s2"},
	} {
		if got := runName(c.arm, c.sims, c.d, c.u, c.leaf, c.seed, c.opp); got != c.want {
			t.Errorf("runName = %q, want %q", got, c.want)
		}
	}
	if searchbench.ItemSeed(0, "a") == searchbench.ItemSeed(0, "b") || searchbench.ItemSeed(0, "a") == searchbench.ItemSeed(1, "a") {
		t.Error("ItemSeed collides")
	}
}
