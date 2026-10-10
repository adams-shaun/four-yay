package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The two level-B templates that pose a setup-drive upkeep-surveil arrange ask
// disagree about XMage's unscripted direction, and only the scenario shape
// separates them: the trigger#0.x template later stops at the same upkeep step
// the setup drive already walked (a pass_to upkeep step), and its stored XMage
// reference KEEPS the looked-at card on top at the setup checkpoint; the
// activate#0.0 template never revisits the phase and its reference graveyards
// the setup ask. D1's empty-set arm therefore applies only to scenarios that
// do NOT revisit the upkeep (oracleRun.revisitsUpkeep).
func TestSetupDriveSurveilTriggerTemplateKeepsOnTop(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Broodheart Engine")
	if !ok {
		t.Fatal("precondition: Broodheart Engine missing from corpus")
	}
	hasSurveilUpkeep := false
	for _, f := range card.Faces {
		for _, tr := range f.Triggers {
			if tr.Effect != nil && tr.Effect.API == "Surveil" {
				hasSurveilUpkeep = true
			}
		}
	}
	if !hasSurveilUpkeep {
		t.Fatal("precondition: Broodheart Engine has no Surveil trigger in the corpus (re-pin this test)")
	}

	// Trigger shape: the scenario walks turn 1's upkeep and later stops at the
	// same upkeep with a pass_to. XMage's driver resolves the upkeep there, so
	// the stored reference kept the looked-at card on top at setup.
	const triggerShape = `{"name":"surveil-trigger","setup":{"p0":{"battlefield":["Broodheart Engine"],"graveyard":["Grizzly Bears"],"library":["Wastes","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"pass_to","seat":0,"step":"upkeep","active":"p0"},{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(triggerShape))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("trigger-shape scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if len(res.Snapshots) == 0 || res.Snapshots[0].Checkpoint != "setup" {
		t.Fatalf("precondition: first snapshot = %+v, want setup", res.Snapshots)
	}
	setup := res.Snapshots[0]
	if setupPermCount(setup, "Broodheart Engine") != 1 {
		t.Fatalf("precondition: Broodheart Engine not on the battlefield at setup: %+v", setup.Permanents)
	}
	line := setupArrangeFallback(res.Transcript)
	if line == "" {
		t.Fatal("precondition: no setup arrange fallback in the transcript — the upkeep surveil never asked; the fixture is wrong")
	}
	if !strings.Contains(line, "-> [\"Wastes\"]") {
		t.Errorf("setup arrange fallback chose %s, want [\"Wastes\"] (keep the looked-at card on top; the trigger-family reference kept it)", line)
	}
	if strings.Contains(line, "-> []") {
		t.Errorf("setup arrange fallback chose the empty set %s, want keep-on-top", line)
	}
	if gy := setup.Players[0].Graveyard; surveilTestContains(gy, "Wastes") {
		t.Errorf("setup p0 graveyard = %v, want no Wastes (the looked-at card must stay on top)", gy)
	}

	// The same trigger shape's SECOND resolution, at the pass_to upkeep's
	// resolve step: the cached XMage reference graveyarded the looked-at card
	// there (the stored rows read `step 1 (resolve) p0.graveyard: gorge "[]",
	// xmage "[Wastes]"`), so the step-time fallback answers the empty set.
	if len(res.Snapshots) != 3 {
		t.Fatalf("precondition: trigger-shape snapshots = %d, want setup + pass_to + resolve", len(res.Snapshots))
	}
	stepLine := arrangeFallback(res.Transcript, "resolve fallback")
	if stepLine == "" {
		t.Fatal("precondition: no resolve arrange fallback in the transcript — the upkeep trigger never asked at step time")
	}
	if !strings.Contains(stepLine, "-> []") {
		t.Errorf("resolve arrange fallback chose %s, want the empty set (the looked-at card to the graveyard)", stepLine)
	}
	if before, after := countName(res.Snapshots[1].Players[0].Graveyard, "Wastes"),
		countName(res.Snapshots[2].Players[0].Graveyard, "Wastes"); after != before+1 {
		t.Errorf("p0 Wastes in graveyard: pass_to %d, resolve %d, want one more at the resolve checkpoint", before, after)
	}

	// Activate shape: no pass_to upkeep step, so the scenario never revisits
	// the phase. D1's empty-set arm still applies; the looked-at card goes to
	// the graveyard at setup, matching the stored activate#0.0 reference.
	const activateShape = `{"name":"surveil-activate","setup":{"p0":{"battlefield":["Broodheart Engine"],"graveyard":["Grizzly Bears"],"library":["Wastes","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`
	res2, err := RunOracleScenarioJSON(reg, []byte(activateShape))
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Fails) != 0 {
		t.Fatalf("activate-shape scenario failed: %v\n%s", res2.Fails, strings.Join(res2.Transcript, "\n"))
	}
	if len(res2.Snapshots) == 0 || res2.Snapshots[0].Checkpoint != "setup" {
		t.Fatalf("precondition: first snapshot = %+v, want setup", res2.Snapshots)
	}
	setup2 := res2.Snapshots[0]
	if setupPermCount(setup2, "Broodheart Engine") != 1 {
		t.Fatalf("precondition: Broodheart Engine not on the battlefield at setup: %+v", setup2.Permanents)
	}
	line2 := setupArrangeFallback(res2.Transcript)
	if line2 == "" {
		t.Fatal("precondition: no setup arrange fallback in the transcript — the upkeep surveil never asked; the fixture is wrong")
	}
	if !strings.Contains(line2, "-> []") {
		t.Errorf("activate-shape setup arrange fallback chose %s, want the empty set (every looked-at card to the graveyard)", line2)
	}
	if gy := setup2.Players[0].Graveyard; !surveilTestContains(gy, "Wastes") {
		t.Errorf("activate-shape setup p0 graveyard = %v, want Wastes (the looked-at card must go to the graveyard)", gy)
	}
}

// setupArrangeFallback returns the one transcript line the setup drive logs
// for an arrange fallback, or "" when none ran.
func setupArrangeFallback(transcript []string) string {
	return arrangeFallback(transcript, "setup fallback")
}

// arrangeFallback returns the first transcript line logging an arrange
// answered by the given fallback tag ("setup fallback", "resolve fallback").
func arrangeFallback(transcript []string, tag string) string {
	for _, l := range transcript {
		if strings.Contains(l, tag) && strings.Contains(l, "arrange") {
			return l
		}
	}
	return ""
}

func countName(xs []string, name string) int {
	n := 0
	for _, x := range xs {
		if x == name {
			n++
		}
	}
	return n
}

func surveilTestContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
