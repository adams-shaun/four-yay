package deck

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestManifestCanonicalizesWithoutAliasing(t *testing.T) {
	parse := func(name string) *cards.Card {
		t.Helper()
		c, diags := cards.ParseBytes("manifest.txt", []byte("Name:"+name+"\nTypes:Creature\nPT:1/1\n"))
		if len(diags) != 0 {
			t.Fatal(diags)
		}
		c.Link()
		return c
	}
	a, b := parse("Zebra"), parse("Alpha")
	main := []*cards.Card{a, b, a}
	got := NewManifest("deck", main, []*cards.Card{b, b, a}, []int{2, 0})
	want := Manifest{Name: "deck", Main: []ManifestRow{{Name: "Alpha", Count: 1}, {Name: "Zebra", Count: 2}}, Sideboard: []ManifestRow{{Name: "Alpha", Count: 2}, {Name: "Zebra", Count: 1}}, Commanders: []string{"Zebra", "Zebra"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
	main[0] = b
	if got.Main[1].Name != "Zebra" {
		t.Fatal("manifest retained the configured deck slice")
	}
	clone := got.Clone()
	clone.Main[0].Name = "mutated"
	if got.Main[0].Name != "Alpha" {
		t.Fatal("Clone shares mutable row storage")
	}
}
