package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// saveAZ restores every az package variable after the test.
func saveAZ(t *testing.T) {
	t.Helper()
	cfg, net, world, kinds, given := azCfg, azNet, azWorldArg, azKindsArg, azFlagsGiven
	azStats.mu.Lock()
	diags := azStats.diags
	azStats.mu.Unlock()
	millis, watch := azmcts.Millis, azmcts.Watch
	t.Cleanup(func() {
		azCfg, azNet, azWorldArg, azKindsArg, azFlagsGiven = cfg, net, world, kinds, given
		azStats.mu.Lock()
		azStats.diags = diags
		azStats.mu.Unlock()
		azmcts.Millis, azmcts.Watch = millis, watch
	})
}

func TestAZFrontDoor(t *testing.T) {
	saveAZ(t)
	azWorldArg, azFlagsGiven = "", false
	if err := azFrontDoor("bot", "bot", nil); err != nil {
		t.Fatalf("no az side, no az flags: %v", err)
	}
	azFlagsGiven = true
	if err := azFrontDoor("bot", "bot", nil); err == nil || !strings.Contains(err.Error(), "neither side is az") {
		t.Fatalf("az flags without an az side: %v", err)
	}
	azFlagsGiven = false
	for _, tc := range []struct{ world, want string }{
		{"", "requires -az-world clairvoyant or redeal"},
		{"sampled", "not implemented"},
		{"oracle", "want clairvoyant, redeal or sampled"},
	} {
		azWorldArg = tc.world
		if err := azFrontDoor("az", "bot", nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("-az-world %q: %v, want %q", tc.world, err, tc.want)
		}
	}
	azWorldArg = "clairvoyant"
	azKindsArg = "priority,bogus"
	if err := azFrontDoor("az", "bot", nil); err == nil || !strings.Contains(err.Error(), "-az-kinds") {
		t.Fatalf("bad -az-kinds: %v", err)
	}
	azKindsArg = "attackers"
	if err := azFrontDoor("bot", "az", &policynet.Model{}); err == nil || !strings.Contains(err.Error(), "no value head") {
		t.Fatalf("policy-only checkpoint: %v", err)
	}
	if err := azFrontDoor("bot", "az", &policynet.Model{ValueHidden: 1, Features: policynet.FeaturesMZOppHand}); err == nil || !strings.Contains(err.Error(), "hidden information") {
		t.Fatalf("oracle checkpoint: %v", err)
	}
	if err := azFrontDoor("az", "bot", nil); err != nil {
		t.Fatalf("valid az side: %v", err)
	}
	if azCfg.Search.Kinds != (azmcts.Kinds{Attackers: true}) || azNet != nil {
		t.Fatalf("front door stored kinds %+v net %v", azCfg.Search.Kinds, azNet)
	}
}

// az-redeal needs no -az-world, never opens the clairvoyant gate, and names
// its world in the SpellBench ledger.
func TestAZRedealFrontDoor(t *testing.T) {
	saveAZ(t)
	azWorldArg, azFlagsGiven, azKindsArg = "", false, "priority,attackers,blockers,target"
	if err := azFrontDoor("az-redeal", "bot", nil); err != nil {
		t.Fatalf("az-redeal without -az-world: %v", err)
	}
	if got := azSeatConfig("az-redeal").World; got != azmcts.WorldRedeal {
		t.Fatalf("az-redeal world %q", got)
	}
	azCfg.Search.Sims, azCfg.Worlds = 25, 0
	if got := sbDisplayName("az-redeal"); got != "az-redeal-sims25" {
		t.Fatalf("display name %q", got)
	}
	azCfg.Worlds = 4
	if got := sbDisplayName("az-redeal"); got != "az-redeal-sims25-k4" {
		t.Fatalf("display name %q", got)
	}
	azWorldArg = "redeal"
	if err := azFrontDoor("az", "bot", nil); err != nil {
		t.Fatalf("az -az-world redeal: %v", err)
	}
	if got := azSeatConfig("az").World; got != azmcts.WorldRedeal {
		t.Fatalf("az -az-world redeal seat world %q", got)
	}
	azCfg.Worlds = 0
	azWorldArg = "clairvoyant"
	if err := azFrontDoor("az", "az-redeal", nil); err != nil {
		t.Fatal(err)
	}
	if sbDisplayName("az") != "az-clairvoyant-sims25" || sbDisplayName("az-redeal") != "az-redeal-sims25" {
		t.Fatalf("names %q %q", sbDisplayName("az"), sbDisplayName("az-redeal"))
	}
}

