package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestDrawKnownKeysSorted: the unread lookup binary-searches the table.
func TestDrawKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(drawKnownKeys[:]) || len(slices.Compact(slices.Clone(drawKnownKeys[:]))) != len(drawKnownKeys) {
		t.Fatal("drawKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileDraw pins the compiled shapes effDraw and rules' scry
// replacement read, the unread report and the zero-alloc front-cache hit.
func TestCompileDraw(t *testing.T) {
	sa := &cards.SA{API: "Draw", Params: map[string]string{
		"NumCards": "X", "RememberDrawn": "AllReplaced", "OptionalDecider": "You",
		"Upto": "true", "Bogus": "1",
	}}
	p := DrawOf(sa)
	if p.NumCards != (ParamText{Text: "X", Present: true}) || !p.RememberDrawn ||
		p.OptionalDecider != "You" || !p.Upto {
		t.Fatalf("compiled = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Bogus"}) {
		t.Fatalf("unread = %v", p.Unread)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = DrawOf(sa) }); allocs != 0 {
		t.Fatalf("DrawOf front-cache hit allocates %v", allocs)
	}
	if d := DrawOf(&cards.SA{API: "Draw", Params: map[string]string{}}); d.NumCards.Present || d.Upto || d.Unread != nil {
		t.Fatalf("bare draw = %+v", d)
	}
}
