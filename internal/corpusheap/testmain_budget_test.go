package corpusheap_test

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testbudget"
)

// TestMain records this test binary's peak RSS for the per-test budget
// (internal/testbudget), which is what internal/testbudget's
// TestEveryTestPackageRecordsRSS requires of every package holding test files.
func TestMain(m *testing.M) {
	testbudget.Main(m)
}
