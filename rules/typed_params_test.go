package rules

import (
	"slices"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// censusKnownKeys is the parameter census's measured read set for apis (their
// union) plus its ignored (presentation/AI-only) and structural keys, sorted:
// what a typed parameter compiler's known-key table must equal.
func censusKnownKeys(d *derivedReads, apis ...string) []string {
	want := map[string]bool{}
	for _, api := range apis {
		for k := range d.api[api] {
			want[k] = true
		}
	}
	for k := range ignoredParamKeys {
		want[k] = true
	}
	for k := range structuralKeys["sa"] {
		want[k] = true
	}
	var out []string
	for k := range want {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkKnownKeys fails when a compiler's known-key table and the census
// disagree, naming the keys to add and remove.
func checkKnownKeys(t *testing.T, table string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	var missing, extra []string
	for _, k := range want {
		if _, ok := slices.BinarySearch(got, k); !ok {
			missing = append(missing, k)
		}
	}
	for _, k := range got {
		if _, ok := slices.BinarySearch(want, k); !ok {
			extra = append(extra, k)
		}
	}
	t.Errorf("effects.%s disagrees with the census: add %q, remove %q", table, missing, extra)
}

// TestTypedParamKnownKeysMatchTheCensus holds each W4 step 3 compiler's
// compile-time unread detection (its known-key table, behind the struct's
// Unread and its loud Note) equal to the parameter census, the
// TestChangeZoneKnownKeysMatchTheCensus contract: a key the engine starts
// reading must join the table (else the resolver Notes a parameter it
// honours), and a key it stops reading must leave it (else the Note goes
// silent on a real gap). Charm and GenericChoice share effCharm and so one
// table, which must equal each API's census on its own.
func TestTypedParamKnownKeysMatchTheCensus(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	checkKnownKeys(t, "charmKnownKeys (api:Charm)", effects.CharmKnownKeys(), censusKnownKeys(d, "Charm"))
	checkKnownKeys(t, "charmKnownKeys (api:GenericChoice)", effects.CharmKnownKeys(), censusKnownKeys(d, "GenericChoice"))
}
