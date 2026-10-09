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
