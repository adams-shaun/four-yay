package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestGeneratedBroodspinnerScenarioPinsItsActualAnswerShape ensures the
// named DSK scenario is generated through the real engine and does not
// misrepresent a synthetic arrange decision as a Broodspinner carrier. The
// current level-A cast/resolve fixture does not trigger its other-creature
// surveil ability; host replay fixtures remain the carrier authority.
func TestGeneratedBroodspinnerScenarioPinsItsActualAnswerShape(t *testing.T) {
	it, skip := Generate(loadGenRegistry(t), "Broodspinner")
	if skip != nil {
		t.Fatalf("Broodspinner generation skipped: %s", skip.Reason)
	}
	if it.Card != "Broodspinner" || len(it.Steps) != 2 {
		t.Fatalf("generated fixture is not the expected cast/resolve scenario: %+v", it)
	}
	want := [][]oraclegen.XAnswer{
		nil,
		{{Seat: 0, Kind: "target", Value: "[target_skip]"}},
	}
	if !reflect.DeepEqual(it.XAnswers, want) {
		t.Fatalf("generated Broodspinner answers = %#v, want %#v", it.XAnswers, want)
	}
}
