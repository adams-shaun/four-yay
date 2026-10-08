package mzenc

import (
	"fmt"
	"io"
	"sort"
)

// FeatureMap is the research/logging index → (namespace, name) table
// (Features.java addIndex, only when Features.useFeatureMap is set). It is not
// needed for id computation; it exists so a golden can be read by name.
type FeatureMap struct {
	m map[int32]map[string]struct{}
}

func NewFeatureMap() *FeatureMap { return &FeatureMap{m: map[int32]map[string]struct{}{}} }

// AddFeature records one (namespace, name) under idx. namespace is the parent
// namespace index, or -1 at the root, mirroring Features.addIndex.
func (fm *FeatureMap) AddFeature(name string, namespace int32, idx int32) {
	s, ok := fm.m[idx]
	if !ok {
		s = map[string]struct{}{}
		fm.m[idx] = s
	}
	s[fmt.Sprintf("%d/%s", namespace, name)] = struct{}{}
}

// Entries returns idx → sorted "namespace/name" list, sorted by idx.
func (fm *FeatureMap) Entries() []struct {
	Idx    int32
	Labels []string
} {
	idxs := make([]int32, 0, len(fm.m))
	for i := range fm.m {
		idxs = append(idxs, i)
	}
	sort.Slice(idxs, func(a, b int) bool { return idxs[a] < idxs[b] })
	out := make([]struct {
		Idx    int32
		Labels []string
	}, 0, len(idxs))
	for _, i := range idxs {
		labels := make([]string, 0, len(fm.m[i]))
		for l := range fm.m[i] {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		out = append(out, struct {
			Idx    int32
			Labels []string
		}{i, labels})
	}
	return out
}

// Print writes the table as "idx: [ns/name], [ns/name]\n" lines, sorted by idx.
func (fm *FeatureMap) Print(w io.Writer) {
	for _, e := range fm.Entries() {
		fmt.Fprintf(w, "%d:", e.Idx)
		for i, l := range e.Labels {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, " [%s]", l)
		}
		fmt.Fprintln(w)
	}
}
