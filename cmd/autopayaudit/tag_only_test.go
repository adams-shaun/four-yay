//go:build !autopayaudit

// This file exists so `go test ./cmd/autopayaudit` succeeds under the default
// build tags. The package's only non-test source (main.go) is constrained to
// `-tags autopayaudit`, so without a default-build file the package is
// unbuildable and every explicit `go test ./cmd/autopayaudit` invocation fails
// with "build constraints exclude all Go files". The per-ticket gate
// (scripts/gate_affected.sh) lists every touched package explicitly, so that
// failure would park any ticket whose diff touches this directory.
//
// The test pins the invariant that makes it safe: this package must never have
// a default-build non-test source, because the audit harness reaches into
// build-tagged rules diagnostics (rules/zz_autopayaudit.go) and must not enter
// production builds. Adding an untagged source here makes this fail.
package main

import (
	"go/build"
	"testing"
)

func TestAutopayauditIsExcludedFromDefaultBuilds(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("import cmd/autopayaudit: %v", err)
	}
	if len(pkg.GoFiles) != 0 {
		t.Fatalf("cmd/autopayaudit must be built only with -tags autopayaudit; "+
			"default-build non-test sources present: %v", pkg.GoFiles)
	}
}
