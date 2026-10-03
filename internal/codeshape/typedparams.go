package codeshape

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// TypedParamAPI names one per-API parameter compiler of W4 step 3 (the
// rules-engine refactor spec, section 8) for the typed-param leak census:
// CompilerFile is the one file in rules/ and effects/ allowed to read the
// API's parameters, Files are the API's own resolution files (every
// parameter read there goes through the compiled struct, so they carry none),
// and OnlyKeys are the keys only that compiler reads (a read of one anywhere
// else is a sibling path interpreting the API's parameter on its own).
// ChangeZone's census predates this table (ChangeZoneParamLeaks).
type TypedParamAPI struct {
	Name         string
	CompilerFile string
	Files        []string
	OnlyKeys     []string
}

// TypedParamAPIs is the census table, one entry per compiled API.
var TypedParamAPIs = []TypedParamAPI{
	{
		Name:         "Charm",
		CompilerFile: "effects/charm_params.go",
		Files:        []string{"effects/charm.go"},
		OnlyKeys: []string{"CanRepeatModes", "CharmNum", "ChoiceRestriction", "FallbackAbility",
			"MinCharmNum", "RandomCompare", "RandomCompareSVar", "TempRemember"},
	},
}

// countTypedParamLeaks appends f's reads that leak past each TypedParamAPIs
// compiler to m.TypedParamLeaks[api] ("file:line key"). A read is what
// countChangeZoneLeaks counts: an index expression <x>Params["k"] that is not
// an assignment target, a Param/ParamStr/HasParam(cards.PK<k>) call, or a
// Num/NumResolved/NumResolvedStrict/NumForObject call with a literal key.
func countTypedParamLeaks(fset *token.FileSet, f *ast.File, rel string, m *Metrics) {
	if m.TypedParamLeaks == nil {
		m.TypedParamLeaks = map[string][]string{}
	}
	reads := paramReads(fset, f)
	for _, api := range TypedParamAPIs {
		if _, ok := m.TypedParamLeaks[api.Name]; !ok {
			m.TypedParamLeaks[api.Name] = []string{}
		}
		if rel == api.CompilerFile {
			continue
		}
		own := false
		for _, file := range api.Files {
			own = own || rel == file
		}
		for _, r := range reads {
			only := false
			for _, k := range api.OnlyKeys {
				only = only || k == r.key
			}
			if own || only {
				m.TypedParamLeaks[api.Name] = append(m.TypedParamLeaks[api.Name], fmt.Sprintf("%s:%d %s", rel, r.line, r.key))
			}
		}
	}
}

// sortTypedParamLeaks sorts every API's leak list (Measure's final pass).
func sortTypedParamLeaks(m *Metrics) {
	if m.TypedParamLeaks == nil {
		m.TypedParamLeaks = map[string][]string{}
	}
	for _, api := range TypedParamAPIs {
		if m.TypedParamLeaks[api.Name] == nil {
			m.TypedParamLeaks[api.Name] = []string{}
		}
		sort.Strings(m.TypedParamLeaks[api.Name])
	}
}

type paramRead struct {
	line int
	key  string
}

// paramReads lists f's parameter reads in source order.
func paramReads(fset *token.FileSet, f *ast.File) []paramRead {
	writes := map[ast.Node]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, l := range as.Lhs {
				writes[l] = true
			}
		}
		return true
	})
	var out []paramRead
	add := func(pos token.Pos, key string) {
		out = append(out, paramRead{line: fset.Position(pos).Line, key: key})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IndexExpr:
			if lit, ok := x.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING && isParams(x.X) && !writes[x] {
				if k, err := strconv.Unquote(lit.Value); err == nil {
					add(x.Pos(), k)
				}
			}
		case *ast.CallExpr:
			name := ""
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				name = sel.Sel.Name
			} else if id, ok := x.Fun.(*ast.Ident); ok {
				name = id.Name
			}
			switch name {
			case "Param", "ParamStr", "HasParam":
				if len(x.Args) == 1 {
					if ks, ok := x.Args[0].(*ast.SelectorExpr); ok && strings.HasPrefix(ks.Sel.Name, "PK") {
						add(x.Pos(), strings.TrimPrefix(ks.Sel.Name, "PK"))
					}
				}
			case "Num", "NumResolved", "NumResolvedStrict", "NumForObject":
				if len(x.Args) >= 4 {
					if lit, ok := x.Args[3].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if k, err := strconv.Unquote(lit.Value); err == nil {
							add(x.Pos(), k)
						}
					}
				}
			}
		}
		return true
	})
	return out
}
