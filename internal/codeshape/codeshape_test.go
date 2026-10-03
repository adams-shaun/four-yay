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
	ReadRole
}

type ReadRole interface {
	A(int) int
	B()
	Nested
}

type Nested interface {
	B()
	C()
}

type Ctx struct {
	TriggerContext
	*Other
	Source, Target int
	Controller     int
}

type optionalHost interface{ Extra() }

func opt(h Host, x any) {
	_ = h.(optionalHost)
	_ = h.(interface{ Inline() })
	_ = h.(ReadRole) // a role: not counted
	switch x.(type) {
	}
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
		FuncsOver300:           2,
		EngineMethods:          4, // A, B, C and the subpackage's D; never the test file's T
		HostMethods:            4, // Game, A, B (declared twice, counted once), C
		HostEmbeds:             1,
		HostDirectMethods:      1,
		HostRoleMaxMethods:     3,
		HostOptionalAssertions: 2,
		CtxFields:              3,
		CtxEmbeds:              2,
		ResumePointFields:      4,
		StringParamReads:       3,
		StringParamKeys:        2,
		StringCaseLiterals:     3,
		ChangeZoneLeaks:        []string{},
		ChangeZoneAllLeaks:     []string{},
		AttachLeaks:            []string{},
		CharmLeaks:             []string{},
		PumpLeaks:              []string{},
		DrawLeaks:              []string{},
		ReplaceEffectLeaks:     []string{},
		Files:                  4,
		LongFuncs: []Func{
			{Name: "deep", File: "rules/sub/deep.go", Line: 6, Lines: 402},
			{Name: "long301", File: "effects/long.go", Line: 303, Lines: 301},
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("Measure =\n%+v\nwant\n%+v", m, want)
	}
}

func TestMeasureCountsContextLiterals(t *testing.T) {
	root := writeTree(t, map[string]string{
		"effects/registry.go": effectsSrc,
		// Bare names inside package effects: 3 Ctx (one pointer, two elided
		// slice elements), 1 SpecContext, 1 TriggerContext; never Other{}.
		"effects/use.go": `package effects

func g() {
	_ = &Ctx{Source: 1}
	_ = []Ctx{{}, {Source: 2}}
	_ = SpecContext{}
	_ = TriggerContext{X: 1}
	_ = Other{}
}
`,
		// The designated constructor files are exempt.
		"effects/ctx_new.go": "package effects\n\nfunc NewCtx() Ctx { return Ctx{} }\n",
		"rules/ctx_new.go": `package rules

import "github.com/adams-shaun/gorge/effects"

func k() effects.Ctx { return effects.Ctx{} }
`,
		// Selector forms through the plain and an aliased import: 2 Ctx
		// (one an elided map value), 1 SpecContext, 1 TriggerContext. A local
		// type named Ctx in rules is not effects.Ctx.
		"rules/engine.go": `package rules

import (
	"github.com/adams-shaun/gorge/effects"
	fx "github.com/adams-shaun/gorge/effects"
)

type Ctx struct{}
type resumePoint struct{ a int }

func h() {
	_ = effects.Ctx{Source: 1}
	_ = map[int]*fx.Ctx{1: {}}
	_ = fx.SpecContext{}
	_ = &effects.TriggerContext{}
	_ = Ctx{}
}
`,
		// Test files are never counted.
		"rules/engine_test.go": "package rules\n\nimport \"github.com/adams-shaun/gorge/effects\"\n\nvar _ = effects.Ctx{}\n",
	})
	m, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.CtxLiterals != 5 || m.SpecContextLiterals != 2 || m.TriggerContextLiterals != 2 {
		t.Errorf("context literals = Ctx %d, SpecContext %d, TriggerContext %d; want 5, 2, 2",
			m.CtxLiterals, m.SpecContextLiterals, m.TriggerContextLiterals)
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

// TestMeasureCountsChangeZoneParamLeaks pins the changeZoneParamLeaks census:
// every read form in a ChangeZone file, a ChangeZone-only key in any other
// file, never the compiler itself, never a write, never a shared key outside
// ChangeZone's files.
func TestMeasureCountsChangeZoneParamLeaks(t *testing.T) {
	root := writeTree(t, map[string]string{
		"effects/registry.go": effectsSrc,
		// The compiler reads everything; none of it counts.
		"effects/changezone_params.go": `package effects

func compile(sa *SA) { _ = sa.Params["Hidden"]; _ = sa.ParamStr(cards.PKOrigin) }
`,
		// A ChangeZone file: index read, PK read, HasParam and a Num key count
		// (4); the map write does not.
		"effects/zone_change.go": `package effects

func eff(sa *SA) {
	_ = sa.Params["Defined"]
	_ = sa.Param(cards.PKOrigin)
	_ = sa.HasParam(cards.PKDestination)
	_ = Num(h, c, sa, "ChangeNum", 1)
	sub.Params["Defined"] = "x"
}
`,
		// Elsewhere only a ChangeZone-only key counts (2), a shared key never.
		"effects/cardflow.go": `package effects

func dig(sa *SA) {
	_ = sa.Params["Hidden"]
	_ = sa.ParamStr(cards.PKDifferentNames)
	_ = sa.ParamStr(cards.PKOrigin)
}
`,
		"rules/engine.go": "package rules\n\ntype resumePoint struct{ a int }\n",
	})
	m, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"effects/cardflow.go:4 Hidden", "effects/cardflow.go:5 DifferentNames",
		"effects/zone_change.go:4 Defined", "effects/zone_change.go:5 Origin",
		"effects/zone_change.go:6 Destination", "effects/zone_change.go:7 ChangeNum",
	}
	if m.ChangeZoneParamLeaks != len(want) || !reflect.DeepEqual(m.ChangeZoneLeaks, want) {
		t.Errorf("ChangeZone leaks = %d %v, want %v", m.ChangeZoneParamLeaks, m.ChangeZoneLeaks, want)
	}
}
