package codeshape

import (
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The side-door censuses: each measures a way around a syntactic ratchet
// above it. EngineMethods counts *Engine methods, so a free function taking
// *Engine, or a method on a struct that carries one, is an Engine method by
// another name (EngineSurface). StringCaseLiterals counts `case "X":`, so an
// if-chain of `s == "X"` is the same dispatch (StringLiteralCompares).
// StringParamReads counts Params["X"], so a helper taking the key as a string
// is the same read (StringKeyedParamReads). The StrCodes tables replaced the
// switches, so the same word named in a second table is a vocabulary split
// (StrCodesTables, StrCodesKeyDup).

// parsedFile is one non-test file of the census, kept for the cross-file
// passes that need every declaration first.
type parsedFile struct {
	fset *token.FileSet
	f    *ast.File
	rel  string // repo-relative, slash-separated
	dir  string // the ScannedDirs root it was found under
}

// strTableCtors are the cards (re-exported by state) constructors of the
// string vocabulary tables, cards/strtab.go.
var strTableCtors = map[string]bool{"NewStrCodes": true, "NewNameSet": true, "NewStrTable": true}

// measureSideDoors fills the side-door metrics from every parsed file.
func measureSideDoors(files []parsedFile, m *Metrics) {
	measureEngineSurface(files, m)
	helpers := stringKeyHelpers(files)
	m.StringKeyedParamHelpers = make([]string, 0, len(helpers))
	for key, mask := range helpers {
		var pos []string
		for i := 0; i < 64; i++ {
			if mask&(1<<i) != 0 {
				pos = append(pos, strconv.Itoa(i))
			}
		}
		m.StringKeyedParamHelpers = append(m.StringKeyedParamHelpers, key+" key args "+strings.Join(pos, ","))
	}
	sort.Strings(m.StringKeyedParamHelpers)
	keyTables := map[string]int{}
	for _, pf := range files {
		ast.Inspect(pf.f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BinaryExpr:
				if (x.Op == token.EQL || x.Op == token.NEQ) && (nonEmptyStringLit(x.X) || nonEmptyStringLit(x.Y)) {
					m.StringLiteralCompares++
				}
			case *ast.CallExpr:
				name := callName(x.Fun)
				if isStringsEqualFold(x.Fun) {
					hasLit, hasParam := false, false
					for _, a := range x.Args {
						if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							hasLit = true
						}
						if containsParamCall(a) {
							hasParam = true
						}
					}
					if hasLit {
						m.StringLiteralCompares++
					}
					if hasParam {
						m.RawBoolParamParses++
					}
				}
				if hm := helpers[helperKey(name, len(x.Args))]; hm != 0 {
					for i, a := range x.Args {
						if lit, isLit := a.(*ast.BasicLit); isLit && lit.Kind == token.STRING && i < 64 && hm&(1<<i) != 0 {
							m.StringKeyedParamReads++
						}
					}
				}
				if strTableCtors[name] {
					m.StrCodesTables++
					for _, k := range tableKeys(x) {
						keyTables[k]++
					}
				}
			}
			return true
		})
	}
	for _, n := range keyTables {
		m.StrCodesKeyDup += n - 1
	}
}

// measureEngineSurface counts, under rules/ (recursive), the *Engine methods,
// the free functions with a *Engine parameter, and the methods of struct
// types holding a *Engine field (a method two of these describe counts once).
func measureEngineSurface(files []parsedFile, m *Metrics) {
	holders := map[string]bool{} // "<pkg dir> <type>"
	for _, pf := range files {
		if pf.dir != "rules" {
			continue
		}
		for _, decl := range pf.f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || ts.Name.Name == "Engine" {
					continue
				}
				for _, fld := range st.Fields.List {
					if isEnginePtr(fld.Type) {
						holders[path.Dir(pf.rel)+" "+ts.Name.Name] = true
					}
				}
			}
		}
	}
	for _, pf := range files {
		if pf.dir != "rules" {
			continue
		}
		for _, decl := range pf.f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			base := receiverBase(fd)
			switch {
			case base == "Engine":
				m.EngineSurface++
			case base != "":
				if holders[path.Dir(pf.rel)+" "+base] {
					m.EngineSurface++
				}
			default:
				for _, p := range fd.Type.Params.List {
					if isEnginePtr(p.Type) {
						m.EngineSurface++
						break
					}
				}
			}
		}
	}
}

