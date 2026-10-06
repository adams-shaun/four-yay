// Command schemagen writes protocol/manabrew/schema_gen.go: the typed JSON
// schema graph manabrew.Decode walks to report unknown fields, plus the
// target and union-member type switches, all read from the package SOURCE
// with go/types so the codec needs no runtime reflection.
//
// Regenerate with `go generate ./protocol/manabrew` (or
// `go run ./protocol/manabrew/internal/schemagen` from the repo root);
// TestSchemaGenIsFresh fails when the committed file is stale.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const genFile = "schema_gen.go"

func main() {
	dir := flag.String("dir", "protocol/manabrew", "package directory")
	flag.Parse()
	src, err := generate(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*dir, genFile), src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

// generate type-checks the package's non-test sources (minus the generated
// file itself, so a stale or missing one never blocks regeneration) and
// renders schema_gen.go.
func generate(dir string) ([]byte, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || n == genFile {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	// Errors are tolerated: the hand-written codec references the generated
	// symbols this run is about to (re)write.
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(error) {}}
	pkg, _ := conf.Check("github.com/adams-shaun/gorge/protocol/manabrew", fset, files, nil)
	if pkg == nil {
		return nil, fmt.Errorf("type-check of %s produced no package", dir)
	}
	g := &gen{pkg: pkg, vars: map[*types.Named]string{}}
	return g.render()
}

type gen struct {
	pkg   *types.Package
	vars  map[*types.Named]string // struct -> schema var name
	order []*types.Named          // structs in first-reference order
}

// localStructs lists the package's own named struct types, sorted by name.
func (g *gen) localStructs() []*types.Named {
	var out []*types.Named
	scope := g.pkg.Scope()
	for _, name := range scope.Names() { // sorted
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		n, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		if _, ok := n.Underlying().(*types.Struct); ok {
			out = append(out, n)
		}
	}
	return out
}

func (g *gen) structVar(n *types.Named) string {
	if v, ok := g.vars[n]; ok {
		return v
	}
	v := "schema" + n.Obj().Name()
	if n.Obj().Pkg() != g.pkg {
		v = "schema_" + n.Obj().Pkg().Name() + "_" + n.Obj().Name()
	}
	g.vars[n] = v
	g.order = append(g.order, n)
	return v
}

// expr renders the schema expression for t, "nil" for a leaf (anything the
// walker never descends into: basics, interfaces, and containers of leaves).
func (g *gen) expr(t types.Type) string {
	t = types.Unalias(t)
	for {
		p, ok := t.Underlying().(*types.Pointer)
		if !ok {
			break
		}
		t = types.Unalias(p.Elem())
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		if n, ok := t.(*types.Named); ok {
			return g.structVar(n)
		}
		return "&schema{kind: schemaStruct, fields: " + g.fieldsExpr(u) + "}"
	case *types.Slice:
		return g.container("schemaList", u.Elem())
	case *types.Array:
		return g.container("schemaList", u.Elem())
	case *types.Map:
		return g.container("schemaMap", u.Elem())
	}
	return "nil"
}

func (g *gen) container(kind string, elem types.Type) string {
	e := g.expr(elem)
	if e == "nil" {
		return "nil" // walking leaf elements reports nothing
	}
	return "&schema{kind: " + kind + ", elem: " + e + "}"
}

// fields mirrors encoding/json's key set the way the reflective walker did:
// exported fields by json tag name (or Go name), "-" skipped, and an
// anonymous embedded struct with no tag name flattened (later keys win).
func (g *gen) fields(st *types.Struct, out map[string]types.Type) {
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Exported() {
			continue
		}
		tv, _ := lookupTag(st.Tag(i), "json")
		name := strings.Split(tv, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			if f.Embedded() {
				ft := types.Unalias(f.Type())
				for {
					p, ok := ft.Underlying().(*types.Pointer)
					if !ok {
						break
					}
					ft = types.Unalias(p.Elem())
				}
				if est, ok := ft.Underlying().(*types.Struct); ok {
					g.fields(est, out)
				}
			}
			continue
		}
		out[name] = f.Type()
	}
}

func (g *gen) fieldsExpr(st *types.Struct) string {
	m := map[string]types.Type{}
	g.fields(st, m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("map[string]*schema{")
	for _, k := range keys {
		fmt.Fprintf(&b, "\n%s: %s,", strconv.Quote(k), g.expr(m[k]))
	}
	b.WriteString("\n}")
	return b.String()
}

func (g *gen) render() ([]byte, error) {
	locals := g.localStructs()
	for _, n := range locals {
		g.structVar(n)
	}
	var body strings.Builder
	// Fields are filled in init so the graph may be cyclic.
	body.WriteString("func init() {\n")
	for i := 0; i < len(g.order); i++ { // g.order grows as fields reference foreign structs
		n := g.order[i]
		fmt.Fprintf(&body, "%s.fields = %s\n", g.vars[n], g.fieldsExpr(n.Underlying().(*types.Struct)))
	}
	body.WriteString("}\n\n")

	var b bytes.Buffer
	b.WriteString("// Code generated by protocol/manabrew/internal/schemagen; DO NOT EDIT.\n\n")
	b.WriteString("package manabrew\n\n")
	b.WriteString("var (\n")
	for _, n := range g.order {
		fmt.Fprintf(&b, "%s = &schema{kind: schemaStruct}\n", g.vars[n])
	}
	b.WriteString(")\n\n")
	b.WriteString(body.String())

	b.WriteString("// schemaOfTarget returns the schema of a Decode target: a non-nil pointer\n")
	b.WriteString("// to one of this package's structs. ok is false for a nil pointer or any\n// other type.\n")
	b.WriteString("func schemaOfTarget(v any) (s *schema, ok bool) {\nswitch x := v.(type) {\n")
	for _, n := range locals {
		fmt.Fprintf(&b, "case *%s:\nreturn %s, x != nil\n", n.Obj().Name(), g.vars[n])
	}
	b.WriteString("}\nreturn nil, false\n}\n")

	for _, iface := range []string{"PromptInputData", "PromptOutputValue"} {
		obj, ok := g.pkg.Scope().Lookup(iface).(*types.TypeName)
		if !ok {
			return nil, fmt.Errorf("no interface %s", iface)
		}
		it, ok := obj.Type().Underlying().(*types.Interface)
		if !ok {
			return nil, fmt.Errorf("%s is not an interface", iface)
		}
		fmt.Fprintf(&b, "\n// deref%s turns a pointer to a %s member (what the\n// case tables allocate so json.Unmarshal has an addressable target) back into\n// the value every consumer type-switches on. Anything else is returned as is.\n", iface, iface)
		fmt.Fprintf(&b, "func deref%s(v %s) %s {\nswitch x := v.(type) {\n", iface, iface, iface)
		for _, n := range locals {
			if types.Implements(n, it) && types.Implements(types.NewPointer(n), it) {
				fmt.Fprintf(&b, "case *%s:\nif x != nil {\nreturn *x\n}\n", n.Obj().Name())
			}
		}
		b.WriteString("}\nreturn v\n}\n")
	}
	return format.Source(b.Bytes())
}

// lookupTag is the struct-tag convention parser (what reflect.StructTag.Get
// implements): space-separated key:"quoted value" pairs.
func lookupTag(tag, key string) (string, bool) {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := tag[:i]
		tag = tag[i+1:]
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		qvalue := tag[:i+1]
		tag = tag[i+1:]
		if key == name {
			value, err := strconv.Unquote(qvalue)
			if err != nil {
				break
			}
			return value, true
		}
	}
	return "", false
}
