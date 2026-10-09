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
			// OpponentTurn$ True: the cast must happen during an opponent's
			// turn, which a plain turn-1 cast never satisfies; its own
			// cast-family cause passes to p1's main phase first.
			if strings.EqualFold(t.ParamStr(cards.PKOpponentTurn), "True") {
				return "trigger.spell-cast-opponent-turn", true
			}
			return "trigger.spell-cast", true
		case "player.opponent+active":
			// agent-20261009T174731Z-42d5e0f4: "an opponent casts a spell
			// during their turn" (Unstable Glyphbridge's back face). The
			// engine's player-spec grammar now reads the dotless `Active`
			// clause, so its own cast-family cause passes to p1's main
			// phase and p1 casts there.
			return "trigger.spell-cast-opponent-active", true
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
		// "Whenever an opponent activates a loyalty ability" (Gideon the
		// Oathless): the cause activates a probe planeswalker's plus ability
		// for p1 during p1's main phase, where the AbilityPush's player is
		// the opponent the ValidSA$ +OppCtrl half reads.
		if activatedFilter && strings.Contains(validSA, "loyalty") && strings.Contains(validSA, "oppctrl") {
			return "trigger.ability-activated-opponent", true
		}
	}
	return "", false
}
