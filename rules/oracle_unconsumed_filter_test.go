package rules

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleUnconsumedPresenceIgnoresSubtestFilter pins the filter-safety of
// TestOracleAudit's end-of-walk presence checks. Go's -run filter still
// iterates the parent scenario loop for every loaded file, but only executes
// the matching t.Run closures. The presence check answers a STATIC question
// ("does this ratchet key name a loaded scenario file"), so it must read a set
// the parent loop fills (#seen), never one a subtest closure fills -- the
// latter fires one "names no scenario" error per ratchet row the filter did
// not run.
//
// Executes the documented per-card repro as a child test binary and requires
// a clean exit. The parent cannot assert this in-process: t.Run's filter is
// applied by the testing package before the closure runs, so the only way to
// observe the parent/closure scope split is from a separate process.
func TestOracleUnconsumedPresenceIgnoresSubtestFilter(t *testing.T) {
	// Skip loudly in a corpus-less worktree rather than letting the child
	// silently find no scenarios and pass vacuously.
	testutil.CorpusRegistry(t)

	// The anchor matters: the looser "^TestOracleAudit/Serra_Angel$" also
	// matches TestOracleAuditUnconsumedAnswerFails (a top-level test whose
	// first '/' element would only be anchored at the front). Pin the audit.
	cmd := exec.Command(os.Args[0],
		"-test.run=^TestOracleAudit$/Serra_Angel$",
		"-test.count=1",
		"-test.v",
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	msg := string(out)

	if strings.Contains(msg, "names no scenario") {
		t.Errorf("filtered Oracle audit reported a ratchet row that names no scenario; the presence check is reading a closure-scoped set:\n%s", msg)
	}
	if err != nil {
		t.Fatalf("filtered Oracle audit (Serra Angel) failed: %v\n%s", err, msg)
	}
	// Assert the child actually walked the card, not merely skipped for a
	// missing corpus: a skip would make every assertion above vacuous.
	if !strings.Contains(msg, "PASS: TestOracleAudit/Serra_Angel/") {
		t.Errorf("child did not run the Serra Angel audit scenarios (expected a --- PASS: TestOracleAudit/Serra_Angel/... line):\n%s", msg)
	}
}
