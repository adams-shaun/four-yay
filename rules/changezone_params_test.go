package rules

import (
	"slices"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// TestChangeZoneKnownKeysMatchTheCensus holds effects' compile-time unread
// detection for api:ChangeZone (changeZoneKnownKeys, behind
// ChangeZoneParams.Unread and its loud Note) equal to the parameter census:
// the census's measured api:ChangeZone read set plus its ignored
// (presentation/AI-only) and structural keys. A key the engine starts reading
// must join the table (else the resolver Notes a parameter it honours), and a
// key it stops reading must leave it (else the Note goes silent on a real
// gap).
func TestChangeZoneKnownKeysMatchTheCensus(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	want := map[string]bool{}
	for k := range d.api["ChangeZone"] {
		want[k] = true
	}
	for k := range ignoredParamKeys {
		want[k] = true
	}
	for k := range structuralKeys["sa"] {
		want[k] = true
	}
	var wantList []string
	for k := range want {
		wantList = append(wantList, k)
	}
	sort.Strings(wantList)
	got := effects.ChangeZoneKnownKeys()
	if !slices.Equal(got, wantList) {
		var missing, extra []string
		for _, k := range wantList {
			if _, ok := slices.BinarySearch(got, k); !ok {
				missing = append(missing, k)
			}
		}
		for _, k := range got {
			if !want[k] {
				extra = append(extra, k)
			}
		}
		t.Fatalf("effects.changeZoneKnownKeys disagrees with the census: add %v, remove %v", missing, extra)
	}
}
