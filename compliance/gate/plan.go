package gate

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Need is what a compliance pass must do for one generated scenario
// (spec 2026-10-03-rules-engine-lasagna-design section 11.3 C1: rerun only
// stale rows).
type Need int

const (
	// Fresh: the committed verdict is for this exact scenario at this
	// XMAGE_REF, it passes, and gorge still meets its frozen expectation.
	// Nothing to do; the gate re-checks it.
	Fresh Need = iota
	// Rediff: the verdict must be recomputed and XMage's result for this
	// exact scenario is already cached, so no Java runs.
	Rediff
	// Replay: the verdict must be recomputed and XMage has to replay it.
	Replay
)

func (n Need) String() string {
	switch n {
	case Fresh:
		return "fresh"
	case Rediff:
		return "rediff"
	}
	return "replay"
}

// PlanItem decides what a pass does for one scenario. A row is stale when
// it is missing, was recorded for another scenario or XMAGE_REF, does not
// pass (diverge, harness, gorge_wrong: a gorge fix may have flipped it), or
// passes but gorge no longer meets its frozen expectation. reason names
// which.
func PlanItem(reg *cards.Registry, it oraclegen.Item, row compliance.VerdictRow, have bool, ref string, cache oraclediff.Cache) (Need, string) {
	sha := ItemSHA(it)
	reason := ""
	switch {
	case !have:
		reason = "no verdict"
	case row.ScenarioSHA != sha:
		reason = "scenario changed"
	case row.XMageRef != ref:
		reason = "xmage_ref changed"
	case row.Status != compliance.StatusAgree && row.Status != compliance.StatusXMageWrong:
		reason = "status " + row.Status
	default:
		if ok, why := StillMeets(reg, it, row); !ok {
			reason = why
		}
	}
	if reason == "" {
		return Fresh, ""
	}
	if _, ok := cache.Get(sha); ok {
		return Rediff, reason
	}
	return Replay, reason
}

// StillMeets reports whether gorge at this head still produces the result a
// passing verdict row froze, and if not, why.
func StillMeets(reg *cards.Registry, it oraclegen.Item, row compliance.VerdictRow) (bool, string) {
	canon, err := GorgeCanon(reg, it)
	if err != nil {
		return false, "gorge replay: " + err.Error()
	}
	if canon != row.CanonSHA {
		return false, "gorge no longer meets the frozen expectation"
	}
	return true, ""
}
