package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchbench"
)

func cmdManifest(t *testing.T) (searchbench.Manifest, string) {
	t.Helper()
	sha := strings.Repeat("a", 64)
	item := func(id string, row int, typ searchbench.DecisionType, opts []string, alt, strict [][]int, act bool, focus string) searchbench.Item {
		return searchbench.Item{ID: id, GameID: id, DraftID: id, Row: row, Split: searchbench.SplitTest, Type: typ, Turn: 3, Sequence: 1, Tier: "T0",
			Options: opts, Focus: focus, PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha,
			Label: searchbench.Label{Alternatives: alt, Strict: strict, Act: act}, WorldSeeds: []uint64{1, 2, 3, 4, 5, 6, 7, 8}}
	}
	m := searchbench.Manifest{Kind: searchbench.ManifestKind, SchemaVersion: searchbench.ManifestSchemaVersion,
		Dataset:   searchbench.Dataset{Name: "t", License: "CC BY 4.0", URI: "https://example.invalid", SHA256: sha},
		Corpus:    searchbench.Corpus{ForgeRef: strings.Repeat("b", 40), CompilerFingerprint: "t"},
		Selection: searchbench.Selection{Test: 6, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2},
		Items: []searchbench.Item{
			item("a1", 1, searchbench.DecisionSpell, []string{"Pass", "Cast A"}, [][]int{{0}, {1}}, [][]int{{1}}, true, ""),
			item("a2", 2, searchbench.DecisionHold, []string{"Pass", "Cast A"}, [][]int{{0}}, [][]int{{0}}, false, ""),
			item("a3", 3, searchbench.DecisionAttack, []string{"no", "yes"}, [][]int{{1}}, [][]int{{1}}, true, "Bear"),
			item("a4", 4, searchbench.DecisionAttack, []string{"no", "yes"}, [][]int{{0}}, [][]int{{0}}, false, "Bear"),
			item("a5", 5, searchbench.DecisionBlock, []string{"no", "Ogre"}, [][]int{{1}}, [][]int{{1}}, true, "Wall"),
			item("a6", 6, searchbench.DecisionBlock, []string{"no", "Ogre"}, [][]int{{0}}, [][]int{{0}}, false, "Wall"),
		}}
	if err := m.Seal(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	p := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return m, p
}

func TestBaselinesAnalyzeCompare(t *testing.T) {
	_, mp := cmdManifest(t)
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"baselines", "-manifest", mp, "-out", dir}, &out); err != nil {
		t.Fatal(err)
	}
	passive := filepath.Join(dir, "baseline-passive.jsonl")
	active := filepath.Join(dir, "baseline-active.jsonl")
	random := filepath.Join(dir, "baseline-random.jsonl")
	js := filepath.Join(dir, "report.json")
	out.Reset()
	if err := run([]string{"analyze", "-manifest", mp, "-results", passive, active, random, "-json", js, "-boot", "200"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| baseline-passive | 6 | 75.0%", "| baseline-active | 6 | 50.0%", "0.500 [0.500, 0.500]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("analyze output lacks %q:\n%s", want, out.String())
		}
	}
	var rep searchbench.Report
	b, err := os.ReadFile(js)
	if err != nil || json.Unmarshal(b, &rep) != nil || len(rep.Runs) != 3 || rep.Resamples != 200 {
		t.Fatalf("report json %v %+v", err, rep)
	}
	out.Reset()
	if err := run([]string{"compare", "-manifest", mp, "-a", passive, "-b", active}, &out); err != nil {
		t.Fatal(err)
	}
	// passive − active: A_set 0.75 − 0.5 = +25 points, balanced 0.
	if !strings.Contains(out.String(), "+25.0") || !strings.Contains(out.String(), "| balanced | +0.0 (+0.0, +0.0) |") || !strings.Contains(out.String(), "| same_choice | 0.0%") {
		t.Fatalf("compare output:\n%s", out.String())
	}
	for _, args := range [][]string{{"analyze", "-manifest", mp}, {"compare", "-manifest", mp, "-a", passive}, {"baselines", "-manifest", mp}} {
		if err := run(args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("run(%q) = %v, want usage", args, err)
		}
	}
}
