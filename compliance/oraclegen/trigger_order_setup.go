package oraclegen

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// xanswers is xanswersSetup without a scenario's setup: a decision recorded
// before step zero is retimed as setup_choice only when the caller knows the
// setup-placed permanents (see triggerOrderSetupPosed), so callers with no
// scenario keep the pre-retiming behaviour. XAnswers and the direct tests use
// this form.
func xanswers(ds []rules.OracleDecision, steps int, modes map[string]int, castSteps map[int]bool) [][]XAnswer {
	return xanswersSetup(ds, steps, modes, castSteps, nil)
}

// triggerOrderSetupPosed reports whether a trigger-order decision is posed by
// XMage during the setup placement, before the recorded step's answers are
// read: gorge recorded it before the first gameplay step (Step < 0) and every
// pick's source is a permanent the scenario's setup put on the battlefield.
// Such an order pends while build() places the seeded permanents, and XMage's
// chooseTriggeredAbility ask fires before any gameplay answer is consumed, so
// the name answers must be queued as setup_choice (xmage_answers[0], before
// build()) or the ask falls to XMage's pending-list fallback and the stack is
// ordered by luck.
//
// A same-source order (two triggers off one permanent) is excluded: a name
// cannot tell the two abilities apart, and the inert rule text is not a match
// for XMage's rule wording, so queuing either as setup_choice only derails the
// as-enters dialogs that share the queue (measured on Ashling, Rekindled and
// Gathering Stone: the setup text answer HARNESSes or diverges). A pick
// without a scenario ref cannot be named either, so it fails the predicate.
func triggerOrderSetupPosed(d rules.OracleDecision, setup map[string]Seat) bool {
	if d.Step >= 0 || d.GorgeKind != "trigger_order" || len(d.Picks) < 2 {
		return false
	}
	if !triggerOrderNamesDistinct(d) {
		return false
	}
	if len(d.PickRefs) < len(d.Picks) {
		return false
	}
	for k := range d.Picks {
		ref := d.PickRefs[k]
		if !isScenarioRefShaped(ref) {
			return false
		}
		seat, ok := setup[ref[:strings.IndexByte(ref, ':')]]
		if !ok || !slices.Contains(seat.Battlefield, oraclediffRefName(ref)) {
			return false
		}
	}
	return true
}
