package templates

import (
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
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

// targetAuditCase is one carrier's Generate + replay outcome, trimmed to
// what the two target audits assert: the cast steps' scenario targets and
// the replay's fails and decisions (no snapshots or transcript).
type targetAuditCase struct {
	name      string
	skip      *oraclegen.Skip
	steps     []targetAuditStep
	err       error
	fails     []string
	decisions []rules.OracleDecision
}

type targetAuditStep struct {
	op      string
	targets []string
}

// targetAuditMemo runs the audit's Generate + replay ONCE per process for
// both TestGeneratedTargetsAreGorgeChoices and
// TestGeneratedMandatoryCastTargetsSurvive: each used to generate and replay
// every carrier itself, doubling the full audit's cost for identical work.
var targetAuditMemo struct {
	sync.Mutex
	names []string
	cases []targetAuditCase
}

// targetAuditCases returns every carrier's case, in carrier order. The
// carriers are independent scenarios on the process-shared read-only
// registry, so a bounded worker pool plays them concurrently; each result
// lands in its own index, so the order the workers finish in never reaches
// an assertion.
func targetAuditCases(t *testing.T, reg *cards.Registry, names []string) []targetAuditCase {
	t.Helper()
	targetAuditMemo.Lock()
	defer targetAuditMemo.Unlock()
	if sameNames(targetAuditMemo.names, names) {
		return targetAuditMemo.cases
	}
	out := make([]targetAuditCase, len(names))
	workers := min(runtime.GOMAXPROCS(0), 8, len(names))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				out[i] = runTargetAuditCase(reg, names[i])
			}
		}()
	}
	for i := range names {
		next <- i
	}
	close(next)
	wg.Wait()
	targetAuditMemo.names = append([]string(nil), names...)
	targetAuditMemo.cases = out
	return out
}

func runTargetAuditCase(reg *cards.Registry, name string) targetAuditCase {
	c := targetAuditCase{name: name}
	it, skip := Generate(reg, name)
	if skip != nil {
		c.skip = skip
		return c
	}
	for _, st := range it.Scenario.Steps {
		c.steps = append(c.steps, targetAuditStep{op: st.Op, targets: st.Targets})
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	c.err, c.fails, c.decisions = err, res.Fails, res.Decisions
	return c
}

func sameNames(a, b []string) bool {
	if a == nil || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
