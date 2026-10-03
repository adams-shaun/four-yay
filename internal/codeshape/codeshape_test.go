package codeshape

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func longFunc(name string, body int) string {
	return "func " + name + "() {\n" + strings.Repeat("\t_ = 0\n", body) + "}\n"
}

const effectsSrc = `package effects

type TriggerContext struct{ X int }
type Other struct{}

type Host interface {
	Game() int
	A(int) int
	B()
}

type Ctx struct {
	TriggerContext
	*Other
	Source, Target int
	Controller     int
}

func f(m map[string]string, s string) {
	_ = m["k"]
	var sa struct{ Params, RestrictParams map[string]string }
	_ = sa.Params["Defined"]
	_ = sa.Params["Defined"]
	_ = sa.RestrictParams["Valid"]
	_ = sa.Params[s]
	switch s {
	case "A", "B":
	case "C":
	}
	switch 1 {
	case 1:
	}
}
`

func TestMeasureSyntheticTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		"effects/registry.go": effectsSrc,
		"effects/long.go":     "package effects\n\n" + longFunc("long300", 298) + longFunc("long301", 299),
		"rules/engine.go": `package rules

type Engine struct{}
type resumePoint struct {
	kind, other string
	obj         int
	Embedded
}
type Embedded struct{}

func (e *Engine) A() {}
func (x *Engine) B() {}
func (e Engine) C()  {}
func free()          {}
`,
		"rules/sub/deep.go":           "package sub\n\ntype Engine struct{}\n\nfunc (e *Engine) D() {}\n" + longFunc("deep", 400),
		"rules/engine_test.go":        "package rules\n\nfunc (e *Engine) T() {}\n" + longFunc("testlong", 500),
		"rules/testdata/x/fixture.go": "package x\n\n" + longFunc("fixture", 500),
	})
	m, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	want := Metrics{
		FuncsOver300:       2,
		EngineMethods:      4, // A, B, C and the subpackage's D; never the test file's T
		HostMethods:        3,
		CtxFields:          3,
		CtxEmbeds:          2,
		ResumePointFields:  4,
		StringParamReads:   3,
		StringParamKeys:    2,
		StringCaseLiterals: 3,
		Files:              4,
		LongFuncs: []Func{
			{Name: "deep", File: "rules/sub/deep.go", Line: 6, Lines: 402},
			{Name: "long301", File: "effects/long.go", Line: 303, Lines: 301},
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("Measure =\n%+v\nwant\n%+v", m, want)
	}
}

func TestMeasureFailsWhenAMeasuredTypeIsMissing(t *testing.T) {
	root := writeTree(t, map[string]string{
		"effects/registry.go": effectsSrc,
		"rules/engine.go":     "package rules\n",
	})
	if _, err := Measure(root); err == nil || !strings.Contains(err.Error(), "resumePoint") {
		t.Fatalf("Measure without resumePoint: err = %v, want a resumePoint error", err)
	}
}

func TestMeasureIsDeterministic(t *testing.T) {
	a := measureRepo(t)
	b := measureRepo(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two measurements of the same tree differ")
	}
}
