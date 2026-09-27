package cards

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"sort"
	"strings"
)

// compilerSources embeds every .go file in this package so CompilerFingerprint
// can hash the source the running binary was actually built from. It is the
// package's own parse/compile source that determines the IR a cache holds, so
// two builds that differ anywhere in cards/ must never share an IR cache file
// (see CachePath).
//
// The pattern also matches _test.go files; computeCompilerFingerprint skips
// them, so a test-only edit does not invalidate a corpus cache.
//
//go:embed *.go
var compilerSources embed.FS

// CompilerFingerprint identifies the cards package source this binary was
// compiled from: a 32-hex-character SHA-256 over the sorted (name, contents)
// pairs of the package's non-test .go files. It is embedded at build time, so
// it is a constant for a given binary and never changes at runtime.
//
// It keys the IR cache file name (CachePath): a worktree whose parser differs
// writes and reads ir-<fingerprint>.gob.gz, so it can neither clobber another
// build's cache nor accidentally read one compiled by a different parser. This
// is deliberately independent of cacheVersion: cacheVersion guards the gob
// schema, the fingerprint guards the parser that produced the values.
var CompilerFingerprint = computeCompilerFingerprint()

func computeCompilerFingerprint() string {
	entries, err := compilerSources.ReadDir(".")
	if err != nil {
		// The embed directive is a build-time constant list; a read failure
		// means the binary was not built from the expected package layout and
		// there is no safe fingerprint to fall back to.
		panic("cards: reading embedded compiler sources: " + err.Error())
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		names = append(names, n)
	}
	// Sorted order makes the fingerprint independent of ReadDir ordering and
	// of filesystem iteration order.
	sort.Strings(names)

	h := sha256.New()
	for _, n := range names {
		b, err := compilerSources.ReadFile(n)
		if err != nil {
			panic("cards: reading embedded compiler source " + n + ": " + err.Error())
		}
		// A 0 byte separates name from contents and contents from the next
		// entry, so no concatenation of file bytes can collide with a
		// different split into names and contents.
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// fingerprintSourcesPresent reports whether the embedded compiler source set
// is non-empty. A build with no cards source files would otherwise hash the
// empty string to a fixed value; this makes that impossible to mistake for a
// real fingerprint.
func fingerprintSourcesPresent() bool {
	entries, err := compilerSources.ReadDir(".")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

// ensure the embedded FS is actually populated before anything relies on
// CompilerFingerprint; a broken embed would give every build the same value
// and silently reintroduce the shared-cache bug this file exists to prevent.
func init() {
	if !fingerprintSourcesPresent() {
		panic("cards: no compiler sources embedded; CompilerFingerprint would be constant across builds")
	}
}
