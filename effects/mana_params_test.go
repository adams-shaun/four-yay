package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestManaKnownKeysSorted: the unread lookup and the plain-shape scan
// binary-search their tables.
func TestManaKnownKeysSorted(t *testing.T) {
	for name, tab := range map[string][]string{"manaKnownKeys": manaKnownKeys[:], "plainManaKeys": plainManaKeys[:]} {
		if !slices.IsSorted(tab) || len(slices.Compact(slices.Clone(tab))) != len(tab) {
			t.Fatalf("%s must be sorted and duplicate-free", name)
		}
	}
}

// TestCompileMana pins the compiled shapes effMana and rules' mana paths
// read, the plain shape, the unread report and the zero-alloc front-cache
// hit.
func TestCompileMana(t *testing.T) {
	sa := &cards.SA{API: "Mana", Params: map[string]string{
		"Produced": " Combo R G ", "Amount": " 2", "RestrictValid": " Spell.Dragon ",
		"AddsNoCounter": "True ", "PersistentMana": " True", "TriggersWhenSpent": " TrigX",
		"AddsCounters": "P1P1", "PersistentUntilEndOfCombat": "True", "Defined": "You", "Bogus": "1",
	}}
	p := ManaOf(sa)
	if !p.HasProduced || p.Produced != "Combo R G" || p.ProducedRaw != " Combo R G " ||
		p.Amount != (ParamText{Text: " 2", Present: true}) || p.AmountTrim != "2" || !p.AmountIsLit || p.AmountLit != 2 ||
		p.RestrictValid != "Spell.Dragon" || p.AddsNoCounter != "True" || p.PersistentMana != "True" ||
		p.TriggersWhenSpent != "TrigX" || p.AddsCounters != "P1P1" || p.PersistentUntilEndOfCombat != "True" ||
		!p.HasDefined || p.PlainSym != 0 {
		t.Fatalf("compiled = %+v", p)
	}
	if c, any := cards.ProducedCounts(" Combo R G "); p.Counts != c || p.CountsAny != any {
		t.Fatalf("counts = %v %v", p.Counts, p.CountsAny)
	}
	if !slices.Equal(p.Unread, []string{"Bogus"}) {
		t.Fatalf("unread = %v", p.Unread)
	}
	if allocs := allocsPerRun(100, func() { _ = ManaOf(sa) }); allocs != 0 {
		t.Fatalf("ManaOf front-cache hit allocates %v", allocs)
	}
	plain := ManaOf(&cards.SA{API: "Mana", Params: map[string]string{"Cost": "T", "Produced": "G", "Amount": "2", "SpellDescription": "x"}})
	if plain.PlainSym != 'G' || plain.PlainAmt != 2 || plain.Unread != nil {
		t.Fatalf("plain = %+v", plain)
	}
	for _, params := range []map[string]string{
		{"Cost": "T", "Produced": "G", "Amount": "X"},
		{"Cost": "T", "Produced": "GG"},
		{"Cost": "T", "Produced": "G", "Defined": "You"},
		{"Cost": "T"},
	} {
		if q := ManaOf(&cards.SA{API: "Mana", Params: params}); q.PlainSym != 0 {
			t.Fatalf("%v is not plain: %+v", params, q)
		}
	}
	if b := ManaOf(&cards.SA{API: "Mana", Params: map[string]string{}}); b.HasProduced || b.Amount.Present || b.Unread != nil {
		t.Fatalf("bare mana = %+v", b)
	}
}
