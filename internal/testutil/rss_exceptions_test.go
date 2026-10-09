package testutil

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// rssExceptionPeaks is the shrink-only inventory. Delete an entry when its
// test fits the budget; adding or changing a measured peak raises the ratchet.
// Empty as of 2026-10-09: the operator doubled the budget (2 GiB/2 vCPU ->
// 4 GiB/4 vCPU, GOMEMLIMIT 1536MiB -> 3GiB) when the box went to 120 GiB, and
// the two rows the 2 GiB budget pinned (host
// TestHostedEnvSeatsNeverSeeTheLiveEngine 2107888, host
// TestHostFeedEqualsRebuildFeed 2102464) fit 4 GiB with room to spare.
var rssExceptionPeaks = map[string]int{}

// rssBudgetKiB is the 4 GiB per-test budget the gate scopes are sized to
// (MemoryMax = concurrent binaries x 4 GB). Doubled from 2 GiB on 2026-10-09
// with the box's DRAM.
const rssBudgetKiB = 4 * 1024 * 1024

func TestRSSExceptionsOnlyShrink(t *testing.T) {
	data, err := os.ReadFile("testdata/rss_exceptions.txt")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows++
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("row %q: want package<TAB>test<TAB>peak_kib", line)
		}
		key := f[0] + "\t" + f[1]
		if seen[key] {
			t.Fatalf("duplicate RSS exception %q", key)
		}
		seen[key] = true
		peak, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("row %q: peak_kib: %v", line, err)
		}
		if peak <= rssBudgetKiB {
			t.Errorf("row %q: %d KiB fits the %d KiB budget; delete the row", line, peak, rssBudgetKiB)
		}
		want, ok := rssExceptionPeaks[key]
		if !ok {
			t.Errorf("unexpected RSS exception %q: inventory only shrinks", key)
		} else if peak != want {
			t.Errorf("row %q: measured peak %d KiB, want pinned %d KiB", key, peak, want)
		}
	}
	if rows != len(rssExceptionPeaks) {
		t.Fatalf("%d exception rows, pinned inventory has %d", rows, len(rssExceptionPeaks))
	}
}
