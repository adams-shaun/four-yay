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
	checkKnownKeysMatchTheCensus(t, "ChangeZone", effects.ChangeZoneKnownKeys())
}

// TestChangeZoneAllKnownKeysMatchTheCensus is the same check for
// api:ChangeZoneAll (effects.changeZoneAllKnownKeys).
func TestChangeZoneAllKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "ChangeZoneAll", effects.ChangeZoneAllKnownKeys())
}

// TestAttachKnownKeysMatchTheCensus is the same check for api:Attach
// (effects.attachKnownKeys).
func TestAttachKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "Attach", effects.AttachKnownKeys())
}

// TestDealDamageKnownKeysMatchTheCensus is the same check for
// api:DealDamage (effects.dealDamageKnownKeys).
func TestDealDamageKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "DealDamage", effects.DealDamageKnownKeys())
}

// TestPutCounterKnownKeysMatchTheCensus is the same check for
// api:PutCounter (effects.putCounterKnownKeys).
func TestPutCounterKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "PutCounter", effects.PutCounterKnownKeys())
}

// TestEffectKnownKeysMatchTheCensus is the same check for api:Effect
// (effects.effectKnownKeys).
func TestEffectKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "Effect", effects.EffectKnownKeys())
}

// TestDelayedTriggerKnownKeysMatchTheCensus is the same check for
// api:DelayedTrigger (effects.delayedTriggerKnownKeys).
func TestDelayedTriggerKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "DelayedTrigger", effects.DelayedTriggerKnownKeys())
}

// TestCopyPermanentKnownKeysMatchTheCensus is the same check for
// api:CopyPermanent (effects.copyPermanentKnownKeys).
func TestCopyPermanentKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "CopyPermanent", effects.CopyPermanentKnownKeys())
}

// TestCloneKnownKeysMatchTheCensus is the same check for api:Clone
// (effects.cloneKnownKeys).
func TestCloneKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "Clone", effects.CloneKnownKeys())
}

// checkKnownKeysMatchTheCensus holds an API compiler's known-key table equal
// to the census's measured read set for api plus its ignored and structural
// keys.
func checkKnownKeysMatchTheCensus(t *testing.T, api string, got []string) {
	t.Helper()
	_, d := measureParamCensus(t, nil)
	want := map[string]bool{}
	for k := range d.api[api] {
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
		t.Fatalf("effects' %s known-key table disagrees with the census: add %q, remove %q", api, missing, extra)
	}
}
