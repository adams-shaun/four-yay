package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// classifyCastTrigger admits cast and activation causes the level-B recipes
// can construct. The existing event classifier owns loyalty activations and
// the narrow self-cast shape remains covered by level A.
func classifyCastTrigger(f *cards.Face, t *cards.Trigger) (sub string, ok bool) {
	if strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return "", false
	}
	switch t.ModeKind() {
	case cards.TriggerSpellCast:
		if selfCastTrigger(f, t) {
			return "", false
		}
		filter := t.ParamStr(cards.PKValidCard)
		if strings.EqualFold(filter, "Card.Self") {
			return "trigger.spell-cast-self-cast", true
		}
		caster := strings.ToLower(t.ParamStr(cards.PKValidActivatingPlayer))
		switch caster {
		case "", "player", "you":
			return "trigger.spell-cast", true
		case "opponent", "player.opponent", "player.nonactive", "opponent.nonactive":
			return "trigger.spell-cast-opponent", true
		}
	case cards.TriggerCommitCrime:
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") {
			return "trigger.commit-crime", true
		}
	case cards.TriggerAbilityCast:
		validSA := strings.ToLower(t.ParamStr(cards.PKValidSA))
		activatedFilter := strings.HasPrefix(validSA, "activated") || strings.HasPrefix(validSA, "spellability")
		if strings.EqualFold(t.ParamStr(cards.PKValidActivatingPlayer), "You") && activatedFilter &&
			!strings.Contains(validSA, "loyalty") {
			return "trigger.ability-activated", true
		}
	}
	return "", false
}
