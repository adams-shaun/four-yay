package searchbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManifest() Manifest {
	sha := strings.Repeat("a", 64)
	ref := strings.Repeat("b", 40)
	seeds := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	m := Manifest{
		Kind: ManifestKind, SchemaVersion: ManifestSchemaVersion,
		Dataset:   Dataset{Name: "17lands FDN Premier Draft", License: "CC BY 4.0", URI: "https://example.invalid/fdn.csv.gz", SHA256: sha},
		Corpus:    Corpus{ForgeRef: ref, CompilerFingerprint: "unit-test"},
		Selection: Selection{Dev: 1, Test: 1, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2},
		Items: []Item{
			{ID: "dev-0001", GameID: "g-dev", DraftID: "d-dev", Split: SplitDev, Type: DecisionHold, Seat: 0, Turn: 3, Sequence: 1, Fidelity: "T0", PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha, Label: Label{Alternatives: [][]int{{1}}}, WorldSeeds: append([]uint64(nil), seeds...)},
			{ID: "test-0001", GameID: "g-test", DraftID: "d-test", Split: SplitTest, Type: DecisionSpell, Seat: 1, Turn: 4, Sequence: 2, Fidelity: "T1", PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha, Label: Label{Alternatives: [][]int{{1}, {3}}, Act: true}, WorldSeeds: append([]uint64(nil), seeds...)},
		},
	}
	if err := m.Seal(); err != nil {
		panic(err)
	}
	return m
}

func TestManifestSealAndValidate(t *testing.T) {
	m := testManifest()
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	first := m.Digest
	if err := m.Seal(); err != nil || m.Digest != first {
		t.Fatalf("reseal: digest=%q err=%v, want %q", m.Digest, err, first)
	}
	m.Items[1].Label.Act = false
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered manifest error = %v", err)
	}
}

func TestManifestRejectsSplitLeakAndNonCanonicalLabels(t *testing.T) {
	for name, change := range map[string]func(*Manifest){
		"draft split":       func(m *Manifest) { m.Items[1].DraftID = m.Items[0].DraftID },
		"unsorted choices":  func(m *Manifest) { m.Items[1].Label.Alternatives = [][]int{{2, 1}} },
		"too many per game": func(m *Manifest) { m.Items[1].GameID = m.Items[0].GameID; m.Selection.MaximumItemsPerGame = 1 },
		"duplicate world":   func(m *Manifest) { m.Items[1].WorldSeeds[7] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			m := testManifest()
			change(&m)
			m.Digest = ""
			if err := m.Seal(); err == nil {
				t.Fatal("Seal accepted invalid manifest")
			}
		})
	}
}

func TestReadRejectsUnknownAndTrailingJSON(t *testing.T) {
	m := testManifest()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"unknown":  string(b[:len(b)-1]) + `,"unexpected":true}`,
		"trailing": string(b) + " {}",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(path); err == nil {
				t.Fatal("Read accepted malformed manifest")
			}
		})
	}
}

func TestAnalyzeRequiresCompleteOneArmResults(t *testing.T) {
	m := testManifest()
	rows := []Result{
		{ManifestDigest: m.Digest, Arm: "pimc-1", ItemID: "dev-0001", AgentChoices: []int{1}},
		{ManifestDigest: m.Digest, Arm: "pimc-1", ItemID: "test-0001", AgentChoices: []int{3}, AgentAct: true},
	}
	s, arm, err := Analyze(m, rows)
	if err != nil || arm != "pimc-1" || s.Matches != [4]int{1, 1, 0, 0} {
		t.Fatalf("Analyze = %+v, %q, %v", s, arm, err)
	}
	for _, bad := range [][]Result{rows[:1], {rows[0], rows[0]}, {rows[0], {ManifestDigest: m.Digest, Arm: "other", ItemID: "test-0001", AgentChoices: []int{3}}}} {
		if _, _, err := Analyze(m, bad); err == nil {
			t.Fatal("Analyze accepted incomplete, duplicate, or mixed-arm output")
		}
	}
}
