package main

// This test pins the real protocol.TableInfo.AutoMana doc comment to its
// generated placement: the field documentation the human payment-plan UI
// reads in web/src/protocol.ts must be the Go source's own comment, emitted
// as a block comment immediately before `auto_mana: boolean;` in the
// committed file. Commit 7022042e6 hand-edited the committed TypeScript to a
// different comment and drifted from the generator; fc7d924ad regenerated it.
// This test fails on any renewed drift in either direction.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestTableInfoAutoManaCommentMatchesGeneratedOutput asserts three things in
// order, each one the precondition of the next:
//  1. the Go source's AutoMana field carries the expected doc comment
//     (precondition — without it the pin below would be vacuous);
//  2. generation emits that comment as a block immediately before the
//     auto_mana field declaration;
//  3. the committed web/src/protocol.ts is byte-identical to that output.
func TestTableInfoAutoManaCommentMatchesGeneratedOutput(t *testing.T) {
	const wantDoc = "AutoMana enables the human payment-plan UI for this table."

	// 1. Precondition: read the doc comment straight out of the Go source.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "../../protocol/protocol.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse protocol source: %v", err)
	}
	var gotDoc string
	ok := false
	for _, decl := range file.Decls {
		gd, okDecl := decl.(*ast.GenDecl)
		if !okDecl || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, okSpec := spec.(*ast.TypeSpec)
			if !okSpec || ts.Name.Name != "TableInfo" {
				continue
			}
			st, okStruct := ts.Type.(*ast.StructType)
			if !okStruct {
				t.Fatal("precondition: protocol.TableInfo is not a struct")
			}
			for _, field := range st.Fields.List {
				if len(field.Names) == 1 && field.Names[0].Name == "AutoMana" {
					if field.Doc == nil {
						t.Fatal("precondition: AutoMana carries no doc comment in the Go source")
					}
					gotDoc = strings.TrimSpace(field.Doc.Text())
					ok = true
				}
			}
		}
	}
	if !ok {
		t.Fatal("precondition: no AutoMana field found on protocol.TableInfo")
	}
	if gotDoc != wantDoc {
		t.Fatalf("precondition: AutoMana doc comment is %q, want %q (the source comment moved — re-pin this test)", gotDoc, wantDoc)
	}

	// 2. Generation emits the comment as a block directly on this field.
	src, err := Render()
	if err != nil {
		t.Fatal(err)
	}
	wantBlock := "/**\n   * " + wantDoc + "\n   */\n  auto_mana: boolean;"
	if !strings.Contains(src, wantBlock) {
		t.Errorf("generated output does not carry the AutoMana doc comment immediately before auto_mana: boolean;\nwant block:\n%s", wantBlock)
	}

	// 3. The committed file matches the generated output.
	got, err := os.ReadFile("../../web/src/protocol.ts")
	if err != nil {
		t.Fatalf("%v — run make gentypes", err)
	}
	if string(got) != src {
		t.Fatal("web/src/protocol.ts drifted from the generator — run make gentypes")
	}
}
