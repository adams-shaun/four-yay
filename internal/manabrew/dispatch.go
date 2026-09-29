package manabrew

import (
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

func (t *Translator) dispatch(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	switch d.Kind {
	case decision.KPriority:
		return t.promptPriority(d, v)
	case decision.KTarget:
		return t.promptTarget(d, v)
	case decision.KAttackers:
		return t.promptCombat(d, v)
	case decision.KBlockers:
		return t.promptCombat(d, v)
	case decision.KMulligan:
		return t.promptMulligan(d, v)
	case decision.KModes:
		return t.promptModes(d, v)
	case decision.KTriggerOrder:
		return t.promptOrder(d, v)
	case decision.KTriggerOptional:
		return t.promptMisc(d, v)
	case decision.KCommanderZone:
		return t.promptMisc(d, v)
	case decision.KChoose:
		return t.promptChoose(d, v)
	case decision.KReplacement:
		return t.promptMisc(d, v)
	case decision.KArrange:
		return t.promptArrange(d, v)
	case decision.KStartingPlayer:
		return t.promptMisc(d, v)
	default:
		return mb.PromptMessage{}, ErrUnmapped
	}
}
