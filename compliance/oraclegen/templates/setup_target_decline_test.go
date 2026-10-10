package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// Summon: Bahamut's combat#0.attack placement resolves chapter I ("Destroy up
// to one target nonland permanent") and queues chapter II's identical ask
// before the setup checkpoint. Gorge declined both by fallback while XMage's
// TestPlayer auto-picked the opposing Grizzly Bears (setup p1.graveyard: gorge
// [], xmage [Grizzly Bears]). The scenario must now script the decline on both
// engines: gorge's setup_answers carry two empty target answers it consumes
// (not the fallback), and xmage_answers[0] carries two setup_target skips the
// driver queues before build().
func TestSetupOptionalTargetDeclineIsScriptedOnBothEngines(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, skip := GenerateB(reg, "Summon: Bahamut", d8Req(t, reg, "Summon: Bahamut", "combat#0.attack"))
	if skip != nil {
		t.Fatalf("Summon: Bahamut combat#0.attack: %s", skip.Reason)
	}

	// Precondition: a FRONT-face Saga placed on the battlefield, so chapter I
	// really fires during the setup drive.
	p0 := it.Scenario.Setup["p0"]
	if len(p0.Battlefield) != 1 || p0.Battlefield[0] != "Summon: Bahamut" || len(p0.BackFace) != 0 {
		t.Fatalf("setup p0 = %+v, want the front face of Summon: Bahamut on the battlefield", p0)
	}

	var declines int
	for _, a := range it.Scenario.SetupAnswers {
		if a.Kind == "target" && len(a.Pick) == 0 {
			declines++
		}
	}
	if declines != 2 {
		t.Errorf("setup_answers = %+v, want two empty target answers (chapter I and the pending chapter II)", it.Scenario.SetupAnswers)
	}

	var skips int
	if len(it.XAnswers) > 0 {
		for _, x := range it.XAnswers[0] {
			if x.Kind == "setup_target" && x.Value == "[target_skip]" && x.Seat == 0 {
				skips++
			}
		}
	}
	if skips != 2 {
		t.Errorf("xmage_answers[0] = %+v, want two seat-0 setup_target [target_skip] answers", it.XAnswers)
	}

	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("scenario does not replay: err=%v fails=%v", err, res.Fails)
	}
	var scripted, fallback int
	for _, d := range res.Decisions {
		if d.Step >= 0 || d.Kind != "target" {
			continue
		}
		switch d.Via {
		case "answer":
			scripted++
		case "setup fallback":
			fallback++
		}
	}
	if scripted != 2 || fallback != 0 {
		t.Errorf("setup target asks: %d scripted, %d fallback; want 2 scripted, 0 fallback", scripted, fallback)
	}
	// Both engines now leave the Bears alone: the decline is the scripted
	// outcome, not an auto-picked destroy.
	snap := res.Snapshots[0]
	if len(snap.Players[1].Graveyard) != 0 {
		t.Errorf("p1 graveyard = %v at setup, want [] (chapter I's optional target declined)", snap.Players[1].Graveyard)
	}
}
