package rules

import (
	"github.com/adams-shaun/gorge/decision"
)

// hasTriggerCostPay reports whether d offers the triggered-cost pay election.
// The pay ask is a KChoose whose options carry the trigger_cost_pay kind; the
// decline-only hard-decline shape carries only trigger_cost_decline and so
// does not count as a posed election a second time.
func hasTriggerCostPay(d *decision.Decision) bool {
	if d == nil || d.Kind != decision.KChoose {
		return false
	}
	for _, op := range d.Options {
		if op.Kind == "trigger_cost_pay" {
			return true
		}
	}
	return false
}
