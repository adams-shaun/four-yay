package rules

import (
	"slices"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// TestDefinedTierKeysMatchTheCensus is the Defined-reference tier's
// known-key census line. effects.compileDefined compiles Defined$,
// DefinedCards$ and DefinedTarget$ for EVERY ability, so the parameter census
// must count each of them read for every API it attributes (which is why
// every per-API known-key table carries all three). DefinedPlayer$ has its own
// single reader instead (effects.definedPlayerRef): the census must attribute
// it to exactly the APIs whose paths reach that reader -- a generic read would
// make every per-API known-key table demand it.
func TestDefinedTierKeysMatchTheCensus(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	if len(d.api) < 50 {
		t.Fatalf("census attributes only %d APIs: the check is vacuous", len(d.api))
	}
	var readers []string
	for api, keys := range d.api {
		for _, k := range effects.DefinedTierKeys() {
			if !keys[k] {
				t.Errorf("api:%s: the census does not count the Defined-tier key %s$ read", api, k)
			}
		}
		if keys["DefinedPlayer"] {
			readers = append(readers, api)
		}
	}
	sort.Strings(readers)
	if want := definedPlayerReaders; !slices.Equal(readers, want) {
		t.Fatalf("DefinedPlayer$ readers = %q, want %q (its single reader is effects.definedPlayerRef)", readers, want)
	}
}

// definedPlayerReaders are the APIs whose paths reach effects.definedPlayerRef.
var definedPlayerReaders = []string{"AddOrRemoveCounter", "ChangeZone", "Cloak", "Manifest", "ManifestDread"}
