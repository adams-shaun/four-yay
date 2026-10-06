package tsgen

import (
	"go/types"
	"os"
	"reflect"
	"strings"
	"testing"
)

const selfPkg = "github.com/adams-shaun/gorge/internal/tsgen"

// fixtureRoot type-checks fixture_types_test.go from source and returns its
// named type.
func fixtureRoot(t *testing.T, l *Loader, name string) types.Type {
	t.Helper()
	pkg, err := l.CheckFiles(selfPkg, "fixture_types_test.go")
	if err != nil {
		t.Fatal(err)
	}
	ty, err := LookupIn(pkg, name)
	if err != nil {
		t.Fatal(err)
	}
	return ty
}

func TestGenerateMatchesFixture(t *testing.T) {
	got, err := Generate(Options{
		Roots:  []types.Type{fixtureRoot(t, NewLoader(), "outer")},
		Unions: map[string][]string{"Kind": {"a", "b"}},
		Header: "// test header\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/fixture.ts")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("generated:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateRejectsTwoStructsWithOneName(t *testing.T) {
	anon := types.NewStruct([]*types.Var{types.NewField(0, nil, "X", types.Typ[types.Int], false)}, nil)
	_, err := Generate(Options{Roots: []types.Type{fixtureRoot(t, NewLoader(), "outer"), anon}})
	if err == nil {
		t.Fatal("anonymous struct accepted")
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	o := Options{Roots: []types.Type{fixtureRoot(t, NewLoader(), "outer")}, Unions: map[string][]string{"Z": {"z"}, "A": {"a"}}}
	a, _ := Generate(o)
	b, _ := Generate(o)
	if a != b {
		t.Fatal("two runs differ")
	}
	if strings.Index(a, "export type A") > strings.Index(a, "export type Z") {
		t.Fatal("unions are not emitted in sorted order")
	}
}

// TestLookupTagMatchesStructTagGet holds the hand-written tag parser to the
// reflect.StructTag.Get it replaced (tests may still use reflect).
func TestLookupTagMatchesStructTagGet(t *testing.T) {
	for _, tag := range []string{
		``, `json:"a"`, `json:"a,omitempty"`, `json:"-"`, `json:"-,"`, `xml:"x" json:"b"`,
		`json:",omitempty"`, `  json:"c"  `, `json:"d\"e"`, `json:bad`, `json:"unterminated`,
		`yaml:"y"`, `json:"f" json:"g"`, `j son:"h"`,
	} {
		want := reflect.StructTag(tag).Get("json")
		if got, _ := lookupTag(tag, "json"); got != want {
			t.Errorf("lookupTag(%q) = %q, StructTag.Get = %q", tag, got, want)
		}
	}
}
