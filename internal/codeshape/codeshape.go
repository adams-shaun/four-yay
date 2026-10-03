// Package codeshape measures the shape of the rules engine's code: how long
// its functions are, how wide its interfaces and context structs are, and how
// much of its behaviour is still keyed on string literals. It is the metric
// behind the W0 shrink-only ratchets of the rules-engine refactor spec
// (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 4) and behind the steward axis of scripts/reward_collect.py, which replaced
// the old file-size count: a long FUNCTION is a concern without a seam, while
// a long FILE says nothing about boundaries and rewarded size-only splits.
//
// It is stdlib only (go/ast, go/parser) and deterministic: every list it
// returns is sorted, and no map order reaches the output.
package codeshape

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LongFuncLines is the threshold above which a function counts as long: a
// FuncDecl spanning MORE than this many source lines (inclusive of its
// signature and closing brace).
const LongFuncLines = 300

// ScannedDirs are the repo-relative trees measured. Each is walked
// recursively (testdata and dot/underscore directories excluded), so a later
// move of code into a rules/* subpackage stays inside the census.
var ScannedDirs = []string{"rules", "effects"}

// Func is one function declaration over LongFuncLines.
type Func struct {
	Name  string `json:"name"`  // receiver-qualified: "(*Engine).payCast" or "effEffect"
	File  string `json:"file"`  // repo-relative, slash-separated
	Line  int    `json:"line"`  // line of the func keyword
	Lines int    `json:"lines"` // end line - start line + 1
}

// Metrics is one measurement. The JSON names are the contract
// scripts/reward_collect.py reads; extend, never rename.
type Metrics struct {
	// FuncsOver300 counts non-test FuncDecls in rules/ and effects/ spanning
	// more than LongFuncLines lines.
	FuncsOver300 int `json:"funcs_over_300"`
	// EngineMethods counts non-test methods whose receiver base type is
	// Engine, under rules/ (recursive).
	EngineMethods int `json:"engine_methods"`
	// HostMethods counts the methods declared in effects.Host; HostEmbeds the
	// interfaces it embeds (none today).
	HostMethods int `json:"host_methods"`
	HostEmbeds  int `json:"host_embeds"`
	// CtxFields counts the named fields of effects.Ctx (each name on a
	// multi-name line counts); CtxEmbeds its embedded fields.
	CtxFields int `json:"ctx_fields"`
	CtxEmbeds int `json:"ctx_embeds"`
	// ResumePointFields counts the fields of rules' resumePoint struct (named
	// fields per name, plus embeds).
	ResumePointFields int `json:"resume_point_fields"`
	// StringParamReads counts index expressions <x>Params["<literal>"] in
	// rules/ and effects/ non-test files (reads and writes alike; the
	// literal key is the debt). StringParamKeys is the distinct-key count.
	StringParamReads int `json:"string_param_reads"`
	StringParamKeys  int `json:"string_param_keys"`
	// StringCaseLiterals counts, in rules/ and effects/ non-test files, the
	// string literals appearing directly in a case clause's expression list:
	// each such literal counts once (`case "A", "B":` is 2). A literal
	// nested deeper inside a case expression (`case f("A"):`) or in the
	// clause body is NOT counted -- unlike the spec's grep figures, which
	// counted string literals on case lines.
	StringCaseLiterals int `json:"string_case_literals"`
	// Files is how many non-test .go files were parsed.
	Files int `json:"files"`
	// LongFuncs lists every function counted by FuncsOver300, longest first
	// (ties by file then line).
	LongFuncs []Func `json:"long_funcs"`
}

