package mzenc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type oracleOp struct {
	Op           string `json:"op"`
	Name         string `json:"name"`
	Num          int    `json:"num"`
	CallParent   *bool  `json:"callParent"`
	PassToParent *bool  `json:"passToParent"`
}

type golden struct {
	Name    string     `json:"name"`
	Ops     []oracleOp `json:"ops"`
	Indices []int32    `json:"indices"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// replay mirrors FeaturesProbe: a fresh tree per vector, ops in file order.
func replay(t *testing.T, ops []oracleOp) map[int32]struct{} {
	t.Helper()
	e := NewEncoder(defaultTable)
	stack := []*Node{e.Root()}
	top := func() *Node { return stack[len(stack)-1] }
	for _, op := range ops {
		switch op.Op {
		case "feature":
			top().AddFeatureCall(op.Name, boolOr(op.CallParent, true))
		case "numeric":
			top().AddNumericFeature(op.Name, op.Num, boolOr(op.CallParent, true))
		case "push":
			stack = append(stack, top().SubFeatures(op.Name, boolOr(op.PassToParent, true)))
		case "pop":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		default:
			t.Fatalf("unknown op %q", op.Op)
		}
	}
	return e.IDs()
}

// TestHashOracle is the byte-identical gate: every golden vector's Java id set
// must equal the Go id set exactly.
func TestHashOracle(t *testing.T) {
	files, err := filepath.Glob("testdata/golden/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden vectors found: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var g golden
		if err := json.Unmarshal(b, &g); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		want := map[int32]struct{}{}
		for _, id := range g.Indices {
			want[id] = struct{}{}
		}
		got := replay(t, g.Ops)
		if len(got) != len(want) {
			t.Fatalf("%s: id-count got %d want %d", f, len(got), len(want))
		}
		for id := range want {
			if _, ok := got[id]; !ok {
				t.Fatalf("%s: missing java id %d", f, id)
			}
		}
		for id := range got {
			if _, ok := want[id]; !ok {
				t.Fatalf("%s: extra id %d not in java output", f, id)
			}
		}
	}
}

// TestOracleCardinalityFileReadable pins one vector's shape so a regression
// names the file rather than just a count.
func TestOracleCardinalityFileReadable(t *testing.T) {
	b, err := os.ReadFile("testdata/golden/cardinality.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Indices) != 3 {
		t.Fatalf("cardinality.json: want 3 ids (Card#1..#3), got %d", len(g.Indices))
	}
	if got := len(replay(t, g.Ops)); got != 3 {
		t.Fatalf("cardinality.json: Go replay want 3 ids, got %d", got)
	}
}
