package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestManaReflectedKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestManaReflectedKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(manaReflectedKnownKeys[:]) || len(slices.Compact(slices.Clone(manaReflectedKnownKeys[:]))) != len(manaReflectedKnownKeys) {
		t.Fatal("manaReflectedKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileManaReflected pins the compiled shapes effManaReflected,
// ManaReflectedCandidates and rules' activation gate read, the unread report
// and the zero-alloc front-cache hit.
func TestCompileManaReflected(t *testing.T) {
	sa := &cards.SA{API: "ManaReflected", Params: map[string]string{
		"ReflectProperty": " Produce ", "ColorOrType": "Type", "Valid": " Land.OppCtrl ",
		"Produced": " R", "Amount": "2", "RestrictValid": " Spell ", "Defined": " You ",
		"ClassBand": "2", "IsPresent": " Creature.YouCtrl", "PresentCompare": " GE2 ", "Bogus": "1",
	}}
	p := ManaReflectedOf(sa)
	if p.ReflectProperty != "Produce" || !p.WidenType || p.Valid != "Land.OppCtrl" || p.Produced != "R" ||
		p.Amount != (ParamText{Text: "2", Present: true}) || p.RestrictValid != "Spell" || p.Defined != "You" ||
		p.ClassBand != "2" || p.IsPresent != " Creature.YouCtrl" || p.PresentCompare != "GE2" {
		t.Fatalf("compiled = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Bogus"}) {
		t.Fatalf("unread = %v", p.Unread)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = ManaReflectedOf(sa) }); allocs != 0 {
		t.Fatalf("ManaReflectedOf front-cache hit allocates %v", allocs)
	}
	b := ManaReflectedOf(&cards.SA{API: "ManaReflected", Params: map[string]string{"IsPresent": "  ", "ColorOrType": "Color"}})
	if b.IsPresent != "" || b.WidenType || b.Amount.Present || b.Unread != nil {
		t.Fatalf("bare = %+v", b)
	}
}
