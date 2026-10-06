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

// TestGenSplitRoomCastShape is the gorge-side pin for the scenario shape the
// XMage driver relies on to cast a split or Room half (its ScenarioReplay
// castSpelling). The manifest names the whole card ("A // B"); the corpus
// resolves that to the front face (CorpusNameFold cuts on " // "), so the
// pipeline produces:
//
//   - card     = the half ("Cease"), the scenario's gorge spelling,
//   - setup hand = the half (the card gorge deals),
//   - xmage_name = the whole "A // B" (the spelling XMage's card object has),
//   - the cast step ref = "p0:<half>", which the driver sends as XMage's
//     cast command (its SpellAbility is "Cast <half>").
//
// The driver must therefore deal the WHOLE card into XMage's hand
// (xmageSpelling) but cast the HALF (castSpelling); casting the whole name is
// "Can't find ability to activate command: Cast <half>". A scenario that
// stopped carrying xmage_name, or that named the cast after the whole card,
// would break that driver contract, so pin all four here.
func TestGenSplitRoomCastShape(t *testing.T) {
	dir := filepath.Join("..", "..", ".cards")
	m := compliance.Manifest{Code: "TINY", Cards: []compliance.ManifestCard{
		{Name: "Cease // Desist"},
		{Name: "Walk-In Closet // Forgotten Cellar"},
	}}
	tmp := t.TempDir()
	mf := filepath.Join(tmp, "TINY.json")
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mf, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "TINY.jsonl")
	if err := runGen(dir, mf, out, "A"); err != nil {
		t.Fatal(err)
	}

	items := map[string]oraclegen.Item{}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 64<<20)
	for s.Scan() {
		var it oraclegen.Item
		if err := json.Unmarshal(s.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		items[it.Card] = it
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		half, whole string
	}{
		{"Cease", "Cease // Desist"},
		{"Walk-In Closet", "Walk-In Closet // Forgotten Cellar"},
	}
	for _, tc := range cases {
		t.Run(tc.half, func(t *testing.T) {
			it, ok := items[tc.half]
			if !ok {
				// Precondition: the manifest card must generate a scenario,
				// not a skip; otherwise the shape assertions never run.
				skips, _ := os.ReadFile(out + ".skips.jsonl")
				t.Fatalf("no scenario for %q (skips: %s)", tc.half, skips)
			}
			if it.Card != tc.half {
				t.Fatalf("card = %q, want the half %q", it.Card, tc.half)
			}
			if it.XMageName != tc.whole {
				t.Fatalf("xmage_name = %q, want the whole %q", it.XMageName, tc.whole)
			}
			hand := it.Setup["p0"].Hand
			if len(hand) == 0 || hand[0] != tc.half {
				t.Fatalf("setup hand = %v, want the half %q first", hand, tc.half)
			}
			if len(it.Steps) == 0 || it.Steps[0].Op != "cast" {
				t.Fatalf("first step = %+v, want a cast", it.Steps)
			}
			if want := "p0:" + tc.half; it.Steps[0].Card != want {
				t.Fatalf("cast ref = %q, want %q", it.Steps[0].Card, want)
			}
		})
	}
}
