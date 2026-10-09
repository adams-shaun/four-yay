package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// SacrificeSub is the level-B sub-family of a "Whenever you sacrifice ..."
// trigger (Sacrificed / SacrificedOnce). Its cause is a spell that makes a
// controller sacrifice (Village Rites, Deadly Dispute), a token maker's
// prelude, or the card's own sacrifice ability, so the recipe in
// compliance/oraclegen/templates is separate from every other event cause.
// Exported so the recipe and this classifier share one spelling.
const SacrificeSub = "trigger.sacrificed"

// sacrificeWords is the CLOSED vocabulary of sacrifice-filter bases the recipe
// can place and sacrifice a victim for: a permanent or creature the controller
// has (Grizzly Bears), an artifact (Ornithopter), a Food/Clue/Treasure token a
// maker prelude creates, and the generic Card/Permanent token bases. This is a
// recognition check, not a blacklist: a base absent from it (Enchantment, a
// graveyard card, a specific subtype) names a victim no probe supplies and
// stays the ordinary trigger-mode gap.
var sacrificeWords = []string{"Card", "Permanent", "Creature", "Artifact", "Food", "Clue", "Treasure"}

// classifySacrificeTrigger recognises a Sacrificed/SacrificedOnce filter whose
// victim an existing p0 probe can place and sacrifice. It runs after
// classifyEventTrigger's own cases, so a shape those classify keeps its
// sub-family. Every unrecognised filter grammar stays a trigger-mode gap.
func classifySacrificeTrigger(t *cards.Trigger) (string, bool) {
	switch t.ModeKind() {
	case cards.TriggerSacrificed, cards.TriggerSacrificedOnce:
	default:
		return "", false
	}
	if !sacrificePlayerShape(t.ParamStr(cards.PKValidPlayer)) {
		return "", false
	}
	if sacrificeFilterSupported(t.ParamStr(cards.PKValidCard)) {
		return SacrificeSub, true
	}
	return "", false
}

// sacrificePlayerShape reports a Sacrificed ValidPlayer$ the sacrifice recipes
// serve: absent, the card's controller, or any player (a player-wide sacrifice
// is caused by p0's own, which still fires a Player-wide trigger).
func sacrificePlayerShape(p string) bool {
	return p == "" || strings.EqualFold(p, "You") || strings.EqualFold(p, "Player")
}

// sacrificeFilterSupported reports whether every alternative of a sacrifice
// filter names a base the recipe can supply. A filter naming the card itself
// is served by the card's own sacrifice ability, whatever else it also names.
func sacrificeFilterSupported(filter string) bool {
	if namesSelf(filter) {
		return true
	}
	any := false
	for _, alt := range strings.Split(filter, ",") {
		base, _, _ := strings.Cut(strings.TrimSpace(alt), ".")
		if !containsFold(sacrificeWords, base) {
			return false
		}
		any = true
	}
	return any
}

// containsFold reports whether want is in words, ignoring case.
func containsFold(words []string, want string) bool {
	for _, w := range words {
		if strings.EqualFold(w, strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
