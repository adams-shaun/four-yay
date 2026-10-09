// quietstats.go implements enginebench's -quietstats observer: it prints the
// quiet-seat proof counters (rules.QuietStats) at the end of a row.
//
// The counters are compiled into rules behind a link-time flag, so a build
// without
//
//	-ldflags "-X github.com/adams-shaun/gorge/rules.quietStatsFlag=1"
//
// pays nothing for the observer and reports all zeros here. -quietstats
// therefore errors clearly rather than printing an empty table.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/adams-shaun/gorge/rules"
)

// quietStatsFlag is set from -quietstats in main.
var quietStatsFlag bool

// quietStatsPrinted guards against printing twice when a row's runSearch exits
// and main also prints at exit.
var quietStatsPrinted bool

// setupQuietStats registers enginebench's wall clock with rules, so the
// -quietstats proof-ns and walk-ns counters are populated. rules may not
// import time (archtest), so the clock is injected here. It is cheap and
// harmless when the observer never runs.
func setupQuietStats() {
	rules.SetQuietClock(func() int64 { return time.Now().UnixNano() })
}

// printQuietStats writes the quiet-seat counters to stderr once per process.
func printQuietStats() {
	if !quietStatsFlag || quietStatsPrinted {
		return
	}
	quietStatsPrinted = true
	if !rules.QuietStatsLinked() {
		fmt.Fprintln(os.Stderr, `enginebench: -quietstats needs -ldflags "-X github.com/adams-shaun/gorge/rules.quietStatsFlag=1" (or verify mode)`)
		return
	}
	fmt.Fprint(os.Stderr, rules.QuietStatsPrint(rules.QuietStats()))
}
