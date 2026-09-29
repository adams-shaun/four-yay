package deck

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// parseManifestCard compiles a minimal one-face card for manifest tests.
func parseManifestCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("manifest.txt", []byte("Name:"+name+"\nTypes:Creature\nPT:1/1\n"))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	c.Link()
	return c
}

// TestManifestCanonicalizesWithoutAliasing pins the canonicalization contract:
// duplicate names collapse to one sorted, positively counted row; sideboard
// rows are the same shape; commanders preserve their DECLARED order and stay
// counted in main; and neither the configured slice nor a published clone is
// aliased.
func TestManifestCanonicalizesWithoutAliasing(t *testing.T) {
	alpha := parseManifestCard(t, "Alpha")
	beta := parseManifestCard(t, "Beta")
	gamma := parseManifestCard(t, "Gamma")
	// main deliberately out of name order and with a repeat.
	main := []*cards.Card{gamma, alpha, beta, alpha}
	sideboard := []*cards.Card{beta, beta, beta}
	// Declared commander order is Gamma then Beta: index 0 first, index 2
	// second. This differs from canonical alphabetical order and proves the
	// field preserves the declared order rather than sorting identities.
	got := NewManifest("deck", main, sideboard, []int{0, 2})
	want := Manifest{
		Name:       "deck",
		Main:       []ManifestRow{{Name: "Alpha", Count: 2}, {Name: "Beta", Count: 1}, {Name: "Gamma", Count: 1}},
		Sideboard:  []ManifestRow{{Name: "Beta", Count: 3}},
		Commanders: []string{"Gamma", "Beta"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
	// The commander identities remain counted in main.
	for _, name := range got.Commanders {
		if row := rowFor(got.Main, name); row == nil || row.Count != 1 {
			t.Fatalf("commander %q is not counted in main: %#v", name, got.Main)
		}
	}
	// The configured slice is not retained.
	main[0] = beta
	if rowFor(got.Main, "Gamma") == nil {
		t.Fatal("manifest retained the configured deck slice")
	}
	// A published clone owns its storage.
	clone := got.Clone()
	clone.Main[0].Name = "mutated"
	clone.Commanders[0] = "mutated"
	if got.Main[0].Name != "Alpha" || got.Commanders[0] != "Gamma" {
		t.Fatal("Clone shares mutable row or commander storage")
	}
}

// TestManifestZeroShape pins the wire shape the spec requires: an empty main
// is [] not null, an absent sideboard is omitted, and a malformed commander
// index is skipped exactly as genesis skips it.
func TestManifestZeroShape(t *testing.T) {
	one := parseManifestCard(t, "One")
	empty := NewManifest("empty", nil, nil, nil)
	if empty.Main == nil {
		t.Fatal("empty main is nil; JSON would be null, not []")
	}
	b, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"name":"empty","main":[]}` {
		t.Fatalf("empty manifest JSON = %s", b)
	}
	// -1 and past-the-end indices are skipped, not panicked on.
	bad := NewManifest("bad", []*cards.Card{one}, nil, []int{-1, 5})
	if len(bad.Commanders) != 0 {
		t.Fatalf("out-of-range commander indices survived: %#v", bad.Commanders)
	}
}

func rowFor(rows []ManifestRow, name string) *ManifestRow {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}
