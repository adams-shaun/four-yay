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
	// Defender, when set, names the planeswalker (or battle) permanent the
	// attacker would be declared against: the option must be the attack on
	// that permanent, not merely on its controller. A "can't be attacked"
	// planeswalker (The Aetherspark attached to a creature) leaves the
	// attacker offered against the player and drops only this option.
	Defender string `json:"defender,omitempty"`
	// MaxAttackers, when set, additionally asserts the AttackRestrict cap the
	// matched option's Group publishes (0 = no restriction). It is the
	// per-(defender, battle) ceiling read Decision.Validate and botpolicy
	// enforce, so a cap nothing applies fails the assertion.
	MaxAttackers *int `json:"max_attackers,omitempty"`
}

// oracleCanAttackFails resolves a can_attack expectation's refs against the
// run and evaluates it against the pending decision. A free function over the
// run, like blockersDecision: a method would grow engineSurface.
func oracleCanAttackFails(r *oracleRun, x oracleExpect) []string {
	id, err := r.resolve(x.CanAttack.Attacker)
	if err != nil {
		return []string{err.Error()}
	}
	var defender state.ObjID
	if x.CanAttack.Defender != "" {
		if defender, err = r.resolve(x.CanAttack.Defender); err != nil {
			return []string{err.Error()}
		}
	}
	return canAttackFails(r.e.Pending(), id, defender, x.CanAttack.Attacker, r.wantBool(x), x.CanAttack.MaxAttackers)
}

// canAttackFails evaluates a can_attack expectation for the creature id (named
// ref in messages) against the pending decision d. A non-zero defender
// restricts the match to the option attacking that permanent, and max, when
// non-nil, is the AttackRestrict cap the matched option's Group must publish
// (0 = no group).
func canAttackFails(d *decision.Decision, id, defender state.ObjID, ref string, want bool, max *int) []string {
	if d == nil || d.Kind != decision.KAttackers {
		return []string{"can_attack: no attackers decision pending"}
	}
	found := false
	matched := -1
	for i, o := range d.Options {
		if o.Obj != id || (defender != 0 && o.Battle != defender) {
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
