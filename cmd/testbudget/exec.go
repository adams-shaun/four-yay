package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/adams-shaun/gorge/internal/testbudget"
)

// rssDirEnv names the directory runExec records peak RSS in.
const rssDirEnv = testbudget.RSSDirEnv

// runExec runs a test binary exactly as `go test` would and records its peak
// RSS. Its exit code is the binary's, so `go test -exec` sees no difference.
func runExec(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "testbudget exec: no test binary")
		return 2
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
		if code < 0 {
			code = 1
		}
	case err != nil:
		fmt.Fprintf(os.Stderr, "testbudget exec: %v\n", err)
		return 1
	}
	if dir := os.Getenv(rssDirEnv); dir != "" && cmd.ProcessState != nil {
		if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			if werr := testbudget.RecordRSS(dir, int64(ru.Maxrss)); werr != nil {
				fmt.Fprintf(os.Stderr, "testbudget exec: %v\n", werr)
			}
		}
	}
	return code
}

func readRSSDir(dir string) (map[string]int64, error) {
	return testbudget.ReadRSSDir(dir)
}
