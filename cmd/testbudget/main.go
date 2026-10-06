// Command testbudget enforces the per-test budget: every single test fits
// 2 GB RSS, 2 vCPU and 1 minute wall (operator, 2026-10-05). It replaces the
// per-package TEST_HISTORY.md / ALLOC_HISTORY.md budgets, which nothing
// enforced.
//
// It has three subcommands, wired together by scripts/postmerge_full.sh:
//
//	testbudget exec <test-binary> [args...]
//	    An ad-hoc wrapper that runs a binary and records its peak RSS (KiB)
//	    when GORGE_TESTBUDGET_RSS_DIR is set.
//
//	go test -json ... | testbudget tee -events events.json
//	    Reprints the plain `go test` text (failing tests' output and every
//	    package line, so the batch's failure parser still works) and writes
//	    the per-test pass/fail/skip events with their elapsed seconds.
//
//	testbudget check -events events.json [-rss-dir dir]
//	    Names every top-level test over the wall budget and every package
//	    whose test binary peaked over the RSS budget. Exit 1 on any
//	    violation that its exception inventory does not cover.
//
// Wall is per top-level test (a parent's elapsed includes its subtests; the
// heaviest subtest is named so the split is obvious). RSS can only be probed
// per test BINARY, which is an upper bound on any test inside it; a package
// over budget is attributed to a test by a capped `-run '^T$'` run and the
// pinned inventory lives in internal/testutil/testdata/rss_exceptions.txt.
// CPU is not probed: the 2 vCPU budget is a property of the scope the suite
// runs in, not of a single test.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "exec":
		os.Exit(runExec(os.Args[2:]))
	case "tee":
		os.Exit(runTee(os.Args[2:], os.Stdin, os.Stdout))
	case "check":
		os.Exit(runCheck(os.Args[2:], os.Stdout, os.Stderr))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: testbudget exec <bin> [args...] | tee -events FILE | check -events FILE [-rss-dir DIR]")
}
