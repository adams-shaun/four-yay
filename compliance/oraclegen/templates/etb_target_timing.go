package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// queueEntryTriggerTargetsWithCast moves answers for a self-entry Airbend
// trigger to the preceding cast action. XMage can resolve the creature and
// put its ETB trigger on the stack before that action returns; unused answers
// remain queued for the explicit resolve action.
func queueEntryTriggerTargetsWithCast(f *cards.Face, sc oraclegen.Scenario, answers [][]oraclegen.XAnswer) {
	if !hasSelfEntryAirbendTrigger(f) {
		return
	}
	for i := 1; i < len(answers) && i < len(sc.Steps); i++ {
		if sc.Steps[i].Op != "resolve" || sc.Steps[i-1].Op != "cast" {
			continue
		}
		var remain []oraclegen.XAnswer
		for _, answer := range answers[i] {
			if answer.Kind == "target" {
				answers[i-1] = append(answers[i-1], answer)
			} else {
				remain = append(remain, answer)
			}
		}
		answers[i] = remain
	}
}

func hasSelfEntryAirbendTrigger(f *cards.Face) bool {
	for _, trigger := range f.Triggers {
		if trigger.Mode == "ChangesZone" && trigger.Effect != nil && trigger.Effect.API == "Airbend" &&
			strings.EqualFold(trigger.Params["Destination"], "Battlefield") &&
			strings.EqualFold(trigger.Params["ValidCard"], "Card.Self") {
			return true
		}
	}
	return false
}
