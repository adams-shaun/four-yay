package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestPaymentStatsFlag pins -payment-stats (ticket aph-plan-diagnostics):
// with the flag on, every game's engine carries its own planner sink and the
// merged counters show real offers; the run's own -out json report is
// byte-identical with the flag off and on, because the sink only observes.
func TestPaymentStatsFlag(t *testing.T) {
	dir := corpusDirOrSkip(t)
	pairs, err := parsePairs("mono-red-goblins:tron", testutil.RepoDeckNames())
	if err != nil {
		t.Fatalf("parsePairs: %v", err)
	}
	reset := func() {
		paymentStatsTotal.Lock()
		paymentStatsTotal.stats = rules.PaymentPlanStats{}
		paymentStatsTotal.Unlock()
	}
	defer func() { paymentStatsEnabled = false; reset() }()

	var off, on bytes.Buffer
	paymentStatsEnabled = false
	reset()
	if err := runMatrix(1, 2, 2, "bot-auto-pay", "bot", dir, "json", pairs, 2, 200, 0, false, nil, &off, io.Discard); err != nil {
		t.Fatalf("runMatrix(flag off): %v", err)
	}
	if paymentStatsTotal.stats.DecisionsBuilt != 0 {
		t.Fatalf("flag off recorded stats: %+v", paymentStatsTotal.stats)
	}
	paymentStatsEnabled = true
	if err := runMatrix(1, 2, 2, "bot-auto-pay", "bot", dir, "json", pairs, 2, 200, 0, false, nil, &on, io.Discard); err != nil {
		t.Fatalf("runMatrix(flag on): %v", err)
	}
	if !bytes.Equal(off.Bytes(), on.Bytes()) {
		t.Fatalf("-out json differs with -payment-stats:\noff:\n%s\non:\n%s", off.String(), on.String())
	}
	s := paymentStatsTotal.stats
	if s.DecisionsBuilt == 0 || s.Candidates == 0 || s.ActionsOffered == 0 || s.Nodes == 0 || s.PlannedSubmissions == 0 {
		t.Fatalf("flag on recorded no auto-pay activity: %+v", s)
	}
	var report bytes.Buffer
	if err := writePaymentStats(&report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"payment plan stats:", "actions offered:", "outcomes by reason:", "outcomes by detail:", "planned submissions:", "fallbacks by reason:"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("report missing %q:\n%s", want, report.String())
		}
	}
}
