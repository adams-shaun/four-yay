package mzenc

import (
	"regexp"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// DefaultTableSize is MageZero v0.2's Features.TABLE_SIZE (Integer.MAX_VALUE).
const DefaultTableSize int64 = 2_147_483_647

// actionTypeNames mirrors ActionEncoder.ActionType.toString(), the six
// decision-type feature names (StateEncoder.processState:639).
var actionTypeNames = [6]string{"PRIORITY", "CHOOSE_NUM", "BLANK", "CHOOSE_TARGET", "MAKE_CHOICE", "CHOOSE_USE"}

// stepName maps the projected view step string (state.Step.String()) to the
// upstream TurnStepType name used as a global phase feature. Keys are exactly
// state.Step.String()'s spellings, held there by TestStepNameCoversAllSteps,
// so they cannot drift from the state package. Only the steps StateEncoder
// emits a name for are listed.
var stepName = map[string]string{
	"untap":             "UNTAP",
	"upkeep":            "UPKEEP",
	"draw":              "DRAW",
	"main1":             "MAIN1",
	"begin-combat":      "BEGIN_COMBAT",
	"declare-attackers": "DECLARE_ATTACKERS",
	"declare-blockers":  "DECLARE_BLOCKERS",
	"combat-damage":     "COMBAT_DAMAGE",
	"end-combat":        "END_COMBAT",
	"main2":             "PRECOMBAT_MAIN", // upstream's second-main name (per the plan)
	"end":               "END",
	"cleanup":           "CLEANUP",
}

var (
	uuidTagRE = regexp.MustCompile(` \[[0-9a-f]+\]`)
	angleRE   = regexp.MustCompile("<[^>]*>")
)

// cleanString ports StateEncoder.cleanString (StateEncoder.java:682-689):
// remove " [hex]" UUID tags and every <...> span.
func cleanString(s string) string {
	if s == "" {
		return s
	}
	s = uuidTagRE.ReplaceAllString(s, "")
	return angleRE.ReplaceAllString(s, "")
}

type walker struct {
	e           *Encoder
	seat        state.PlayerID
	names       map[state.ObjID]string
	unsupported map[string]bool
}

// ProcessState walks an omniscient gorGE view and returns the MageZero
// feature-id set for the given decision.
func ProcessState(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) map[int32]struct{} {
	e := NewEncoder(DefaultTableSize)
	w := &walker{e: e, seat: seat, unsupported: map[string]bool{}}
	root := w.e.Root()
	// globals (StateEncoder.java:634-641)
	if name, ok := stepName[v.Step]; ok {
		root.AddFeature(name)
	}
	if decisionType >= 0 && decisionType < len(actionTypeNames) {
		root.AddFeature(actionTypeNames[decisionType])
	}
	root.AddFeature(cleanString(decisionsText))
	return e.IDs()
}
