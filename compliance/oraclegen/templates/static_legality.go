package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// These legality families require a live decision checkpoint. Keep unsupported
// shapes explicit rather than emitting an item whose checkpoint cannot test
// the static; the runner's Expect vocabulary is shared with these templates.
func canAttackDefenderItem(_ *cards.Registry, _ *cards.Face, name string, _ levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static CanAttackDefender requires attack-option snapshot; no matching checkpoint is available"}
}

func cantBlockByItem(_ *cards.Registry, _ *cards.Face, name string, _ levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static CantBlockBy requires a blockers-decision scenario; no validated control fixture is available"}
}

func cantBeCastItem(_ *cards.Registry, _ *cards.Face, name string, _ levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static CantBeCast requires a phase- and condition-specific cast-option control"}
}

func cantBeActivatedItem(_ *cards.Registry, _ *cards.Face, name string, _ levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static CantBeActivated requires a phase-specific activated-ability option control"}
}
