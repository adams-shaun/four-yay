package archtest

import "testing"

// TestPayMirrorClockStaysAtCommandBoundary pins the separation introduced by
// 507fe00a7: the diagnostic command owns the harness wall clock, while the
// paymirror library receives only an injected budget predicate.
func TestPayMirrorClockStaysAtCommandBoundary(t *testing.T) {
	pkgs := packages(t)
	commandPath := module + "/cmd/paymirror"
	libraryPath := module + "/internal/paymirror"

	command, commandFound := pkgs[commandPath]
	library, libraryFound := pkgs[libraryPath]
	if !commandFound {
		t.Fatalf("go list did not discover %s", commandPath)
	}
	if !libraryFound {
		t.Fatalf("go list did not discover %s", libraryPath)
	}
	if !command.imports["time"] {
		t.Errorf("%s must directly import time for diagnostic harness budgeting", commandPath)
	}
	if library.imports["time"] {
		t.Errorf("%s must not directly import time; the command owns the harness clock", libraryPath)
	}
}
