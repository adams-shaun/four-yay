package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// caseSolvedMatches implements Mode$ CaseSolved ("whenever you solve a
// Case", MKM Case File Auditor -- the corpus's one carrier). A Case becomes
// solved through its own "To solve --" end-step trigger (CR 719.3a), whose
// `AlterAttribute | Attributes$ Solved` body emits the events.AlterAttribute
// grant effects.effAlterAttribute stamps with the solving player; the
// events.Apply fold sets state.Object.Solved (CR 719.3b). This matcher reads
// that grant: ValidCard$ is filtered against the Case (ev.Obj), ValidPlayer$
// against the player who solved it (ev.Player). The effect emits no second
// grant for an already-solved Case, so each Case fires the trigger once.
func caseSolvedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if !events.IsAlterAttribute(ev, "Solved") || ev.Amount < 1 {
		return false
	}
	return eventCardAndPlayerMatch(e, t, source, ev.Obj, ev.Player)
}

func init() {
	registerTrigMatcher(caseSolvedMatches, "CaseSolved")
	effects.RegisterNonAPI("trig:CaseSolved")
}
