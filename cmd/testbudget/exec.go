package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// rssDirEnv names the directory runExec records peak RSS in.
const rssDirEnv = "GORGE_TESTBUDGET_RSS_DIR"

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
			if werr := recordRSS(dir, int64(ru.Maxrss)); werr != nil {
				fmt.Fprintf(os.Stderr, "testbudget exec: %v\n", werr)
			}
		}
	}
	return code
}

// recordRSS writes kib (ru_maxrss, KiB on linux) to the file for the current
// package directory, keeping the larger of any value already there.
func recordRSS(dir string, kib int64) error {
	rel, err := moduleRelDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, url.PathEscape(rel))
	if b, err := os.ReadFile(path); err == nil {
		if old, perr := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); perr == nil && old >= kib {
			return nil
		}
	}
	return os.WriteFile(path, []byte(strconv.FormatInt(kib, 10)+"\n"), 0o644)
}

// moduleRelDir is the working directory (go test runs a binary in its
// package's directory) relative to the module root, slash-separated.
func moduleRelDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			rel, rerr := filepath.Rel(d, wd)
			return filepath.ToSlash(rel), rerr
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no go.mod above %s", wd)
		}
	}
}

// readRSSDir returns package directory -> peak KiB for every file in dir.
func readRSSDir(dir string) (map[string]int64, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(ents))
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		kib, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", e.Name(), err)
		}
		pkg, err := url.PathUnescape(e.Name())
		if err != nil {
			return nil, err
		}
		out[pkg] = kib
	}
	return out, nil
}
