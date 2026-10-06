package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestNewOwnsTokenMap pins the guarantee that a shared Config.Tokens map (a
// corpus registry's) is never mutated by an engine. New no longer copies the
// map -- nothing writes Game.Tokens after genesis -- so the guarantee is that
// building and running a game leaves the caller's map untouched, and that the
// one sanctioned way for a fixture to register a token (setFixtureToken)
// copies before it writes.
func TestNewOwnsTokenMap(t *testing.T) {
	tok := card(t, "Name:Initial Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	const initialKey = "fixture:initial-token"
	callerTokens := map[string]*cards.Card{initialKey: tok}
	before := maps.Clone(callerTokens)
	cfg := Config{
		Seed: 1, Names: []string{"a", "b"}, Tokens: callerTokens,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
	}

	e := New(cfg)
	if got := e.G.Tokens[initialKey]; got != tok {
		t.Fatalf("precondition: engine did not preserve initial token definition: got %p, want %p", got, tok)
	}
	if reflect.ValueOf(e.G.Tokens).Pointer() != reflect.ValueOf(callerTokens).Pointer() {
		t.Fatal("precondition: New copied Config.Tokens; the shared-map contract is gone")
	}
	e.Advance() // genesis deals and the first decision opens; neither may write the map
	if !maps.Equal(callerTokens, before) {
		t.Fatalf("building the engine mutated the caller's token map: %v", callerTokens)
	}

	const engineKey = "fixture:engine-only-token"
	setFixtureToken(e, engineKey, tok)
	if e.G.Tokens[engineKey] != tok {
		t.Fatal("precondition: fixture registration did not reach the engine")
	}
	if _, ok := callerTokens[engineKey]; ok {
		t.Fatal("engine token registration mutated caller's map")
	}
	if reflect.ValueOf(e.G.Tokens).Pointer() == reflect.ValueOf(callerTokens).Pointer() {
		t.Fatal("registration kept the shared map instead of copying it first")
	}
}

// gameTokenWrites lists the lines in src that assign into a `<x>.G.Tokens[k]`
// map entry in place, outside the function named allowed.
func gameTokenWrites(t *testing.T, name string, src any, allowed string) []token.Position {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var hits []token.Position
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == allowed {
			continue
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range as.Lhs {
				ix, ok := lhs.(*ast.IndexExpr)
				if !ok {
					continue
				}
				tokSel, ok := ix.X.(*ast.SelectorExpr)
				if !ok || tokSel.Sel.Name != "Tokens" {
					continue
				}
				if gSel, ok := tokSel.X.(*ast.SelectorExpr); ok && gSel.Sel.Name == "G" {
					hits = append(hits, fset.Position(as.Pos()))
				}
			}
			return true
		})
	}
	return hits
}

// TestNoInPlaceGameTokenWrites is the census behind the shared token map: New
// hands every engine the caller's Config.Tokens by reference, so a rules
// source or test that writes `e.G.Tokens[k] = v` would write into a corpus
// registry shared by every engine. Fixtures register through setFixtureToken.
func TestNoInPlaceGameTokenWrites(t *testing.T) {
	const bad = "package p\nfunc f(e *E) { e.G.Tokens[\"k\"] = nil }\n"
	if got := gameTokenWrites(t, "bad.go", bad, "none"); len(got) != 1 {
		t.Fatalf("detector precondition: want 1 hit on a known violation, got %v", got)
	}
	if got := gameTokenWrites(t, "bad.go", bad, "f"); len(got) != 0 {
		t.Fatalf("detector precondition: the allowed function must be skipped, got %v", got)
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob rules/*.go: %v (%d files)", err, len(files))
	}
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, pos := range gameTokenWrites(t, name, src, "setFixtureToken") {
			t.Errorf("%s: in-place write to Game.Tokens; use setFixtureToken", pos)
		}
	}
}
