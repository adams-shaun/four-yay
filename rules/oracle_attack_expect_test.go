package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleAttackExpectPreSubmit pins the attack op's pre-submit expect read
// (agent-20261009T172754Z-e2838eed): a scenario step that declares attackers
// AND asserts expectations evaluates them against the still-pending
// declare-attackers decision, before the declaration goes in. Juggernaut is
// the corpus's must-attack shape; Savannah Lions is the plain probe beside
// it.
func TestOracleAttackExpectPreSubmit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := func(wantRequired bool) oracleScenario {
		wr := wantRequired
		return oracleScenario{
			CR:  []string{"508.1d"},
			Why: "the attack step's expects read the pending attackers decision pre-submit",
			Setup: map[string]oracleSeat{
				"p0": {Battlefield: []string{"Juggernaut", "Savannah Lions"}},
				"p1": {},
			},
			Steps: []oracleStep{
				{
					Op: "attack", Seat: 0, Defender: "p1",
					Attackers: []string{"p0:Juggernaut"},
					Expect: []oracleExpect{
						{AttackRequired: &oracleAttackRequired{Attacker: "p0:Juggernaut"}, Want: &wr},
					},
				},
				{Op: "pass_to", Seat: 0, Step: "declare-blockers", Decision: "blockers"},
			},
		}
	}

	// want=false: the requirement is real, so the flipped assertion fails
	// PRE-submit and no declaration is ever submitted. Prove both: the fail
	// carries the pre-submit marker, and the engine is still sitting at the
	// attackers decision with Juggernaut untapped (the last snapshot is the
	// setup one -- the failed step adds none).
	notRequired := false
	scenario := sc(notRequired)
	fails, transcript, run := runOracleScenario(reg, scenario)
	if len(fails) == 0 {
		t.Fatalf("flipped attack_required expect was not enforced:\ntranscript:\n%s", strings.Join(transcript, "\n"))
	}
	found := false
	for _, f := range fails {
		if strings.Contains(f, "pre-submit (attack)") &&
			strings.Contains(f, "Juggernaut must attack = true, want false") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fail does not carry the pre-submit marker: %v\ntranscript:\n%s", fails, strings.Join(transcript, "\n"))
	}
	if run.e == nil {
		t.Fatalf("harness: no engine after the failed run")
	}
	d := run.e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("after the pre-submit failure the pending decision is %+v, want the untouched attackers decision", d)
	}
	for _, p := range run.snaps[len(run.snaps)-1].Permanents {
		if p.Ref == "p0:Juggernaut" && (p.Tapped || p.Attacking) {
			t.Fatalf("precondition: the declaration went in despite the pre-submit failure: %+v", p)
		}
	}

	// want=true: the requirement holds, the declaration submits, and the
	// scenario replays to the declare-blockers checkpoint with Juggernaut
	// tapped attacking and the probe undeclared.
	scenario = sc(true)
	fails, transcript, run = runOracleScenario(reg, scenario)
	if len(fails) != 0 {
		t.Fatalf("the true expect must not fail:\n%v\ntranscript:\n%s", fails, strings.Join(transcript, "\n"))
	}
	snap := run.snaps[len(run.snaps)-1]
	if snap.Step != "declare-blockers" {
		t.Fatalf("checkpoint step = %q, want declare-blockers", snap.Step)
	}
	jugAttacking, probeHome := false, false
	for _, p := range snap.Permanents {
		switch p.Ref {
		case "p0:Juggernaut":
			if p.Tapped && p.Attacking {
				jugAttacking = true
			}
		case "p0:Savannah Lions":
			if !p.Tapped && !p.Attacking {
				probeHome = true
			}
		}
	}
	if !jugAttacking || !probeHome {
		t.Fatalf("precondition: checkpoint state wrong (Juggernaut tapped attacking=%v probe home=%v): %+v",
			jugAttacking, probeHome, snap.Permanents)
	}
}

// TestOracleAttackExpectPostStepStillChecked pins the per-expect split of the
// attack op's expect handling (agent-20261009T172754Z-e2838eed, review round
// 1): only the decision-dependent expects (attack_required / can_attack) are
// evaluated pre-submit; a board read on the same step (here `tapped:true`,
// true only AFTER the declaration) must still be evaluated on the ordinary
// post-step path, and a WRONG board read must fail there. Before the fix the
// whole step's expect set was skipped once marked pre-checked, so the wrong
// read passed silently.
func TestOracleAttackExpectPostStepStillChecked(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	trueVal := true
	base := func(boardExpect oracleExpect) oracleScenario {
		return oracleScenario{
			CR:  []string{"508.1d"},
			Why: "an attack step's board reads stay on the post-step check",
			Setup: map[string]oracleSeat{
				"p0": {Battlefield: []string{"Juggernaut", "Savannah Lions"}},
				"p1": {},
			},
			Steps: []oracleStep{
				{
					Op: "attack", Seat: 0, Defender: "p1",
					Attackers: []string{"p0:Juggernaut"},
					Expect: []oracleExpect{
						{AttackRequired: &oracleAttackRequired{Attacker: "p0:Juggernaut"}, Want: &trueVal},
						boardExpect,
					},
				},
				{Op: "pass_to", Seat: 0, Step: "declare-blockers", Decision: "blockers"},
			},
		}
	}

	// Correct board read: the declared attacker is tapped post-step. Zero
	// fails proves the post-step read is not blocked by the pre-submit split.
	correct := base(oracleExpect{Card: "p0:Juggernaut", Tapped: &trueVal})
	if fails, transcript, _ := runOracleScenario(reg, correct); len(fails) != 0 {
		t.Fatalf("mixed attack step with a correct board read must pass:\n%v\ntranscript:\n%s",
			fails, strings.Join(transcript, "\n"))
	}

	// Wrong board read: the probe was never declared, so it stays untapped.
	// This must fail on the POST-step path -- if the step were skipped
	// wholesale the wrong read would pass silently (the round-1 regression).
	wrong := base(oracleExpect{Card: "p0:Savannah Lions", Tapped: &trueVal})
	fails, transcript, run := runOracleScenario(reg, wrong)
	if len(fails) == 0 {
		t.Fatalf("a wrong board read on an attack step was not enforced:\ntranscript:\n%s",
			strings.Join(transcript, "\n"))
	}
	found := false
	for _, f := range fails {
		if strings.Contains(f, "after step 0 (attack)") &&
			strings.Contains(f, "Savannah Lions: tapped=false, want true") {
			found = true
		}
	}
	if !found {
		t.Fatalf("wrong board read did not fail on the post-step path: %v\ntranscript:\n%s",
			fails, strings.Join(transcript, "\n"))
	}
	// Precondition: the declaration did go in (the step was not aborted
	// pre-submit), so Juggernaut is tapped attacking at the final checkpoint.
	if run.e == nil {
		t.Fatal("harness: no engine after the run")
	}
	snap := run.snaps[len(run.snaps)-1]
	jugAttacking := false
	for _, p := range snap.Permanents {
		if p.Ref == "p0:Juggernaut" && p.Tapped && p.Attacking {
			jugAttacking = true
		}
	}
	if !jugAttacking {
		t.Fatalf("precondition: declaration did not submit (Juggernaut not tapped attacking): %+v", snap.Permanents)
	}
}
