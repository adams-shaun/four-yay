package searchbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
			{ID: "dev-0001", GameID: "g-dev", DraftID: "d-dev", Row: 17, Split: SplitDev, Type: DecisionHold, Seat: 0, Turn: 3, Sequence: 1, Tier: "T0", Options: []string{"Pass", "Cast Llanowar Elves"}, PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha, Label: Label{Alternatives: [][]int{{0}}, Strict: [][]int{{0}}}, WorldSeeds: append([]uint64(nil), seeds...)},
			{ID: "test-0001", GameID: "g-test", DraftID: "d-test", Row: 53, Split: SplitTest, Type: DecisionSpell, Seat: 1, Turn: 4, Sequence: 2, Tier: "T1", OnPlay: true, Options: []string{"Pass", "Cast A", "Cast B", "Cast C"}, PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha, Label: Label{Alternatives: [][]int{{0}, {1}, {3}}, Strict: [][]int{{1}, {3}}, Act: true}, WorldSeeds: append([]uint64(nil), seeds...)},
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
	m.Items[1].Turn++
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered manifest error = %v", err)
	}
}

func TestManifestRejectsSplitLeakAndNonCanonicalLabels(t *testing.T) {
	for name, change := range map[string]func(*Manifest){
		"draft split":           func(m *Manifest) { m.Items[1].DraftID = m.Items[0].DraftID },
		"unsorted alternatives": func(m *Manifest) { m.Items[1].Label.Alternatives = [][]int{{3}, {1}} },
		"multi-index alternative": func(m *Manifest) {
			m.Items[1].Label.Alternatives = [][]int{{1, 3}}
			m.Items[1].Label.Strict = [][]int{{1, 3}}
		},
		"alternative out of range": func(m *Manifest) {
			m.Items[1].Label.Alternatives = [][]int{{1}, {4}}
			m.Items[1].Label.Strict = [][]int{{1}, {4}}
		},
		"spell strict keeps pass": func(m *Manifest) { m.Items[1].Label.Strict = [][]int{{0}, {1}, {3}} },
		"spell strict not subset": func(m *Manifest) { m.Items[1].Label.Strict = [][]int{{2}} },
		"spell act false":         func(m *Manifest) { m.Items[1].Label.Act = false },
		"hold acts":               func(m *Manifest) { m.Items[0].Label.Act = true },
		"hold casts": func(m *Manifest) {
			m.Items[0].Label = Label{Alternatives: [][]int{{1}}, Strict: [][]int{{1}}, Act: true}
		},
		"hold without strict": func(m *Manifest) { m.Items[0].Label.Strict = nil },
		"one option":          func(m *Manifest) { m.Items[0].Options = []string{"Pass"} },
		"duplicate option":    func(m *Manifest) { m.Items[1].Options[2] = "Cast A" },
		"focus on a spell":    func(m *Manifest) { m.Items[1].Focus = "Grizzly Bears" },
		"bad tier":            func(m *Manifest) { m.Items[1].Tier = "T2" },
		"negative row":        func(m *Manifest) { m.Items[1].Row = -1 },
		"game spans two rows": func(m *Manifest) {
			m.Items[1].GameID = m.Items[0].GameID
			m.Items[1].DraftID = m.Items[0].DraftID
			m.Items[1].Split = SplitDev
			m.Selection = Selection{Dev: 2, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2}
		},
		"row names two games": func(m *Manifest) { m.Items[1].Row = m.Items[0].Row },
		"too many per game":   func(m *Manifest) { m.Items[1].GameID = m.Items[0].GameID; m.Selection.MaximumItemsPerGame = 1 },
		"duplicate world":     func(m *Manifest) { m.Items[1].WorldSeeds[7] = 1 },
		"old schema":          func(m *Manifest) { m.SchemaVersion = 1 },
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

func TestManifestAttackAndBlockLabels(t *testing.T) {
	base := testManifest().Items[1]
	for name, tc := range map[string]struct {
		it Item
		ok bool
	}{
		"attack yes":             {Item{Type: DecisionAttack, Focus: "Bear", Options: []string{"no", "yes"}, Label: Label{Alternatives: [][]int{{1}}, Strict: [][]int{{1}}, Act: true}}, true},
		"attack no":              {Item{Type: DecisionAttack, Focus: "Bear", Options: []string{"no", "yes"}, Label: Label{Alternatives: [][]int{{0}}, Strict: [][]int{{0}}}}, true},
		"attack no but act":      {Item{Type: DecisionAttack, Focus: "Bear", Options: []string{"no", "yes"}, Label: Label{Alternatives: [][]int{{0}}, Strict: [][]int{{0}}, Act: true}}, false},
		"attack lenient":         {Item{Type: DecisionAttack, Focus: "Bear", Options: []string{"no", "yes"}, Label: Label{Alternatives: [][]int{{0}, {1}}, Strict: [][]int{{1}}, Act: true}}, false},
		"attack without focus":   {Item{Type: DecisionAttack, Options: []string{"no", "yes"}, Label: Label{Alternatives: [][]int{{1}}, Strict: [][]int{{1}}, Act: true}}, false},
		"block an attacker":      {Item{Type: DecisionBlock, Focus: "Wall", Options: []string{"no block", "Ogre", "Goblin"}, Label: Label{Alternatives: [][]int{{2}}, Strict: [][]int{{2}}, Act: true}}, true},
		"block mixes no and yes": {Item{Type: DecisionBlock, Focus: "Wall", Options: []string{"no block", "Ogre", "Goblin"}, Label: Label{Alternatives: [][]int{{0}, {2}}, Strict: [][]int{{0}, {2}}}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			m := testManifest()
			it := base
			it.Type, it.Focus, it.Options, it.Label = tc.it.Type, tc.it.Focus, tc.it.Options, tc.it.Label
			m.Items[1] = it
			m.Digest = ""
			if err := m.Seal(); (err == nil) != tc.ok {
				t.Fatalf("Seal error = %v, want ok=%v", err, tc.ok)
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

func TestSBV1QuotasMatchSelection(t *testing.T) {
	for split, want := range map[Split]int{SplitTest: SBV1Selection.Test, SplitDev: SBV1Selection.Dev} {
		got := 0
		for _, n := range SBV1Quotas[split] {
			got += n
		}
		if got != want {
			t.Fatalf("%s quotas=%d want %d", split, got, want)
		}
	}
}

func TestValidateSBV1RejectsOrdinaryManifest(t *testing.T) {
	if err := testManifest().ValidateSBV1(); err == nil {
		t.Fatal("ordinary partial manifest passed sb-v1 validation")
	}
}

func TestSBV1WorldSeedsAreStableAndDistinct(t *testing.T) {
	a, b := SBV1WorldSeeds(), SBV1WorldSeeds()
	if len(a) != WorldCount || !uniqueSeeds(a) || !reflect.DeepEqual(a, b) {
		t.Fatalf("world seeds %v", a)
	}
}
