package cards_test

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestForgeBoundary is the licence guard from the Forge oracle design (§11,
// design decision P1-7). Forge is GPL-3.0; gorge is Apache-2.0. Nothing that
// belongs to Forge — its Java source, its oracle driver tree, its result rows
// or request files — may be tracked in this repository. The existing
// cards/boundary_test.go guards the card-script corpus by content; this guard
// covers the four Forge-artifact shapes the design names.
//
// A tracked file is an offender when it is any of:
//
//  1. a *.java file declaring `package forge.`;
//  2. a path under `forge-oracle/` (any depth);
//  3. a path under `compliance/forge*` (the result-row / request-file surface);
//  4. a file carrying the Forge GPL header line.
//
// forgeBoundaryAllowlist is the explicit, empty-by-default escape hatch for
// the §9 adjudication ledger. The design (§9, recommended option C) permits
// committing `compliance/adjudication/<a-z>.jsonl`, which carries only gorge's
// own pattern vocabulary and field names — never a Forge-produced value,
// message or script fragment. An entry is a repo-relative slash-separated
// path; paths are matched exactly, so a ledger directory cannot be used to
// smuggle anything else. Keep it empty until that ledger actually lands, and
// add only paths the design names as allowed.
var forgeBoundaryAllowlist = map[string]bool{}

// forgeGPLHeader is the distinguishing line of the Forge source header. It is
// a full sentence rather than a fragment like "GNU General Public License" so
// that a gorge file merely discussing the GPL (as this test's own comments
// do) is not mistaken for Forge source. It is assembled from two pieces so
// that this file — which IS tracked and IS scanned by TestForgeBoundary —
// never literally contains the substring it tests for.
const forgeGPLHeader = "Forge: " + "Play Magic: the Gathering."

// hasPathComponent reports whether slash (a slash-separated path) contains
// name as a whole component — so "forge-oracle" matches "forge-oracle/x" and
// "tools/forge-oracle/x" but not "forge-oracle-tools/x".
func hasPathComponent(slash, name string) bool {
	for _, part := range strings.Split(slash, "/") {
		if part == name {
			return true
		}
	}
	return false
}

// forgeBoundaryOffender describes one tracked path that breaches the boundary,
// with the rule that caught it, so a failure names the rule as well as the file.
type forgeBoundaryOffender struct {
	path   string
	reason string
}

// forgeBoundaryViolations classifies a repo-relative path and the blob it
// holds (blob may be nil when the file exists only in the index or in HEAD, in
// which case the path rules still apply). It returns every rule the path and
// content breach; a path may breach more than one.
func forgeBoundaryViolations(p string, blob []byte) []forgeBoundaryOffender {
	if forgeBoundaryAllowlist[p] {
		return nil
	}

	var out []forgeBoundaryOffender

	// Rule 2: a path with a forge-oracle/ component at any depth. Use the
	// slash form so matching is independent of the host OS separator.
	slash := path.Clean(filepath.ToSlash(p))
	if hasPathComponent(slash, "forge-oracle") {
		out = append(out, forgeBoundaryOffender{p, "path under forge-oracle/"})
	}

	// Rule 3: a path under compliance/forge* (the result-row / request-file
	// surface). The design's glob is a single component: a file or directory
	// directly under compliance/ whose name begins with "forge".
	if rest, ok := strings.CutPrefix(slash, "compliance/"); ok && strings.HasPrefix(rest, "forge") {
		out = append(out, forgeBoundaryOffender{p, "path under compliance/forge*"})
	}

	if blob != nil {
		// Rule 1: a Java file declaring package forge.
		if strings.HasSuffix(slash, ".java") {
			for _, line := range strings.Split(string(blob), "\n") {
				trimmed := strings.TrimSpace(line)
				trimmed = strings.TrimSuffix(trimmed, ";")
				if strings.HasPrefix(trimmed, "package forge.") || trimmed == "package forge" {
					out = append(out, forgeBoundaryOffender{p, "*.java declaring package forge."})
					break
				}
			}
		}

		// Rule 4: the Forge GPL header line. Scan line by line so a blob of
		// any size is fine and a binary file cannot smuggle it past a prefix
		// check.
		if strings.Contains(string(blob), forgeGPLHeader) {
			out = append(out, forgeBoundaryOffender{p, "carries the Forge GPL header"})
		}
	}

	return out
}

// checkForgeBoundaryInRepo scans every file tracked in the git repository at
// repoDir (the working tree, then the staged index, then HEAD, matching
// cards/boundary_test.go's ordering) and returns the offenders. It returns an
// error only when git itself is unavailable.
func checkForgeBoundaryInRepo(t *testing.T, repoDir string) ([]forgeBoundaryOffender, error) {
	t.Helper()

	out, err := gitCmd("-C", repoDir, "ls-files", "-z").Output()
	if err != nil {
		return nil, err
	}

	// A file whose name matches a path rule is an offender even when its blob
	// cannot be read, so collect the names first and union the findings.
	var offenders []forgeBoundaryOffender
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" {
			continue
		}
		offenders = append(offenders, forgeBoundaryViolations(p, readTrackedBlob(t, repoDir, p))...)
	}
	return offenders, nil
}

