package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestDigUntilKnownKeysSorted: the unread lookup binary-searches the table.
func TestDigUntilKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(digUntilKnownKeys[:]) || len(slices.Compact(slices.Clone(digUntilKnownKeys[:]))) != len(digUntilKnownKeys) {
		t.Fatal("digUntilKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileDigUntil pins the compiled shapes the resolution reads: the
// parsed destinations, the riders, the Amount$ literal, the withheld list in
// its historical order and the unread report.
func TestCompileDigUntil(t *testing.T) {
	sa := &cards.SA{API: "DigUntil", Params: map[string]string{
		"Valid": "Permanent.Creature", "RevealedDestination": " Graveyard ", "FoundDestination": "Battlefield",
		"RevealedLibraryPosition": " -1 ", "OptionalFoundMove": "True", "NoMoveRevealed": "true",
		"RevealRandomOrder": " True ", "Tapped": "True", "GainControl": "True", "RememberFound": "True",
		"RememberRevealed": "True", "Amount": " 3 ", "DigZone": "PlanarDeck", "NoMoveFound": "Maybe",
		"Shuffle": "True", "ShuffleCondition": "Always", "ImprintFound": "True", "ImprintRevealed": "False",
		"FoundLibraryPosition": "2", "NoneFoundLibraryPosition": "7", "OptionalNoDestination": "Exile",
		"Defined": " You", "Hidden": "True",
	}}
	p := DigUntilOf(sa)
	if p.Spec != "PermanentCard.Creature" || p.RevDest != state.ZGraveyard || p.FoundWithRevealed ||
		p.FoundDest != state.ZBattlefield || p.RevPos != "-1" || p.DeclineDest != state.ZExile || p.Defined.Raw != " You" || !p.Defined.Is(RefYou) {
		t.Fatalf("destinations = %+v", p)
	}
	if !p.OptionalMove || !p.NoMoveRevealed || !p.RevealRandomOrder || !p.Tapped || !p.GainControl ||
		!p.RememberFound || !p.RememberRevealed || p.NoMoveFound || !p.Shuffle || p.ShuffleNoneFound ||
		!p.ImprintFound || p.ImprintRevealed || p.FoundPos != "" || p.AmountRaw != "3" || p.AmountLit != 3 {
		t.Fatalf("riders = %+v", p)
	}
	if !p.NoneFoundSet || p.NoneFoundDest != state.ZLibrary || p.NoneFoundPos != "" {
		t.Fatalf("none-found = %+v", p)
	}
	want := []string{"DigZone$ PlanarDeck", "NoMoveFound$ Maybe", "ShuffleCondition$ Always",
		"FoundLibraryPosition$ 2", "NoneFoundLibraryPosition$ 7"}
	if !slices.Equal(p.Withheld, want) {
		t.Fatalf("withheld = %q, want %q", p.Withheld, want)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if DigUntilOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	def := DigUntilOf(&cards.SA{API: "DigUntil", Params: map[string]string{"Amount": "X",
		"ShuffleCondition": "NoneFound", "NoneFoundDestination": "Hand", "NoneFoundLibraryPosition": "-1"}})
	if def.Spec != "Card" || def.RevDest != state.ZLibrary || !def.FoundWithRevealed || def.FoundDest != state.ZLibrary ||
		def.DeclineDest != state.ZLibrary || def.AmountRaw != "X" || def.AmountLit != 0 || !def.ShuffleNoneFound ||
		def.NoneFoundDest != state.ZHand || def.NoneFoundPos != "-1" || len(def.Withheld) != 0 {
		t.Fatalf("defaults = %+v", def)
	}
}

// TestDigUntilOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing.
func TestDigUntilOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "DigUntil", Params: map[string]string{"Valid": "Creature", "FoundDestination": "Hand"}}
	f := NewSAFacts(bound)
	f.Publish()
	cached := &cards.SA{API: "DigUntil", Params: map[string]string{"Valid": "Land"}}
	DigUntilOf(cached)
	if n := allocsPerRun(100, func() {
		_ = DigUntilOf(bound)
		_ = DigUntilOf(cached)
	}); n != 0 {
		t.Fatalf("DigUntilOf allocated %v objects per run; want 0", n)
	}
	if f.DigUntil == nil || !f.DigUntil.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the DigUntil half")
	}
}
