package main

import (
	"go/types"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/tsgen"
	"github.com/adams-shaun/gorge/protocol"
)

func TestCommittedProtocolTSIsFresh(t *testing.T) {
	want, err := Render()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../web/src/protocol.ts")
	if err != nil {
		t.Fatalf("%v — run make gentypes", err)
	}
	if string(got) != want {
		t.Fatal("web/src/protocol.ts is stale — run make gentypes")
	}
}

func TestFrameTypeUnionListsEveryConstant(t *testing.T) {
	src, _ := Render()
	for _, ft := range []protocol.FrameType{protocol.THello, protocol.TWidget, protocol.TMatchStart, protocol.TSnapshot,
		protocol.TEvent, protocol.TDecision, protocol.TMatchEnd, protocol.TTableHalted, protocol.TOverflow, protocol.TError} {
		if !strings.Contains(src, `"`+string(ft)+`"`) {
			t.Errorf("FrameType union lacks %q", ft)
		}
	}
	for _, name := range []string{"View", "PlayerView", "CardView", "StackView", "TargetView", "PendingView", "Printing", "Decision", "Option", "TargetEffect", "DamageEffect"} {
		if !strings.Contains(src, "export interface "+name+" {") {
			t.Errorf("view/decision type %s missing from the generated output", name)
		}
	}
}

// leafRoot type-checks leaves_test.go from source and returns its named type.
func leafRoot(t *testing.T, l *tsgen.Loader, name string) types.Type {
	t.Helper()
	pkg, err := l.CheckFiles("github.com/adams-shaun/gorge/cmd/gentypes", "leaves_test.go")
	if err != nil {
		t.Fatal(err)
	}
	ty, err := tsgen.LookupIn(pkg, name)
	if err != nil {
		t.Fatal(err)
	}
	return ty
}

// Leaf 1: a field's Go doc comment is emitted as a block comment in the
// generated TypeScript. Table-driven over small synthetic structs so it does
// not depend on the real protocol package.
func TestGentypesEmitsFieldDocComment(t *testing.T) {
	cases := []struct {
		name   string
		root   string
		wants  []string
		blocks int // number of /* */ blocks the struct's fields should emit
	}{
		{
			name:   "first struct",
			root:   "gt1LeafOne",
			wants:  []string{"Alpha is the first documented field; it must appear.", "Beta is documented too, and carries omitempty."},
			blocks: 2, // Alpha and Beta only; Gamma (trailing comment) gets none
		},
		{
			name:   "second struct",
			root:   "gt1LeafTwo",
			wants:  []string{"Delta carries its own struct's field comment."},
			blocks: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := tsgen.Generate(tsgen.Options{Roots: []types.Type{leafRoot(t, tsgen.NewLoader(), c.root)}, Header: "// test header\n"})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.wants {
				if !strings.Contains(src, w) {
					t.Errorf("missing field doc comment %q; generated:\n%s", w, src)
				}
			}
			// Gamma carries only a trailing (non-doc) comment, so it must not
			// add a block. The block count pins that the undocumented field
			// was passed over.
			if got := strings.Count(src, "*/"); got != c.blocks {
				t.Errorf("generated %d comment blocks, want %d; generated:\n%s", got, c.blocks, src)
			}
		})
	}
}

// Leaf 2: a doc comment containing `*/` is escaped, so it cannot terminate its
// own block comment early. The generated text has exactly one block terminator
// (the `*/` that closes the single comment) and the field that follows it still
// appears.
func TestGentypesEscapesCommentTerminator(t *testing.T) {
	src, err := tsgen.Generate(tsgen.Options{Roots: []types.Type{leafRoot(t, tsgen.NewLoader(), "gt1LeafPathological")}, Header: "// test header\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(src, "*/"); got != 1 {
		t.Fatalf("comment terminator appears %d times (want 1, block ended early or doubled); generated:\n%s", got, src)
	}
	if !strings.Contains(src, "a* /b") {
		t.Errorf("pathological `*/` not escaped; generated:\n%s", src)
	}
	if !strings.Contains(src, "  y: number;") {
		t.Errorf("field after the pathological comment is missing; generated:\n%s", src)
	}
}

// Leaf 3: generating twice from the same input is byte-identical, so the
// freshness gate is not a coin flip between runs.
func TestGentypesIsDeterministic(t *testing.T) {
	l := tsgen.NewLoader()
	opts := tsgen.Options{
		Roots:  []types.Type{leafRoot(t, l, "gt1LeafOne"), leafRoot(t, l, "gt1LeafPathological")},
		Unions: map[string][]string{"Z": {"z"}, "A": {"a"}},
		Header: "// test header\n",
	}
	a, err := tsgen.Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := tsgen.Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("two generations differ")
	}
}

// Leaf 4: the Group exclusivity contract sentence — the thing a rules-ignorant
// client must be able to read in the file it imports — is carried into the
// generated output. This fails on a generator that emits no comments at all.
func TestGeneratedOutputCarriesGroupContract(t *testing.T) {
	src, err := Render()
	if err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{"Group is an exclusivity marker", "mutually exclusive", "rules-ignorant client"} {
		if !strings.Contains(src, frag) {
			t.Errorf("Group contract %q absent from rendered output", frag)
		}
	}
}