// Measure parses rules/ and effects/ under root (the module root) and returns
// the metrics. It fails if a measured type (effects.Host, effects.Ctx,
// rules' resumePoint) cannot be found, so a rename cannot silently zero a
// ratchet.
func Measure(root string) (Metrics, error) {
	var m Metrics
	keys := map[string]bool{}
	hostFound, ctxFound, rpFound := false, false, false
	for _, dir := range ScannedDirs {
		files, err := goFiles(root, dir)
		if err != nil {
			return m, err
		}
		for _, rel := range files {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
			if err != nil {
				return m, fmt.Errorf("codeshape: parse %s: %w", rel, err)
			}
			m.Files++
			inEffectsTop := dir == "effects" && strings.Count(rel, "/") == 1
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					start := fset.Position(d.Pos()).Line
					end := fset.Position(d.End()).Line
					if n := end - start + 1; n > LongFuncLines {
						m.LongFuncs = append(m.LongFuncs, Func{Name: QualName(d), File: rel, Line: start, Lines: n})
					}
					if dir == "rules" && receiverBase(d) == "Engine" {
						m.EngineMethods++
					}
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts := spec.(*ast.TypeSpec)
						switch {
						case inEffectsTop && ts.Name.Name == "Host":
							it, ok := ts.Type.(*ast.InterfaceType)
							if !ok {
								return m, fmt.Errorf("codeshape: %s: effects.Host is not an interface", rel)
							}
							hostFound = true
							for _, fld := range it.Methods.List {
								if _, isFunc := fld.Type.(*ast.FuncType); isFunc {
									m.HostMethods += len(fld.Names)
								} else {
									m.HostEmbeds++
								}
							}
						case inEffectsTop && ts.Name.Name == "Ctx":
							named, embeds, err := structFields(ts, rel)
							if err != nil {
								return m, err
							}
							ctxFound = true
							m.CtxFields, m.CtxEmbeds = named, embeds
						case dir == "rules" && ts.Name.Name == "resumePoint":
							named, embeds, err := structFields(ts, rel)
							if err != nil {
								return m, err
							}
							if rpFound {
								return m, fmt.Errorf("codeshape: %s: a second resumePoint struct", rel)
							}
							rpFound = true
							m.ResumePointFields = named + embeds
						}
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.IndexExpr:
					if lit, ok := x.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING && isParams(x.X) {
						m.StringParamReads++
						if k, err := strconv.Unquote(lit.Value); err == nil {
							keys[k] = true
						}
					}
				case *ast.CaseClause:
					for _, e := range x.List {
						if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							m.StringCaseLiterals++
						}
					}
				}
				return true
			})
		}
	}
	switch {
	case !hostFound:
		return m, fmt.Errorf("codeshape: no `type Host interface` in effects/; update codeshape if it moved")
	case !ctxFound:
		return m, fmt.Errorf("codeshape: no `type Ctx struct` in effects/; update codeshape if it moved")
	case !rpFound:
		return m, fmt.Errorf("codeshape: no `type resumePoint struct` under rules/; update codeshape if it moved or was retired")
	}
	m.StringParamKeys = len(keys)
	m.FuncsOver300 = len(m.LongFuncs)
	sort.Slice(m.LongFuncs, func(i, j int) bool {
		a, b := m.LongFuncs[i], m.LongFuncs[j]
		if a.Lines != b.Lines {
			return a.Lines > b.Lines
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	if m.LongFuncs == nil {
		m.LongFuncs = []Func{}
	}
	return m, nil
}

// goFiles returns the non-test .go files under root/dir, repo-relative and
// slash-separated, sorted.
func goFiles(root, dir string) ([]string, error) {
	var out []string
	base := filepath.Join(root, dir)
	if _, err := os.Stat(base); err != nil {
		return nil, fmt.Errorf("codeshape: %w (is %q the module root?)", err, root)
	}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != base && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

func structFields(ts *ast.TypeSpec, rel string) (named, embeds int, err error) {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return 0, 0, fmt.Errorf("codeshape: %s: %s is not a struct", rel, ts.Name.Name)
	}
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			embeds++
		} else {
			named += len(fld.Names)
		}
	}
	return named, embeds, nil
}

// isParams reports whether expr names a string-keyed parameter map: an
// identifier or selector whose name ends in Params (sa.Params,
// ce.RestrictParams, ce.ReplacementParams, ...).
func isParams(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return strings.HasSuffix(x.Name, "Params")
	case *ast.SelectorExpr:
		return strings.HasSuffix(x.Sel.Name, "Params")
	}
	return false
}

// receiverBase is the receiver's base type name ("Engine" for both
// `(e *Engine)` and `(e Engine)`), or "" for a free function.
func receiverBase(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr: // generic receiver T[P]
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// QualName renders a declaration the way a review names it: "(*Engine).Ask"
// for a pointer method, "(T).M" for a value method, bare for a function.
func QualName(fd *ast.FuncDecl) string {
	base := receiverBase(fd)
	if base == "" {
		return fd.Name.Name
	}
	if _, ptr := fd.Recv.List[0].Type.(*ast.StarExpr); ptr {
		return "(*" + base + ")." + fd.Name.Name
	}
	return "(" + base + ")." + fd.Name.Name
}
