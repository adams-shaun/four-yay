package effects

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// answerObjs is the object list a tape-served answer names, in answer order:
// the same Obj != 0 read the object-carrying resume arms ("discard", "dig",
// "reveal_pick") make off the chosen options. The slice is non-nil, so an
// empty answer stays distinguishable from no answer, as on the re-entry.
func answerObjs(ans []decision.Option) []state.ObjID {
	ids := make([]state.ObjID, 0, len(ans))
	for _, o := range ans {
		if o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	return ids
}

// answerYes reports whether a tape-served yes/no answer elected "yes": the
// read every yes/no resume arm makes (option Kind "yes"; an empty or
// malformed answer is the decline).
func answerYes(ans []decision.Option) bool {
	return len(ans) > 0 && ans[0].Kind == "yes"
}
