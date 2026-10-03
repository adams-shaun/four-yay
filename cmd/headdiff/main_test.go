package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/replay"
)

// repoRootOrSkip resolves the repo root with git (cards.GitEnv, so a hook's
// exported GIT_DIR cannot redirect it; see internal/testutil/decks.go) and
// Skips when .cards/ is absent, as cmd/mtgsim's corpusDirOrSkip does.
func repoRootOrSkip(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("could not resolve git repo root: %v", err)
	}
	root := strings.TrimSpace(string(out))
	if _, err := os.Stat(filepath.Join(root, ".cards")); err != nil {
		t.Skip("no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}
	return root
}

// TestDumpThenAgainst is the reviewer workflow end to end on one seat count:
// a dump compared against the same tree is identical and matches the pinned
// head, and a recording with one event changed (re-chained, so it is a
// well-formed recording of a different game) is reported at that event.
func TestDumpThenAgainst(t *testing.T) {
	if testing.Short() {
		t.Skip("plays an acceptance game")
	}
	root := repoRootOrSkip(t)
	corpus := filepath.Join(root, ".cards")
	heads := filepath.Join(root, "rules", "testdata", "heads")
	ref := filepath.Join(t.TempDir(), "ref.json")

	var out, errOut bytes.Buffer
	if code := run([]string{"-dir", corpus, "-heads", heads, "-seats", "2", "-dump", ref}, &out, &errOut); code != 0 {
		t.Fatalf("dump: exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "(matches the pinned head)") {
		t.Errorf("dump output does not match the pinned 2-seat head:\n%s", out.String())
	}

	out.Reset()
	if code := run([]string{"-dir", corpus, "-heads", heads, "-seats", "2", "-against", ref}, &out, &errOut); code != 0 {
		t.Fatalf("against the same tree: exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "identical to the recording") {
		t.Errorf("against the same tree is not identical:\n%s", out.String())
	}

	recs, err := readRecords(ref)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[2]
	const at = 100
	r.Events[at].Text += "-perturbed"
	r.Head = (&events.Log{Seed: r.Seed, Events: r.Events}).HeadAt(len(r.Events))
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"-dir", corpus, "-heads", heads, "-seats", "2", "-against", ref}, &out, &errOut); code != 1 {
		t.Fatalf("against a perturbed recording: exit %d, want 1\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "DIVERGED at event 100 ") || !strings.Contains(out.String(), "-perturbed") {
		t.Errorf("perturbed recording not reported at event %d:\n%s", at, out.String())
	}
}

// TestReadRecordsRefusesALossyStream: a recording whose events do not chain
// to its recorded head is refused rather than compared.
func TestReadRecordsRefusesALossyStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	raw, _ := json.Marshal(record{Seats: 2, Seed: 1, Head: "0000000000000000", Events: []events.Event{{Seq: 0, Kind: events.GameStart}}})
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecords(path); err == nil || !strings.Contains(err.Error(), "not its recorded head") {
		t.Fatalf("readRecords = %v, want a head mismatch refusal", err)
	}
}

// TestReportShowsContext pins the divergence report's shape on synthetic
// streams: the common prefix, then each side from the divergent event, with
// the divergent event marked.
func TestReportShowsContext(t *testing.T) {
	mk := func(texts ...string) []events.Event {
		evs := make([]events.Event, len(texts))
		for i, s := range texts {
			evs[i] = events.Event{Seq: uint64(i), Kind: events.Note, Text: s}
		}
		return evs
	}
	want := record{Head: "w", Events: mk("a", "b", "c", "d", "e")}
	got := record{Head: "g", Events: mk("a", "b", "c", "X", "e", "f")}
	d := replay.FirstDivergence(want.Events, got.Events)
	if d == nil || d.Seq != 3 {
		t.Fatalf("FirstDivergence = %+v, want event 3", d)
	}
	var buf bytes.Buffer
	report(&buf, want, got, d, 2)
	s := buf.String()
	for _, frag := range []string{
		"DIVERGED at event 3 (recording: 5 events, head w; this tree: 6 events, head g)",
		"      #1 note p0 text=\"b\"\n      #2 note p0 text=\"c\"\n  recording:",
		"  > #3 note p0 text=\"d\"",
		"  > #3 note p0 text=\"X\"",
		"    #5 note p0 text=\"f\"",
	} {
		if !strings.Contains(s, frag) {
			t.Errorf("report missing %q:\n%s", frag, s)
		}
	}
}
