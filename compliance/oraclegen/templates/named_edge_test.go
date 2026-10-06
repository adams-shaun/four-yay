package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestOraclePushPullGeneratesWithTappedTargetFixture(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	item, skip := Generate(reg, "Push // Pull")
	if skip != nil {
		t.Fatalf("Push // Pull: %s", skip.Reason)
	}
	cast := stepFor(item.Scenario, "cast")
	if cast == nil || len(cast.Targets) == 0 {
		t.Fatalf("scenario has no targeted cast step: %+v", item.Scenario.Steps)
	}
	target := cast.Targets[0]
	colon := strings.IndexByte(target, ':')
	if colon < 0 {
		t.Fatalf("cast target %q is not a named seat permanent", target)
	}
	seatKey, cardName := target[:colon], target[colon+1:]
	if hash := strings.IndexByte(cardName, '#'); hash >= 0 {
		cardName = cardName[:hash]
	}
	seat, ok := item.Setup[seatKey]
	if !ok {
		t.Fatalf("cast target %q has no corresponding setup seat", target)
	}
	battlefieldCount := 0
	for _, name := range seat.Battlefield {
		if name == cardName {
			battlefieldCount++
		}
	}
	if battlefieldCount != 1 {
		t.Fatalf("cast target %q appears on the corresponding battlefield %d times, want exactly once: %+v", target, battlefieldCount, seat.Battlefield)
	}
	tappedCount := 0
	for _, name := range seat.Tapped {
		if name == cardName {
			tappedCount++
		}
	}
	if tappedCount != 1 {
		t.Fatalf("cast target %q is marked tapped %d times in its setup seat, want exactly once: %+v", target, tappedCount, seat.Tapped)
	}
	if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
		t.Fatalf("gorge cannot play the Push // Pull scenario: %+v", item.Scenario.Steps)
	}
}
