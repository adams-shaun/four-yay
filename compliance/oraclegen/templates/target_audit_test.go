package templates

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

const fullTargetAuditEnv = "GORGE_ORACLEGEN_FULL_TARGET_AUDIT"

// targetAuditCards keeps ordinary package gates focused while leaving the
// full, pinned-corpus generation/replay audit explicitly available via
// GORGE_ORACLEGEN_FULL_TARGET_AUDIT=1 go test -run 'TestGeneratedTargetsAreGorgeChoices|TestGeneratedMandatoryCastTargetsSurvive' ./compliance/oraclegen/templates/.
func targetAuditCards(t *testing.T, reg *cards.Registry) []string {
	t.Helper()
	if os.Getenv(fullTargetAuditEnv) == "1" {
		return targetCarriers(t, reg)
	}
	return []string{"Conduct Electricity", "Repulsive Mutation"}
}

// TestTargetCarrierCensus preserves the exhaustive identity ratchet without
// invoking Generate or replaying scenarios. The opt-in full audit above uses
// this same exact-set census before traversing all identities.
func TestTargetCarrierCensus(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := targetCarriers(t, reg)
	if len(carriers) == 0 {
		t.Fatal("precondition: target carrier census is empty")
	}
}
