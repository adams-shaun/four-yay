package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
)

// decodeLiveCaptureTokens reads a captured match.json back into host's own
// snapshot model and returns the captured token text plus the recorded
// unread list. It is the explicit precondition both this file's regression
// pin and TestReproEmitTestStripsTokenScriptsFromLiveCapture need before
// they may assert anything about -emit-test's stripping: the medical rule
// the original poisoned-cache regression failed at is that a LIVE capture
// must carry readable token TEXT. The prior substring check for `"tokens"`
// was satisfied by a `tokens_unread` list, so a capture whose every token
// path pointed at a removed worktree passed the check and only failed
// later, if at all.
func decodeLiveCaptureTokens(t *testing.T, dir string) (map[string]string, []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "match.json"))
	if err != nil {
		t.Fatalf("read captured match.json: %v", err)
	}
	var m host.FeedbackMatch
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode captured match.json: %v", err)
	}
	if len(m.Tokens) == 0 {
		t.Fatalf("live capture carried %d token script texts (tokens_unread=%d): the corpus's token "+
			"source paths were not readable back — a cache compiled under a now-gone root has not "+
			"been rerooted onto the corpus this process opened", len(m.Tokens), len(m.TokensUnread))
	}
	if len(m.TokensUnread) != 0 {
		t.Fatalf("live capture recorded %d unread token stems: %v", len(m.TokensUnread), m.TokensUnread)
	}
	return m.Tokens, m.TokensUnread
}

// TestReproCorpusCachedTokenPathsReadable is the real-corpus regression pin
// for the shared-IR-cache reroot fix (cards/open.go rerootPaths). The
// synthetic cards/cache_fingerprint_test.go:TestCachedPathsFollowTheOpenedDirectory
// checks a moved root's paths in isolation; this pin instead drives the
// live path the symptom was reported through: open the shared .cards corpus
// (a real on-disk cache compiled by whichever worktree got there first),
// take a real feedback capture, and require that every captured token came
// back as text with no unread stem AND that the registry's own token source
// paths are readable under the .cards/tokenscripts this process opened.
//
// The precondition this asserts is that the token sources the capture reads
// (Card.Path) are files under the opened corpus directory. A cache whose
// paths still name a removed worktree fails here at its source instead of
// silently degrading to tokens_unread.
func TestReproCorpusCachedTokenPathsReadable(t *testing.T) {
	requireCorpus(t)
	root, err := feedback.Root()
	if err != nil {
		t.Fatalf("no repo root: %v", err)
	}
	tokenscripts := filepath.Join(root, ".cards", "tokenscripts")
	info, err := os.Stat(tokenscripts)
	if err != nil || !info.IsDir() {
		t.Fatalf("no tokenscripts directory under the opened corpus %s: %v", tokenscripts, err)
	}

	advance, r := gatedFixtureRegistry(t)
	advance(12)

	// The same shared corpus the host's registry was built over: its token
	// Card.Path values are what host's feedback capture reads back, so a
	// cache compiled under another worktree's .cards shows up here as a
	// path outside the directory this process opened.
	reg := testutil.CorpusRegistry(t)

	src := t.TempDir()
	captureSnapshotFiles(t, r, src)

	// The capture's token completeness is the precondition, checked
	// explicitly rather than by substring: nonempty text, zero unread.
	texts, unread := decodeLiveCaptureTokens(t, src)
	if len(texts) == 0 {
		t.Fatalf("no token text captured")
	}
	if len(unread) != 0 {
		t.Fatalf("capture recorded unread token stems: %v", unread)
	}

	// Every sampled token source must be readable under the corpus directory
	// this process opened — the fix this pin protects. A path left pointing
	// at another (removed) worktree's .cards compiles the token fine but
	// fails this read, exactly the regression.
	for _, stem := range []string{"r_1_1_goblin", "c_1_1_thopter"} {
		c, ok := reg.Tokens[stem]
		if !ok {
			continue // stem absent from this corpus pin; nothing to sample
		}
		if c.Path == "" {
			t.Errorf("token %q carries no source path", stem)
			continue
		}
		if !strings.HasPrefix(filepath.Clean(c.Path), filepath.Clean(tokenscripts)+string(filepath.Separator)) {
			t.Errorf("token %q source path %q is not under the opened corpus %s — cache paths were not rerooted",
				stem, c.Path, tokenscripts)
		}
		if _, err := os.ReadFile(c.Path); err != nil {
			t.Errorf("token %q source %q is not readable: %v", stem, c.Path, err)
		}
	}
}
