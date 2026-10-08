package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// TurnedFaceUpSub is the level-B sub-family of a TurnFaceUp trigger whose
// filter names the source card itself ("When this creature is turned face
// up"). TurnedFaceUpOtherSub is the family whose filter names another
// permanent ("Whenever a permanent you control is turned face up"). Both
// share the cause shape -- a disguise cast followed by the battlefield
// turn-face-up special action -- so the recipe in
// compliance/oraclegen/templates is one file for the pair. Exported so the
// recipe and this classifier share one spelling.
const (
	TurnedFaceUpSub      = "trigger.turned-face-up"
	TurnedFaceUpOtherSub = "trigger.turned-face-up-other"
)

// classifyTurnFaceUpTrigger names the two TurnFaceUp sub-families: the source
// itself (Card.Self, or a comma list that includes it) and another permanent
// (Permanent, Permanent.YouCtrl, Detective.YouCtrl, ...). A ValidCause$
// qualifier ("turned face up by a spell or ability you control") needs an
// event-cause match the morph-family turn-face-up special action does not
// carry, so it stays a mode gap. ok is false for every other trigger.
func classifyTurnFaceUpTrigger(t *cards.Trigger) (sub string, ok bool) {
	if t.ModeKind() != cards.TriggerTurnFaceUp {
		return "", false
	}
	if strings.TrimSpace(t.ParamStr(cards.PKValidCause)) != "" {
		return "", false
	}
	v := t.ParamStr(cards.PKValidCard)
	if v == "" {
		// A filter-less TurnFaceUp trigger names the source the way a
		// Card.Self one does; the recipe then reports a named skip if the
		// card carries no morph family.
		return TurnedFaceUpSub, true
	}
	if namesSelf(v) {
		return TurnedFaceUpSub, true
	}
	return TurnedFaceUpOtherSub, true
}
