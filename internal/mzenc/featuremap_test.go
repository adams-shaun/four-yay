package mzenc

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"
)

func TestFeatureMapMatchesJava(t *testing.T) {
	b, err := os.ReadFile("testdata/golden/featuremap.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Map) == 0 {
		t.Fatal("featuremap.json has no map; regenerate it with useFeatureMap:true")
	}

	e := NewEncoder(defaultTable)
	e.UseFeatureMap = true
	applyOps(t, e, g.Ops)

	want := map[int32][]string{}
	for k, v := range g.Map {
		i, err := strconv.ParseInt(k, 10, 32)
		if err != nil {
			t.Fatalf("bad idx key %q: %v", k, err)
		}
		want[int32(i)] = v
	}

	got := e.FeatureMap().Entries()
	if len(got) != len(want) {
		t.Fatalf("featuremap idx count got %d want %d", len(got), len(want))
	}
	for _, entry := range got {
		w, ok := want[entry.Idx]
		if !ok {
			t.Fatalf("featuremap: extra idx %d not in java map", entry.Idx)
		}
		if !slices.Equal(entry.Labels, w) {
			t.Fatalf("featuremap idx %d: labels got %v want %v", entry.Idx, entry.Labels, w)
		}
		delete(want, entry.Idx)
	}
	if len(want) != 0 {
		t.Fatalf("featuremap: missing idxs %v", want)
	}
}
