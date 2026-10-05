package templates

import (
	"testing"
)

func TestOraclePushPullGeneratesWithTappedTargetFixture(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	item, skip := Generate(reg, "Push // Pull")
	if skip != nil {
		t.Fatalf("Push // Pull: %s", skip.Reason)
	}
	if len(item.Steps) == 0 {
		t.Fatal("scenario has no cast step")
	}
	foundTappedTarget := false
	for _, seat := range item.Setup {
		if len(seat.Tapped) > 0 {
			foundTappedTarget = true
		}
	}
	if !foundTappedTarget {
		t.Fatalf("Push // Pull scenario did not arrange a tapped target: %+v", item.Setup)
	}
}
