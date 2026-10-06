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
}

// canAttackFails evaluates a can_attack expectation for the creature id (named
// ref in messages) against the pending decision d.
func canAttackFails(d *decision.Decision, id state.ObjID, ref string, want bool) []string {
	if d == nil || d.Kind != decision.KAttackers {
		return []string{"can_attack: no attackers decision pending"}
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == id {
			found = true
		}
	}
	if found != want {
		return []string{fmt.Sprintf("%s can attack = %v, want %v", ref, found, want)}
	}
	return nil
}
