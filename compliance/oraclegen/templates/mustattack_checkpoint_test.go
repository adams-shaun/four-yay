package templates

import (
	"testing"
)

// The four level-B MustAttack verdict rows whose pass_to-to-declare-attackers
// checkpoint diverged (agent-20261009T172754Z-e2838eed): XMage's
// checkAttackRequirements force-declares a must-attack creature when its
// engine enters DECLARE_ATTACKERS, so the stored reference showed the card
// tapped attacking where gorge's rules-faithful state (CR 508.1d -- the
// declaration is the active player's act) had it undeclared at the pending
// decision. mustAttackItem now serves the attack-op shape their
// combat#0.attack rows already proved both engines agree on: the attack step
// carries the expects against the PENDING decision (read pre-submit by the
// runner), declares only the required card, and the trailing pass_to stops at
// the declare-blockers checkpoint where both engines show the required
// attacker tapped attacking and the probe undeclared. These tests generate
// each card's item and pin the shape, so all four rows can re-freeze as
// agree on the host's next driver pass.
func TestMustAttackCheckpointDeclares(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Red Herring", "Ares, God of War", "Flamewake Phoenix", "Juggernaut"} {
		t.Run(name, func(t *testing.T) {
			it := staticItemFor(t, reg, name, "static#0.0", "static.must-attack-self")
			self, probe := cardAt(0, name), cardAt(0, smallAttackerProbe)
			atk := -1
			for i := range it.Steps {
				if it.Steps[i].Op == "attack" {
					atk = i
				}
			}
			if atk < 0 {
				t.Fatal("precondition: item carries no attack step")
			}
			canSelf, canProbe, reqSelf, reqProbe := false, false, false, false
			for _, e := range it.Steps[atk].Expect {
				if e.Want == nil {
					continue
				}
				switch {
				case e.CanAttack != nil && e.CanAttack.Attacker == self:
					canSelf = *e.Want
				case e.CanAttack != nil && e.CanAttack.Attacker == probe:
					canProbe = *e.Want
				case e.AttackRequired != nil && e.AttackRequired.Attacker == self:
					reqSelf = *e.Want
				case e.AttackRequired != nil && e.AttackRequired.Attacker == probe:
					reqProbe = !*e.Want
				}
			}
			// Precondition: the scenario replays with zero fails and the card
			// under test sits on p0's battlefield where the requirement is
			// read -- the four expects below would otherwise be vacuous.
			if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not replay with zero fails: %v", res.Fails)
			}
			if !canSelf || !canProbe || !reqSelf || !reqProbe {
				t.Fatalf("precondition: attack step lacks the four expects (canSelf=%v canProbe=%v reqSelf=%v probe-unrequired=%v): %+v",
					canSelf, canProbe, reqSelf, reqProbe, it.Steps[atk].Expect)
			}
			if atk != len(it.Steps)-2 {
				t.Fatalf("precondition: the attack step is not followed by one checkpoint step: steps %d, attack at %d", len(it.Steps), atk)
			}
			cp := it.Steps[len(it.Steps)-1]
			if cp.Op != "pass_to" || cp.Step != "declare-blockers" || cp.Decision != "blockers" {
				t.Fatalf("precondition: trailing checkpoint is %+v, want pass_to declare-blockers blockers", cp)
			}
			// The declare-blockers checkpoint both engines compare: the
			// required card is tapped attacking (the declaration the attack
			// step submitted), the probe is neither, and the game sits at
			// declare-blockers.
			res, ok := runStatic(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not replay with zero fails: %v", res.Fails)
			}
			snap := res.Snapshots[len(res.Snapshots)-1]
			if snap.Step != "declare-blockers" {
				t.Fatalf("checkpoint step = %q, want declare-blockers", snap.Step)
			}
			selfAttacking, probeHome := false, false
			for _, p := range snap.Permanents {
				switch p.Ref {
				case self:
					if p.Tapped && p.Attacking {
						selfAttacking = true
					}
				case probe:
					if !p.Tapped && !p.Attacking {
						probeHome = true
					}
				}
			}
			if !selfAttacking || !probeHome {
				t.Fatalf("precondition: checkpoint state wrong (self tapped attacking=%v probe home=%v): %+v", selfAttacking, probeHome, snap.Permanents)
			}
		})
	}
}
