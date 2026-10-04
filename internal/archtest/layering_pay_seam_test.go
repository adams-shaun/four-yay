package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// layering_pay_seam_test.go pins the two role interfaces rules/pay reaches
// the engine through besides pay.Engine (lasagna spec §9.2): chars.Reader,
// the characteristics layer's read direction, and the rule that the payment
// layer never holds an effects.Host -- an evaluation that needs one goes
// through a named, counted pay.Eval method on the engine side.

// charsReaderMethods is the method count of chars.Reader
// (rules/chars/reader.go). It is a BUDGETED ratchet like payEngineMethods:
// it may rise only in a commit that moves ring code onto the new read, the
// comment recording the step, and never above charsReaderCeiling (the
// spec's narrow-interface target). E4 slice 1 landed it with the six reads
// the mana activation gates use: DerivedTypes, HasKeyword, SVarGate,
// ActivationPhasesOK, PresentGate, GrantedAbilities. Slice 5 added
// SameColorRevealSets and TapPower (the non-mana cost-part castability walk,
// nonManaCastableP): 6 -> 8. E7 flow slice 1: ZoneEntrySeq (the zone-entry
// index read, moved off pay.Engine to make room for the flow seam): 8 -> 9.
const (
	charsReaderMethods = 9
	charsReaderCeiling = 20
)

// TestCharsReaderBudget counts chars.Reader's declared methods.
func TestCharsReaderBudget(t *testing.T) {
	n := interfaceMethods(t, filepath.Join("..", "..", "rules", "chars"), "Reader")
	switch {
	case n < 0:
		t.Fatal("rules/chars declares no Reader interface; the budget would run vacuously")
	case n > charsReaderCeiling:
		t.Errorf("chars.Reader has %d methods, above the ceiling %d", n, charsReaderCeiling)
	case n > charsReaderMethods:
		t.Errorf("chars.Reader has %d methods, above the recorded %d: raise charsReaderMethods in the "+
			"commit that moves ring code onto the new read, with the step in its comment", n, charsReaderMethods)
	case n < charsReaderMethods:
		t.Errorf("chars.Reader shrank to %d methods; lower charsReaderMethods (%d) to match", n, charsReaderMethods)
	}
}

// interfaceMethods counts the methods interface name declares in dir's
// non-test files (-1 when it is not declared). An embedded interface fails
// the test: the budgeted interfaces declare every method so the count is
// visible.
func interfaceMethods(t *testing.T, dir, name string) int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	n := -1
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			ts, ok := node.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}
			n = 0
			for _, m := range it.Methods.List {
				if len(m.Names) == 0 {
					t.Errorf("%s.%s embeds %v; declare its methods directly so the budget sees them", filepath.Base(dir), name, m.Type)
					continue
				}
				n += len(m.Names)
			}
			return false
		})
	}
	return n
}

// TestPayHoldsNoHost fails on any reference to effects.Host in rules/pay's
// non-test files. The payment layer may name effects.Ctx and call the pure
// effects helpers, but an evaluation that needs the engine as a Host goes
// through pay.Eval, so the Host never crosses into the package.
func TestPayHoldsNoHost(t *testing.T) {
	dir := filepath.Join("..", "..", "rules", "pay")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatal("rules/pay has no files")
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		local := ""
		for _, is := range f.Imports {
			if p, _ := strconv.Unquote(is.Path.Value); p == module+"/effects" {
				local = "effects"
				if is.Name != nil {
					local = is.Name.Name
				}
			}
		}
		if local == "" {
			continue
		}
		ast.Inspect(f, func(node ast.Node) bool {
			se, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := se.X.(*ast.Ident); ok && id.Name == local && se.Sel.Name == "Host" {
				t.Errorf("%s: rules/pay names effects.Host; evaluate through pay.Eval instead (lasagna spec §9.2)", fset.Position(se.Pos()))
			}
			return true
		})
	}
}

// payEvalMethods is the method count of pay.Eval (rules/pay/eval.go), the
// payment layer's evaluation seam, budgeted like charsReaderMethods. E4
// slice 2 landed it with EvalCount, MatchesSpec and WindowUnits (the
// unless-payment reachability and the SVar-fixed cost counts). Slice 6
// added ManaShape, ManaStatic and SourceInterference (the planner's
// per-source alternatives and tiers): 3 -> 6. Slice 7 added ManaUnits (the
// planner's source census): 6 -> 7. E7 flow slice 2 moved Conv,
// MayPlayRider and PayLifeInsteadOfB (static-pricing evaluations) off
// pay.Engine onto it: 7 -> 10.
const (
	payEvalMethods = 10
	payEvalCeiling = 20
)

// TestPayEvalBudget counts pay.Eval's declared methods.
func TestPayEvalBudget(t *testing.T) {
	n := interfaceMethods(t, filepath.Join("..", "..", "rules", "pay"), "Eval")
	switch {
	case n < 0:
		t.Fatal("rules/pay declares no Eval interface; the budget would run vacuously")
	case n > payEvalCeiling:
		t.Errorf("pay.Eval has %d methods, above the ceiling %d", n, payEvalCeiling)
	case n > payEvalMethods:
		t.Errorf("pay.Eval has %d methods, above the recorded %d: raise payEvalMethods in the "+
			"commit that moves ring code onto the new evaluation, with the step in its comment", n, payEvalMethods)
	case n < payEvalMethods:
		t.Errorf("pay.Eval shrank to %d methods; lower payEvalMethods (%d) to match", n, payEvalMethods)
	}
}
