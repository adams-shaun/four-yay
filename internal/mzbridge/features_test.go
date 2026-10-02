package mzbridge

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestFeatureTreeMatchesJava replays the op script that was run against the
// unmodified Features.java and compares every emission (id, namespace, key)
// in order, and the distinct sorted id set at each checkpoint.
func TestFeatureTreeMatchesJava(t *testing.T) {
	rows := goldenRows(t, "feature_tree.txt")
	type emit struct {
		id, ns int32
		key    string
	}
	var (
		set      *FeatureSet
		nodes    map[string]*Node
		got      []emit
		scenario string
		emits    int
		checks   int
	)
	// the trace is "op" lines split on spaces and "emit" lines split on tabs
	for i := 0; i < len(rows); i++ {
		if rows[i][0] == "emit" {
			t.Fatalf("row %d: emit with no op before it", i)
		}
		p := strings.Split(rows[i][0], " ")
		got = got[:0]
		switch p[0] {
		case "scenario":
			scenario = p[1]
			set = NewFeatureSet()
			set.SetDebug(true)
			set.onEmit = func(id int32, n *Node, key string) { got = append(got, emit{id, n.Namespace(), key}) }
			nodes = map[string]*Node{"0": set.Root()}
		case "sub":
			nodes[p[1]] = nodes[p[2]].Sub(unhex(t, p[4]), p[3] == "1")
		case "add":
			nodes[p[1]].AddFeature(unhex(t, p[3]), p[2] == "1")
		case "num":
			nodes[p[1]].AddNumericFeature(unhex(t, p[4]), atoi(t, p[3]), p[2] == "1")
		case "refresh":
			set.Refresh()
		case "set":
			i++
			q := strings.Split(rows[i][0], " ")
			if q[0] != "ids" {
				t.Fatalf("row %d: want ids after set, got %q", i, q[0])
			}
			want := []int32{}
			for _, s := range q[1:] {
				want = append(want, int32(atoi(t, s)))
			}
			if ids := set.IDs(); !slices.Equal(append([]int32{}, ids...), want) {
				t.Errorf("%s: id set = %v, Java %v", scenario, ids, want)
			}
			checks++
			continue
		default:
			t.Fatalf("row %d: unknown op %q", i, p[0])
		}
		var want []emit
		for i+1 < len(rows) && rows[i+1][0] == "emit" {
			i++
			want = append(want, emit{int32(atoi(t, rows[i][1])), int32(atoi(t, rows[i][2])), unhex(t, rows[i][3])})
		}
		emits += len(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: op %v emitted\n got %v\nJava %v", scenario, p, got, want)
		}
	}
	if emits < 300 || checks < 6 {
		t.Fatalf("replayed only %d emissions and %d set checks", emits, checks)
	}
}

func TestCleanStringMatchesJava(t *testing.T) {
	rows := goldenRows(t, "clean_vectors.tsv")
	if len(rows) < 25 {
		t.Fatalf("only %d vectors", len(rows))
	}
	for _, r := range rows {
		in, want := unhex(t, r[0]), unhex(t, r[1])
		if got := CleanString(in); got != want {
			t.Errorf("CleanString(%q) = %q, Java %q", in, got, want)
		}
	}
}

// TestFeaturePooling states the tree's rules in the open, independent of
// the golden trace: occurrence numbering, pooling to every ancestor, a
// sealed namespace, and de-duplication.
func TestFeaturePooling(t *testing.T) {
	s := NewFeatureSet()
	s.SetDebug(true)
	var keys []string
	s.onEmit = func(_ int32, n *Node, key string) { keys = append(keys, n.Path()+"/"+key) }

	player := s.Root().Sub("Player", true)
	bf := player.Sub("Battlefield", true)
	bf.Sub("Forest", true).Add("Land")
	bf.Sub("Forest", true).Add("Land")
	stack := s.Root().Sub("Stack", false)
	stack.Add("Depth")

	want := []string{
		"root/Player#1",
		"root/Battlefield#1", "root/Player#1/Battlefield#1",
		"root/Forest#1", "root/Player#1/Forest#1", "root/Player#1/Battlefield#1/Forest#1",
		"root/Land#1", "root/Player#1/Land#1", "root/Player#1/Battlefield#1/Land#1", "root/Player#1/Battlefield#1/Forest#1/Land#1",
		"root/Forest#2", "root/Player#1/Forest#2", "root/Player#1/Battlefield#1/Forest#2",
		"root/Land#2", "root/Player#1/Land#2", "root/Player#1/Battlefield#1/Land#2", "root/Player#1/Battlefield#1/Forest#2/Land#1",
		"root/Stack#1",
		"root/Stack#1/Depth#1", // sealed: nothing reaches the root
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("emissions:\n got %q\nwant %q", keys, want)
	}

	ids := s.IDs()
	if !slices.IsSorted(ids) || len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Fatalf("IDs not sorted and distinct: %v", ids)
	}
	if len(ids) != len(want) {
		t.Fatalf("%d distinct ids for %d distinct paths", len(ids), len(want))
	}
	// every id maps back to the path that produced it
	for _, id := range ids {
		if len(s.Names(id)) != 1 {
			t.Fatalf("id %d has names %q", id, s.Names(id))
		}
	}
	id := FeatureID("Land#1", Hash64("Forest#2", Hash64("Battlefield#1", Hash64("Player#1", GlobalSeed))))
	if got := s.Names(id); len(got) != 1 || got[0] != "root/Player#1/Battlefield#1/Forest#2/Land#1" {
		t.Fatalf("Names(%d) = %q", id, got)
	}
	if d := s.Describe(); len(d) != len(want) || !strings.HasPrefix(d[0], strconv.Itoa(int(ids[0]))+"\t") {
		t.Fatalf("Describe = %q", d)
	}

	// the same state encoded again after Refresh gives the same ids, and a
	// repeated emission is stored once
	first := slices.Clone(ids)
	s.Refresh()
	if len(s.IDs()) != 0 || len(s.Names(id)) != 0 {
		t.Fatal("Refresh left ids or names behind")
	}
	player = s.Root().Sub("Player", true)
	bf = player.Sub("Battlefield", true)
	bf.Sub("Forest", true).Add("Land")
	bf.Sub("Forest", true).Add("Land")
	s.Root().Sub("Stack", true).Add("Depth") // existing node keeps passToParent=false
	if !slices.Equal(s.IDs(), first) {
		t.Fatalf("re-encoding changed the ids:\n%v\n%v", s.IDs(), first)
	}
}

func TestNumericThermometer(t *testing.T) {
	cases := []struct {
		num  int
		want []string
	}{
		{0, nil},
		{-5, nil},
		{1, []string{"N@0"}},
		{3, []string{"N@0", "N@1", "N@2"}},
		{33, append([]string{"N@32"}, unary(20)...)},
		{600, append([]string{"N@32", "N@64", "N@128", "N@256", "N@512"}, unary(20)...)},
	}
	for _, c := range cases {
		s := NewFeatureSet()
		var keys []string
		s.onEmit = func(_ int32, _ *Node, key string) { keys = append(keys, strings.TrimSuffix(key, "#1")) }
		s.Root().AddNumeric("N", c.num)
		if !slices.Equal(keys, c.want) {
			t.Errorf("AddNumeric(%d) = %q, want %q", c.num, keys, c.want)
		}
	}
}

func unary(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("N@%d", i))
	}
	return out
}