// readTrackedBlob returns the content of a tracked path, preferring the
// working tree, then the staged index, then HEAD. It returns nil when none of
// the three hold the file (a genuinely absent blob) — the path rules then
// decide the verdict on their own.
func readTrackedBlob(t *testing.T, repoDir, p string) []byte {
	t.Helper()

	if data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(p))); err == nil {
		return data
	}
	if data, err := gitCmd("-C", repoDir, "show", ":"+p).Output(); err == nil {
		return data
	}
	if data, err := gitCmd("-C", repoDir, "show", "HEAD:"+p).Output(); err == nil {
		return data
	}
	return nil
}

// TestForgeBoundary fails if any file tracked in this repository is a Forge
// source file, a forge-oracle/ tree entry, a compliance/forge* artifact, or a
// file carrying the Forge GPL header.
func TestForgeBoundary(t *testing.T) {
	root := repoRoot(t)

	offenders, err := checkForgeBoundaryInRepo(t, root)
	if err != nil {
		t.Fatalf("git unavailable while scanning %s: %v", root, err)
	}
	if len(offenders) > 0 {
		var b strings.Builder
		b.WriteString("Forge artifacts are tracked in git, which breaks the GPL boundary:\n")
		for _, o := range offenders {
			b.WriteString("  " + o.path + "  (" + o.reason + ")\n")
		}
		t.Fatalf("%s", b.String())
	}
}