func TestAZCostReportNumbers(t *testing.T) {
	saveAZ(t)
	pri := azmcts.Stats{Searched: 1, Simulations: 10, Completed: 10, EnvSteps: 50, Expanded: 8, Terminal: 1, StepCapped: 1}
	pri.KindSearched[azmcts.KindPriority] = 1
	att := azmcts.Stats{Searched: 1, Simulations: 10, Completed: 9, Panics: 1, EnvSteps: 70, Expanded: 9}
	att.KindSearched[azmcts.KindAttackers] = 1
	tap := azmcts.Stats{Skipped: 1}
	tap.KindSkipped[azmcts.KindPriority][azmcts.SkipFewCandidates] = 1
	tap.PrioritySkipped[azmcts.BaseActivate][azmcts.SkipFewCandidates] = 1
	azStats.mu.Lock()
	azStats.diags = []azmcts.Diag{
		{Turn: 3, Kind: "priority", Searched: true, Candidates: 3, Choice: 0, MS: 100, Stats: pri},
		{Turn: 8, Kind: "attackers", Searched: true, Candidates: 4, Choice: 2, MS: 300, Stats: att},
		{Turn: 14, Kind: "priority", MS: 1, Stats: tap},
		{Stats: azmcts.Stats{FeedStopped: 1}},
	}
	azStats.mu.Unlock()
	out := azCostReport(2)
	for _, want := range []string{
		"az cost report: 3 decisions of a searched kind over 2 games, searched 2 (1.0/game), overrides 1 (50.0% of searched)",
		"ms/searched decision: mean 200.0 p50 100.0 p95 100.0; ms/simulation mean 20.000; simulations/decision mean 10.0; env steps/simulation mean 6.0",
		"  kind priority: asked 2, searched 1, overrides 0, ms mean 100.0 p95 100.0",
		"  kind attackers: asked 1, searched 1, overrides 1, ms mean 300.0 p95 300.0",
		"  kind blockers: none",
		"  kind target: none",
		"  t01-06: searched 1, ms mean 100.0 p95 100.0",
		"  t07-12: searched 1, ms mean 300.0 p95 300.0",
		"  t13+: searched 0",
		"counters: simulations 20, completed 19, chance-failures 0, panics 1, submit-errors 0, bad-worlds 0, no-world 0, all-failed 0, step-capped 1, terminal 1, expanded 17, unavailable 0, prior-fallbacks 0, skipped 1, feed-stopped 1",
		"searched by kind: priority 1, attackers 1, blockers 0, target 0\n",
		"  skipped priority: payment 0, few-candidates 1, translate-error 0\n",
		"  skipped attackers: payment 0, few-candidates 0, translate-error 0\n",
		"  skipped target: payment 0, few-candidates 0, translate-error 0\n",
		"  skipped priority, bot answered cast: payment 0, few-candidates 0, translate-error 0\n",
		"  skipped priority, bot answered activate: payment 0, few-candidates 1, translate-error 0\n",
		"  skipped priority, bot answered other: payment 0, few-candidates 0, translate-error 0\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestAZCostReportEmpty(t *testing.T) {
	saveAZ(t)
	azStats.mu.Lock()
	azStats.diags = nil
	azStats.mu.Unlock()
	if out := azCostReport(3); !strings.Contains(out, "no decisions of a searched kind") {
		t.Fatalf("empty report = %q", out)
	}
}

// End to end through the matrix path: `-a az -az-world clairvoyant -az-sims
// 2 -b bot -pairs <pair> -games 1` exits 0 and the seat reported decisions;
// a checkpoint with no value head is refused at the front door.
func TestAZPlaysPairMatrix(t *testing.T) {
	dir := corpusDirOrSkip(t)
	saveAZ(t)
	azWorldArg = "clairvoyant"
	azCfg.Search.Sims = 2
	code := mainExit("az", "bot", 1, 42, 2, 0, "mono-red-goblins:mono-blue-tempo", "constructed", "text", 0,
		200, 20000, dir, "", false, false, "", 0, 0, "", "", "", "", "")
	if code != 0 {
		t.Fatalf("az pair run exited %d", code)
	}
	azStats.mu.Lock()
	n := len(azStats.diags)
	azStats.mu.Unlock()
	if n == 0 {
		t.Fatal("the az seat reported no decision of a searched kind")
	}
	code = mainExit("az", "bot", 1, 42, 2, 0, "mono-red-goblins:mono-blue-tempo", "constructed", "text", 0,
		200, 20000, dir, "", false, false, "", 0, 0, "", "", "", "", writeZeroCheckpoint(t))
	if code == 0 {
		t.Fatal("an az side accepted a checkpoint without a value head")
	}
}
