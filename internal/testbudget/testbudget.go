// Package testbudget records per-test-binary resource measurements.
package testbudget

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const RSSDirEnv = "GORGE_TESTBUDGET_RSS_DIR"

// Main runs the test suite and records this process's peak resident set size
// when RSS measurement is enabled.
func Main(m *testing.M, after ...func()) {
	code := m.Run()
	for _, fn := range after {
		fn()
	}
	if dir := os.Getenv(RSSDirEnv); dir != "" {
		var usage syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
			fmt.Fprintf(os.Stderr, "testbudget: getrusage: %v\n", err)
			if code == 0 {
				code = 1
			}
		} else if err := RecordRSS(dir, int64(usage.Maxrss)); err != nil {
			fmt.Fprintf(os.Stderr, "testbudget: record RSS: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// RecordRSS writes this run's peak KiB to the current package's record. A
// cached package does not call it, preserving that package's last measurement.
func RecordRSS(dir string, kib int64) error {
	rel, err := moduleRelDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, url.PathEscape(rel))
	return os.WriteFile(path, []byte(strconv.FormatInt(kib, 10)+"\n"), 0o644)
}

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

// ReadRSSDir returns package directory -> peak KiB for every file in dir.
func ReadRSSDir(dir string) (map[string]int64, error) {
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
