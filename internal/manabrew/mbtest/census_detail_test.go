package mbtest

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestCensusDetailIsSortedAndComplete pins Detail's contract, because
// cmd/cardfuzz -manabrew prints exactly this block as a run's aggregate
// census and two runs at the same seed must render it identically: fixed
// bucket order (posed, enumerated, rejected, unmapped), keys sorted within
// a bucket, exact per-key counts, and a header line carrying the totals.
func TestCensusDetailIsSortedAndComplete(t *testing.T) {
	c := NewCensus()
	c.AddGame()
	c.addPosed("chooseAction")
	c.addPosed("mulligan")
	c.addPosed("chooseAction")
	c.addEnumerated(decision.KPriority, []decision.Option{{Kind: "pass"}, {Kind: "cast"}})
	c.addEnumerated(decision.KMulligan, []decision.Option{{Kind: "keep"}})
	c.addRejected("invalid_option")
	c.addUnmapped(decision.KTarget)

	want := strings.Join([]string{
		"census: games=1 posed=3 enumerated=2 rejected=1 unmapped=1",
		"posed chooseAction 2",
		"posed mulligan 1",
		"enumerated " + string(decision.KMulligan) + ":keep 1",
		"enumerated " + string(decision.KPriority) + ":cast,pass 1",
		"rejected invalid_option 1",
		"unmapped " + string(decision.KTarget) + " 1",
		"",
	}, "\n")
	if got := c.Detail(); got != want {
		t.Fatalf("Detail mismatch:\n got: %q\nwant: %q", got, want)
	}
	// Deterministic: a second render is byte-identical.
	if again := c.Detail(); again != want {
		t.Fatalf("Detail is not deterministic:\n first: %q\nsecond: %q", want, again)
	}
	// An empty census still renders its (zero) header.
	if got := NewCensus().Detail(); got != "census: games=0 posed=0 enumerated=0 rejected=0 unmapped=0\n" {
		t.Fatalf("empty census Detail = %q", got)
	}
	if got := (*Census)(nil).Detail(); got != "census: <nil>\n" {
		t.Fatalf("nil census Detail = %q", got)
	}
}
