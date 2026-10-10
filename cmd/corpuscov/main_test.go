package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func corpusDirOrSkip(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("could not resolve git repo root: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), ".cards")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}
	return dir
}

// TestCensusExploreDeterministicAndFlagged plays two short games with
// coverage-directed exploration on, twice: the census and the decision
// record must be byte-identical (seeded PCG, no ambient randomness), and
// forced answers must be flagged in the record and counted apart from the
// policy's own choices.
func TestCensusExploreDeterministicAndFlagged(t *testing.T) {
	dir := corpusDirOrSkip(t)
	runOnce := func() (string, []byte, []byte) {
		tmp := t.TempDir()
		c := config{games: 2, seed: 7, policy: "bot", autopay: true, explore: 0.3, dir: dir, top: 5,
			jsonOut: filepath.Join(tmp, "c.json"), record: filepath.Join(tmp, "r.jsonl")}
		var out bytes.Buffer
		if err := run(c, &out); err != nil {
			t.Fatal(err)
		}
		js, _ := os.ReadFile(c.jsonOut)
		rec, _ := os.ReadFile(c.record)
		return out.String(), js, rec
	}
	t1, j1, r1 := runOnce()
	t2, j2, r2 := runOnce()
	if t1 != t2 || !bytes.Equal(j1, j2) || !bytes.Equal(r1, r2) {
		t.Fatal("census run is not deterministic")
	}
	var doc struct {
		Summary struct {
			Games, Slots, InDeck, Offered, Chosen, ExploreDecisions int
		}
	}
	if err := json.Unmarshal(j1, &doc); err != nil {
		t.Fatal(err)
	}
	s := doc.Summary
	if s.Games != 2 || s.Slots == 0 || s.Offered == 0 || s.Chosen > s.Offered {
		t.Fatalf("implausible funnel %+v", s)
	}
	if s.ExploreDecisions == 0 {
		t.Fatal("explore=0.3 forced no decision")
	}
	flagged := 0
	sc := bufio.NewScanner(bytes.NewReader(r1))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Explore {
			flagged++
			if r.Kind != "priority" {
				t.Fatalf("explore flag on a %s decision", r.Kind)
			}
		}
	}
	if flagged != s.ExploreDecisions {
		t.Fatalf("record flags %d explore decisions, census counted %d", flagged, s.ExploreDecisions)
	}
	if !strings.Contains(t1, "explore=0.3") {
		t.Fatalf("header missing explore setting:\n%s", t1)
	}
}
