package view

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestCopyDecisionPaymentOwnership(t *testing.T) {
	baseOption := 3
	d := &decision.Decision{
		Options: []decision.Option{{Index: 0, Label: "Mountain: CARDNAME"}},
		PaymentActions: []decision.PaymentAction{{
			ID:              "action",
			BaseOptionIndex: &baseOption,
			Plans: []decision.PaymentPlan{{
				ID:          "plan",
				Activations: []decision.PaymentActivation{{Source: state.ObjID(9)}},
			}},
		}},
		PaymentFallback: &decision.PaymentFallback{PlanID: "fallback", Reason: "original"},
	}
	if d.PaymentActions[0].BaseOptionIndex == nil || d.PaymentFallback == nil ||
		d.PaymentActions[0].Plans[0].Activations == nil || len(d.Options) != 1 {
		t.Fatal("fixture does not exercise all decision-copy ownership fields")
	}
	if d.Options[0].Label == "Mountain: Mountain" || d.PaymentActions[0].Plans[0].Activations[0].Source == 10 {
		t.Fatal("fixture comparison values unexpectedly equal")
	}

	cp := copyDecision(d)
	if cp == nil || cp.PaymentActions[0].BaseOptionIndex == nil || cp.PaymentFallback == nil {
		t.Fatal("copyDecision omitted payment extension")
	}
	if cp.Options[0].Label != "Mountain: Mountain" {
		t.Errorf("projected option label = %q, want substituted label", cp.Options[0].Label)
	}
	if d.Options[0].Label != "Mountain: CARDNAME" {
		t.Errorf("projection changed original option label: %q", d.Options[0].Label)
	}

	*cp.PaymentActions[0].BaseOptionIndex = 8
	cp.PaymentActions[0].Plans[0].Activations[0].Source = 10
	cp.PaymentFallback.Reason = "projected"
	cp.Options[0].Label = "projected option"

	if *cp.PaymentActions[0].BaseOptionIndex != 8 || cp.PaymentActions[0].Plans[0].Activations[0].Source != 10 ||
		cp.PaymentFallback.Reason != "projected" || cp.Options[0].Label != "projected option" {
		t.Fatal("projected fields did not take their mutation values")
	}
	if *d.PaymentActions[0].BaseOptionIndex != 3 {
		t.Errorf("projected BaseOptionIndex mutation reached original: %d", *d.PaymentActions[0].BaseOptionIndex)
	}
	if got := d.PaymentActions[0].Plans[0].Activations[0].Source; got != 9 {
		t.Errorf("projected activation mutation reached original: source = %d", got)
	}
	if got := d.PaymentFallback.Reason; got != "original" {
		t.Errorf("projected fallback mutation reached original: reason = %q", got)
	}
	if got := d.Options[0].Label; got != "Mountain: CARDNAME" {
		t.Errorf("projected option mutation reached original: label = %q", got)
	}
}

// TestCopyDecisionRejectsDerefedClone pins the value-clone shape of the
// projection choke point. bf2668175 made copyDecision use `cp := *d.Clone()`
// and fc7d924ad (this ticket) replaced it with `cp := d.CloneValue()` so the
// projected decision is built by value again. No allocation-count test can
// distinguish the two under the current toolchain: go1.26.3 inlines
// decision.(*Decision).Clone into copyDecision (go build -gcflags='-m'), the
// pointee of the inlined intermediate pointer is then stack-elided, and
// copyDecision measures the identical 9 allocations per run under both
// expressions on a payment-extension fixture -- which is also why the
// searchprobe allocation ceiling passes uncached with the old expression
// restored. This guard therefore rejects the historical source shape itself:
// a dereferenced Decision.Clone() call inside copyDecision.
func TestCopyDecisionRejectsDerefedClone(t *testing.T) {
	src, err := os.ReadFile("view.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "view.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	ast.Inspect(file, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok && fd.Name.Name == "copyDecision" {
			body = fd.Body
			return false
		}
		return true
	})
	if body == nil {
		t.Fatal("copyDecision not found in view.go; the projection choke point moved -- repoint this guard")
	}
	var derefedClone, cloneValue bool
	ast.Inspect(body, func(n ast.Node) bool {
		if star, ok := n.(*ast.StarExpr); ok {
			if call, ok := star.X.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Clone" {
					derefedClone = true
				}
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "CloneValue" {
				cloneValue = true
			}
		}
		return true
	})
	if !cloneValue {
		t.Fatal("copyDecision no longer clones through Decision.CloneValue; the projection copy changed shape")
	}
	if derefedClone {
		t.Fatal("copyDecision dereferences Decision.Clone() (`cp := *d.Clone()`), the shape bf2668175 introduced and fc7d924ad removed")
	}
}
