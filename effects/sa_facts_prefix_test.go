package effects

import (
	"testing"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects/params"
)

// TestFactsIsSAFactsPrefix pins params.LoadFacts' reading of a published
// SAFacts as a *params.Facts: the leaf half must stay SAFacts' first field,
// and the configured record's leaf records are the ones the leaf readers
// serve.
func TestFactsIsSAFactsPrefix(t *testing.T) {
	if off := unsafe.Offsetof(SAFacts{}.Facts); off != 0 {
		t.Fatalf("SAFacts.Facts is at offset %d; it must be the first field", off)
	}
	const src = "Name:Prefix Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\n" +
		"A:AB$ Mana | Cost$ T | Produced$ G | Defined$ You\nOracle:x\n"
	card, diags := cards.ParseBytes("inline-prefix-elf.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("fixture parse: %v", diags)
	}
	card.Link()
	if len(card.Faces[0].Abilities) != 1 || card.Faces[0].Abilities[0].ExtSlot() == nil {
		t.Fatal("precondition: one derived mana ability with a facts slot")
	}
	sa := card.Faces[0].Abilities[0]
	f := NewSAFacts(sa)
	f.Publish()
	if params.LoadFacts(sa) != &f.Facts {
		t.Fatal("params.LoadFacts does not read the published record's leaf half")
	}
	if ManaOf(sa) != f.Mana || TargetsOf(sa) != f.Targets || DefinedOf(sa) != f.Defined {
		t.Fatal("the leaf readers do not serve the configured record")
	}
}