// isEnginePtr reports whether t is *Engine or *rules.Engine.
func isEnginePtr(t ast.Expr) bool {
	s, ok := t.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch x := s.X.(type) {
	case *ast.Ident:
		return x.Name == "Engine"
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == "rules" && x.Sel.Name == "Engine"
	}
	return false
}

// helperKey names a string-keyed helper by bare name and arity, so two
// same-named functions of different shapes do not alias.
func helperKey(name string, arity int) string { return name + "/" + strconv.Itoa(arity) }

// stringKeyHelpers finds, automatically, the functions whose string
// parameters are used as a string-keyed parameter-map index (<x>Params[key],
// as in effects.Num's `sa.Params[key]`), and closes the set over helpers that
// pass their own string parameter on at another helper's key position
// (NumResolvedStrict -> NumResolved). The result maps helperKey to a bitmask
// of key-parameter positions.
func stringKeyHelpers(files []parsedFile) map[string]uint64 {
	type cand struct {
		key    string
		body   *ast.BlockStmt
		params map[string]int // string parameter name -> position
	}
	var cands []cand
	for _, pf := range files {
		for _, decl := range pf.f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ps := map[string]int{}
			n := 0
			for _, fld := range fd.Type.Params.List {
				id, isStr := fld.Type.(*ast.Ident)
				isStr = isStr && id.Name == "string"
				if len(fld.Names) == 0 {
					n++
					continue
				}
				for _, nm := range fld.Names {
					if isStr && nm.Name != "_" && n < 64 {
						ps[nm.Name] = n
					}
					n++
				}
			}
			if len(ps) > 0 {
				cands = append(cands, cand{helperKey(fd.Name.Name, n), fd.Body, ps})
			}
		}
	}
	helpers := map[string]uint64{}
	for changed := true; changed; {
		changed = false
		for _, c := range cands {
			var mask uint64
			ast.Inspect(c.body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.IndexExpr:
					if id, ok := x.Index.(*ast.Ident); ok && isParams(x.X) {
						if i, ok := c.params[id.Name]; ok {
							mask |= 1 << i
						}
					}
				case *ast.CallExpr:
					hm := helpers[helperKey(callName(x.Fun), len(x.Args))]
					for i, a := range x.Args {
						if hm&(1<<i) == 0 {
							continue
						}
						if id, ok := a.(*ast.Ident); ok {
							if j, ok := c.params[id.Name]; ok {
								mask |= 1 << j
							}
						}
					}
				}
				return true
			})
			if mask != 0 && helpers[c.key]|mask != helpers[c.key] {
				helpers[c.key] |= mask
				changed = true
			}
		}
	}
	return helpers
}

// tableKeys returns the string-literal keys a table constructor call names:
// every literal argument of NewNameSet, and the Key of every StrEntry row
// (`{Key: "x", Val: v}` or positional `{"x", v}`) of NewStrCodes/NewStrTable.
func tableKeys(call *ast.CallExpr) []string {
	var out []string
	seen := map[string]bool{}
	addLit := func(e ast.Expr) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		if k, err := strconv.Unquote(lit.Value); err == nil && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, a := range call.Args {
		cl, ok := a.(*ast.CompositeLit)
		if !ok {
			addLit(a)
			continue
		}
		for i, el := range cl.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Key" {
					addLit(kv.Value)
				}
			} else if i == 0 {
				addLit(el)
			}
		}
	}
	return out
}

// callName is a call's bare function name: "Num" for Num(...), effects.Num(...)
// and x.Num(...), looking through generic instantiation.
func callName(fun ast.Expr) string {
	switch x := fun.(type) {
	case *ast.IndexExpr:
		return callName(x.X)
	case *ast.IndexListExpr:
		return callName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

func isStringsEqualFold(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "EqualFold" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "strings"
}

func nonEmptyStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s != ""
}

// containsParamCall reports whether e contains a Param or ParamStr call.
func containsParamCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if name := callName(c.Fun); name == "Param" || name == "ParamStr" {
				found = true
			}
		}
		return !found
	})
	return found
}
