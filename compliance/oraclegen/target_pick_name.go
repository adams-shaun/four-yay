// Answer spellings for target picks whose option label names a different
// object than the pick's scenario ref.
//
// The engine renders a target option's label as "<object name> (<controller
// seat>)" (rules/target_legal.go's targetOptionLabel). A pick's ref, however,
// keeps the card's setup identity -- the FRONT face of a transform DFC, the
// printed name of a face-down permanent. When a permanent's current face has
// a different name (Jill, Shiva's Dominant transformed into Shiva, Warden of
// Ice; Inverted Iceberg's back face Iceberg Titan), the front name no longer
// matches any XMage object and the queued answer is left unused. The ref's
// identity alias does match (the permanent keeps its object id through a
// transform), so those picks are answered by their exact ref.
package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// labelNamesRef reports whether target pick k's option label names the same
// object its ref does. An empty label falls back to the ref's name; the
// engine's "<name> (<seat>)" label names the ref's object when its name part
// is the ref's name. A label naming any other object is the object's CURRENT
// name, which the ref's front name cannot select.
func labelNamesRef(d rules.OracleDecision, k int) bool {
	if k < 0 || k >= len(d.Picks) || k >= len(d.PickRefs) {
		return true
	}
	label := d.Picks[k]
	if label == "" {
		return true
	}
	name := oraclediffRefName(d.PickRefs[k])
	if strings.EqualFold(label, name) || label == d.PickRefs[k] {
		return true
	}
	return strings.HasPrefix(label, name+" (")
}

// currentFaceTargetRef reports whether target pick k must be answered by its
// scenario ref instead of the ref's name: the option label names a different
// object (the permanent's current face), the ref is a scenario ref and it is
// exact, so the driver can bind it to the object by identity.
func currentFaceTargetRef(d rules.OracleDecision, k int) bool {
	if k < 0 || k >= len(d.PickRefs) {
		return false
	}
	ref := d.PickRefs[k]
	if !strings.Contains(ref, ":") || strings.HasPrefix(ref, "[") {
		return false
	}
	return !labelNamesRef(d, k) && pickRefExact(d, k)
}

// tapOrUntapChoice is XMage's answer for a TapOrUntap election: the engine's
// option 0 is always the state-changing choice ("untap" when the target is
// tapped, "tap" when it is untapped) and option 1 the no-op, while XMage's
// MayTapOrUntapTargetEffect asks a single chooseUse -- "Untap that
// permanent?" / "Tap that permanent?" -- whose yes takes the state-changing
// action. The pick's option index therefore maps to yes (0) or no (1)
// whatever the target's state.
func tapOrUntapChoice(d rules.OracleDecision) string {
	if len(d.PickIdx) > 0 && d.PickIdx[0] == 0 {
		return "yes"
	}
	return "no"
}
