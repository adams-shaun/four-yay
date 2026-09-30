package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// outsideDistributionPairs are the 8 supported 60-card constructed repo decks
// that cmd/exitloop's defaultPairs (uw-tempo vs the five mono decks) does not
// reach. The loop needs no production change to train/eval/collect over them:
// -pairs is passed through verbatim (main.go evalBase, teacher args; ppo.go
// bench), and searchteacher/botbench both resolve arbitrary repo-deck names.
// These tests pin that reach, so a deck rename or a stage that drops the
// argument fails the build instead of silently shrinking the distribution.
var outsideDistributionPairs = []string{
	"death-n-taxes:death-n-taxes",
	"dimir-tempo:dimir-tempo",
	"eldrazi-stompy:eldrazi-stompy",
	"mono-red-goblins:mono-red-goblins",
	"the-epic-storm:the-epic-storm",
	"tron:tron",
	"ur-delver:ur-delver",
	"uw-control:uw-control",
}

func outsideDistributionPairString() string { return strings.Join(outsideDistributionPairs, ",") }

// Each deck named in the pair list must still exist as an embedded repo deck;
// a rename or a removal would otherwise drop the deck out of the distribution
// with nothing failing.
func TestPairsReachOutsideDistributionDecks(t *testing.T) {
	names := testutil.RepoDeckNames()
	if len(names) == 0 {
		t.Fatal("RepoDeckNames is empty: the deck embed did not load")
	}
	for _, pair := range outsideDistributionPairs {
		a, b, ok := strings.Cut(pair, ":")
		if !ok {
			t.Fatalf("pair %q is not a:b", pair)
		}
		for _, deck := range []string{a, b} {
			if !slices.Contains(names, deck) {
				t.Errorf("pair deck %q is not in RepoDeckNames(): the deck was renamed or removed", deck)
			}
		}
	}

	// Every stage that carries -pairs must carry the full expanded set; the
	// deadline corpus train stage has no -pairs and must not gain one.
	pairs := outsideDistributionPairString()
	cfg := mustConfig(t, "-bin", "/B", "-out", "/O", "-pairs", pairs)
	st := plan(cfg)
	if want := 1 + cfg.gens*(2+len(cfg.evalKinds)); len(st) != want {
		t.Fatalf("got %d stages, want %d", len(st), want)
	}
	carriers := 0
	for _, s := range st {
		if !hasArg(s, "-pairs") {
			if s.Name == "train" {
				continue
			}
			t.Errorf("stage %s (gen %d) does not carry -pairs", s.Name, s.Gen)
			continue
		}
		carriers++
		if got := argValue(t, s, "-pairs"); got != pairs {
			t.Errorf("stage %s (gen %d) -pairs = %q, want the 8-pair string", s.Name, s.Gen, got)
		}
	}
	// control + every teacher + every eval arm carries it (train never does).
	if want := 1 + cfg.gens*(1+len(cfg.evalKinds)); carriers != want {
		t.Fatalf("got %d -pairs carriers, want %d (control + teachers + eval arms)", carriers, want)
	}
}

// With -mode ppo, the control, every collect and every eval stage must carry
// the same expanded -pairs value, so collection over the outside-distribution
// decks reaches the trained checkpoint too.
func TestPPOPairsReachOutsideDistributionDecks(t *testing.T) {
	pairs := outsideDistributionPairString()
	cfg := mustConfig(t, "-mode", "ppo", "-bin", "/B", "-out", "/O",
		"-seed-checkpoint", "/S.gpol", "-rounds", "2", "-pairs", pairs)
	st := planPPO(cfg)
	if len(st) == 0 {
		t.Fatal("planPPO returned no stages")
	}
	carriers := 0
	for _, s := range st {
		if !hasArg(s, "-pairs") {
			if s.Name == "train" {
				continue
			}
			t.Errorf("stage %s (gen %d) does not carry -pairs", s.Name, s.Gen)
			continue
		}
		carriers++
		if got := argValue(t, s, "-pairs"); got != pairs {
			t.Errorf("stage %s (gen %d) -pairs = %q, want the 8-pair string", s.Name, s.Gen, got)
		}
	}
	// control + round 0 eval + 2 rounds x (collect + eval); the ppo train
	// stages have no -pairs.
	if want := 2 + 2*2; carriers != want {
		t.Fatalf("got %d -pairs carriers, want %d", carriers, want)
	}
}
