package codeshape

import (
	"reflect"
	"strings"
	"testing"
)

// The W4 step 3 typed-param leak ratchets (TypedParamAPIs): each constant is
// the measured number of the API's parameter reads outside its compiler. Like
// every ratchet in ratchet_test.go it only falls; each landed at zero.
const (
	// charmParamLeaks: the modal family's reads outside
	// effects/charm_params.go -- any read in effects/charm.go, plus any read
	// of a Charm-only key elsewhere.
	charmParamLeaks = 0
)

func emptyTypedParamLeaks() map[string][]string {
	m := map[string][]string{}
	for _, api := range TypedParamAPIs {
		m[api.Name] = []string{}
	}
	return m
}

func TestTypedParamLeaksOnlyShrink(t *testing.T) {
	m := measureRepo(t)
	limits := map[string]int{"Charm": charmParamLeaks}
	var rs []ratchet
	for _, api := range TypedParamAPIs {
		limit, ok := limits[api.Name]
		if !ok {
			t.Fatalf("TypedParamAPIs names %s but typedparams_test.go has no ratchet constant for it", api.Name)
		}
		leaks := m.TypedParamLeaks[api.Name]
		rs = append(rs, ratchet{strings.ToLower(api.Name[:1]) + api.Name[1:] + "ParamLeaks", len(leaks), limit,
			"Read the parameter through effects." + api.Name + "Of's compiled struct (add a field to " +
				"its compiler, " + api.CompilerFile + ") instead of reading the ability's Params in " +
				strings.Join(api.Files, ", ") + " or an " + api.Name + "-only key elsewhere. Leaks: " +
				strings.Join(leaks, ", ")})
	}
	checkRatchets(t, rs)
}

// TestMeasureCountsTypedParamLeaks pins the census: every read form in an
// API's own file, an API-only key in any other file, never the compiler
// itself, never a write, never a shared key outside the API's files.
func TestMeasureCountsTypedParamLeaks(t *testing.T) {
	root := writeTree(t, map[string]string{
		"effects/registry.go":     effectsSrc,
		"effects/charm_params.go": "package effects\n\nfunc compile(sa *SA) { _ = sa.Params[\"CharmNum\"]; _ = sa.ParamStr(cards.PKChoices) }\n",
		"effects/charm.go": `package effects

func eff(sa *SA) {
	_ = sa.Params["Defined"]
	_ = sa.Param(cards.PKChoices)
	_ = Num(h, c, sa, "CharmNum", 1)
	sub.Params["Defined"] = "x"
}
`,
		"rules/engine.go": "package rules\n\ntype resumePoint struct{ a int }\n\nfunc f(sa *SA) { _ = sa.Params[\"TempRemember\"]; _ = sa.ParamStr(cards.PKChoices) }\n",
	})
	m, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"effects/charm.go:4 Defined", "effects/charm.go:5 Choices", "effects/charm.go:6 CharmNum",
		"rules/engine.go:5 TempRemember",
	}
	if got := m.TypedParamLeaks["Charm"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Charm leaks = %v, want %v", got, want)
	}
}
