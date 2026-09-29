package manabrew

import (
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// dispatch routes one decision onto its per-kind prompt builder. The second
// return value is the MBX-7 fallback flag: true exactly when the prompt is
// the generic chooseFromSelection fallback (an ask mapped by label only,
// answered by native option index) rather than a kind-specific prompt. The
// default arm is still an explicit ErrUnmapped -- the decision.Kind
// universe is closed (decision.Kinds, TestEveryDecisionKindTranslates) and
// every kind is mapped; the fallback class is the KChoose OPTION shapes a
// card can pose, which promptChoose closes with its own fallbackChoose.
func (t *Translator) dispatch(d *decision.Decision, v *view.View) (mb.PromptMessage, bool, error) {
	mapped := func(msg mb.PromptMessage, err error) (mb.PromptMessage, bool, error) { return msg, false, err }
	switch d.Kind {
	case decision.KPriority:
		return mapped(t.promptPriority(d, v))
	case decision.KTarget:
		return mapped(t.promptTarget(d, v))
	case decision.KAttackers:
		return mapped(t.promptCombat(d, v))
	case decision.KBlockers:
		return mapped(t.promptCombat(d, v))
	case decision.KMulligan:
		return mapped(t.promptMulligan(d, v))
	case decision.KModes:
		return mapped(t.promptModes(d, v))
	case decision.KTriggerOrder:
		return mapped(t.promptOrder(d, v))
	case decision.KTriggerOptional:
		return mapped(t.promptMisc(d, v))
	case decision.KCommanderZone:
		return mapped(t.promptMisc(d, v))
	case decision.KChoose:
		return t.promptChoose(d, v)
	case decision.KReplacement:
		return mapped(t.promptMisc(d, v))
	case decision.KArrange:
		return mapped(t.promptArrange(d, v))
	case decision.KStartingPlayer:
		return mapped(t.promptMisc(d, v))
	default:
		return mb.PromptMessage{}, false, ErrUnmapped
	}
}
