package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestReplaceEffectKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestReplaceEffectKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(replaceEffectKnownKeys[:]) || len(slices.Compact(slices.Clone(replaceEffectKnownKeys[:]))) != len(replaceEffectKnownKeys) {
		t.Fatal("replaceEffectKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileReplaceEffect pins the compiled shapes effReplaceEffect and
// rules' ReplaceWith$ readers use, the unread report and the zero-alloc
// front-cache hit.
func TestCompileReplaceEffect(t *testing.T) {
	sa := &cards.SA{API: "ReplaceEffect", Params: map[string]string{
		"VarName": "DamageAmount", "VarValue": " ReplaceCount$DamageAmount/Twice ", "VarType": "Card",
	}}
	p := ReplaceEffectOf(sa)
	if p.VarName != "DamageAmount" || p.VarValue != (ParamText{Text: " ReplaceCount$DamageAmount/Twice ", Present: true}) {
		t.Fatalf("compiled = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"VarType"}) {
		t.Fatalf("unread = %v", p.Unread)
	}
	if allocs := allocsPerRun(100, func() { _ = ReplaceEffectOf(sa) }); allocs != 0 {
		t.Fatalf("ReplaceEffectOf front-cache hit allocates %v", allocs)
	}
	if d := ReplaceEffectOf(&cards.SA{API: "ReplaceEffect", Params: map[string]string{}}); d.VarName != "" || d.VarValue.Present || d.Unread != nil {
		t.Fatalf("bare body = %+v", d)
	}
}