// TestForgeBoundaryDetection is the negative case the brief requires: each
// shape this guard exists to catch is planted in a throwaway git repository
// under t.TempDir, and the guard must flag it. The allowlist is exercised too,
// so the escape hatch is proven to work rather than assumed.
func TestForgeBoundaryDetection(t *testing.T) {
	// A blob carrying the header must be assembled from pieces so that THIS
	// file does not itself contain the substring it tests for — otherwise the
	// guard, run over the repo, would flag its own test.
	gplHeader := "Forge: " + "Play Magic: the Gathering."

	tests := []struct {
		name      string
		file      string
		body      string
		allow     bool // add file to the allowlist for this case
		wantMatch bool
		reason    string
	}{
		{
			name:      "java declaring package forge",
			file:      "src/main/java/forge/game/Game.java",
			body:      "/* header */\npackage forge.game;\n\nclass Game {}\n",
			wantMatch: true,
			reason:    "*.java declaring package forge. is Forge source",
		},
		{
			name:      "java in a non-forge package",
			file:      "src/main/java/com/example/App.java",
			body:      "package com.example;\n\nclass App {}\n",
			wantMatch: false,
			reason:    "a Java file that does not declare package forge. is not Forge source",
		},
		{
			name:      "file under forge-oracle",
			file:      "forge-oracle/src/main/java/forge/oracle/ScenarioReplay.java",
			body:      "package forge.oracle;\n",
			wantMatch: true,
			reason:    "any path under forge-oracle/ belongs to the GPL driver",
		},
		{
			name:      "nested file under forge-oracle",
			file:      "tools/forge-oracle/pom.xml",
			body:      "<project/>\n",
			wantMatch: true,
			reason:    "forge-oracle/ is caught at any depth",
		},
		{
			name:      "path under compliance forge prefix",
			file:      "compliance/forge-verdicts/a.jsonl",
			body:      "{}\n",
			wantMatch: true,
			reason:    "compliance/forge* is forbidden apart from the ledger",
		},
		{
			name:      "file carrying the Forge GPL header",
			file:      "internal/oracle/snapshot.go",
			body:      "package oracle\n\n// " + gplHeader + "\n",
			wantMatch: true,
			reason:    "the Forge GPL header must never enter a gorge file",
		},
		{
			name:      "innocuous Go file",
			file:      "internal/oracle/snapshot.go",
			body:      "package oracle\n\n// A snapshot read from an external process.\n",
			wantMatch: false,
			reason:    "an ordinary gorge file is not an offender",
		},
		{
			name:      "allowlisted forge path",
			file:      "compliance/forge-verdicts/a.jsonl",
			body:      "{}\n",
			allow:     true,
			wantMatch: true,
			reason:    "control: the same path IS flagged once the allowlist entry is removed",
		},
		{
			name:      "ledger path is not a forge-oracle artifact",
			file:      "compliance/adjudication/a.jsonl",
			body:      "{}\n",
			wantMatch: false,
			reason:    "the section 9 ledger lives under compliance/adjudication and is allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if err := gitCmd("-C", tmpDir, "init").Run(); err != nil {
				t.Fatalf("git init failed: %v", err)
			}
			if err := gitCmd("-C", tmpDir, "-c", "user.email=test@test.com", "-c", "user.name=Test",
				"commit", "--allow-empty", "-m", "initial").Run(); err != nil {
				t.Fatalf("initial commit failed: %v", err)
			}

			full := filepath.Join(tmpDir, filepath.FromSlash(tt.file))
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				t.Fatalf("mkdir failed: %v", err)
			}
			if err := os.WriteFile(full, []byte(tt.body), 0644); err != nil {
				t.Fatalf("write failed: %v", err)
			}
			if err := gitCmd("-C", tmpDir, "add", "--", tt.file).Run(); err != nil {
				t.Fatalf("git add failed: %v", err)
			}

			// Precondition: the file really is tracked under the path we chose,
			// so a guard that silently skipped it could not pass this case.
			tracked, err := gitCmd("-C", tmpDir, "ls-files", "-z").Output()
			if err != nil {
				t.Fatalf("git ls-files failed: %v", err)
			}
			if !strings.Contains(string(tracked), tt.file) {
				t.Fatalf("precondition failed: %q is not tracked", tt.file)
			}

			if tt.allow {
				forgeBoundaryAllowlist[tt.file] = true
				defer delete(forgeBoundaryAllowlist, tt.file)

				// With the allowlist entry, the path must be accepted.
				allowed, err := checkForgeBoundaryInRepo(t, tmpDir)
				if err != nil {
					t.Fatalf("git unavailable: %v", err)
				}
				if len(allowed) != 0 {
					t.Fatalf("allowlisted %q was still flagged: %v", tt.file, allowed)
				}
				// Control below removes the entry and proves the case can fail.
			}

			// Drop any allowlist entry so the rest of the case sees the raw rule.
			delete(forgeBoundaryAllowlist, tt.file)

			offenders, err := checkForgeBoundaryInRepo(t, tmpDir)
			if err != nil {
				t.Fatalf("git should be available in temp repo: %v", err)
			}

			matched := false
			for _, o := range offenders {
				if o.path == tt.file {
					matched = true
					break
				}
			}

			if !tt.wantMatch {
				if matched {
					t.Errorf("expected %q NOT to be flagged, but it was: %v (reason: %s)",
						tt.file, offenders, tt.reason)
				}
				return
			}

			if tt.allow {
				if !matched {
					t.Errorf("control failed: %q must be flagged once its allowlist entry is removed (reason: %s)",
						tt.file, tt.reason)
				}
				return
			}

			if !matched {
				t.Errorf("expected %q to be flagged, but it was not (reason: %s)", tt.file, tt.reason)
			}
		})
	}
}
