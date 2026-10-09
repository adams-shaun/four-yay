package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// oracleCanAttack asserts whether a creature is among the attackers the
// pending declare-attackers decision offers. It is the attack-side twin of
// oracleCanBlock: a legality static (Defender, CantAttack) suppresses the
// option and a suppressed option leaves no other trace.
type oracleCanAttack struct {
	Attacker string `json:"attacker"`
	// Battle, when set, requires the matched option to name the attacked
	// battlefield permanent (a planeswalker ref).
	Battle string `json:"battle,omitempty"`
	// MaxAttackers, when set, additionally asserts the AttackRestrict cap the
	// matched option's Group publishes (0 = no restriction). It is the
	// per-(defender, battle) ceiling read Decision.Validate and botpolicy
	// enforce, so a cap nothing applies fails the assertion.
	MaxAttackers *int `json:"max_attackers,omitempty"`
}

// canAttackFails evaluates a can_attack expectation for the creature id (named
// ref in messages) against the pending decision d. battle is the attacked
// permanent the option must name (0 = any), and max, when non-nil, is the
// AttackRestrict cap the matched option's Group must publish (0 = no group).
func canAttackFails(d *decision.Decision, id state.ObjID, ref string, want bool, battle state.ObjID, max *int) []string {
	if d == nil || d.Kind != decision.KAttackers {
		return []string{"can_attack: no attackers decision pending"}
	}
	found := false
	matched := -1
	for i, o := range d.Options {
		if o.Obj != id || (battle != 0 && o.Battle != battle) {
			continue
		}
		found = true
		if matched < 0 {
			matched = i
		}
	}
	if found != want {
		return []string{fmt.Sprintf("%s can attack = %v, want %v", ref, found, want)}
	}
	if max != nil && matched >= 0 {
		got := 0
		if group := d.Options[matched].Group; group != "" {
			got = d.GroupCapFor(group)
		}
		if got != *max {
			return []string{fmt.Sprintf("%s attack cap = %d, want %d", ref, got, *max)}
		}
	}
	return nil
}
