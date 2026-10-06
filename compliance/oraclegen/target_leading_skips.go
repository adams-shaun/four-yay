package oraclegen

import "github.com/adams-shaun/gorge/rules"

// leadingUnposedSkips is the target skips XMage consumes BEFORE a target
// decision's own picks: the "up to N" slots of the same triggered ability
// that gorge settled empty ahead of this, the chain's first posed ask (a root
// "up to one Equipment" slot with no Equipment, Swordsman, Sharp Scoundrel).
// XMage asks each in order and would take the first pick for the empty slot.
func leadingUnposedSkips(d rules.OracleDecision) []XAnswer {
	var as []XAnswer
	for n := 0; n < d.LeadingUnposed; n++ {
		as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
	}
	return as
}
