package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// ManaExpendSub is the level-B sub-family of Mode$ ManaExpend ("Whenever you
// expend N ...", CR-expend): a normal triggered ability that fires when a
// player's per-turn cast expenditure crosses N. Its cause is a probe spell
// whose mana value spends at least N, built by the mana recipe in
// compliance/oraclegen/templates. Exported so the recipe and this classifier
// share one spelling.
const ManaExpendSub = "trigger.mana-expend"

// TapsForManaSub is the level-B sub-family of a Mode$ TapsForMana trigger
// marked Static$ True (CR 605.1b triggered mana ability): tapping a permanent
// for mana makes the source add mana. It resolves off the stack, so its
// observation is the mana pool, not a stack entry. Exported so the recipe and
// this classifier share one spelling.
const TapsForManaSub = "trigger.mana-tap"

// classifyManaTrigger names the mana-trigger shapes a p0-only recipe can
// cause: a "whenever you expend N" trigger (a probe cast spends N) and a
// Static$ True TapsForMana triggered mana ability (a probe tap for mana).
// ok is false for every other trigger, which classifyTrigger then classifies
// as before. The shapes are disjoint from the v1 ones, so running it first
// moves a requirement only between sub-families, never in or out of the
// requirement set.
func classifyManaTrigger(t *cards.Trigger) (sub string, ok bool) {
	switch t.ModeKind() {
	case cards.TriggerManaExpend:
		player := strings.TrimSpace(t.ParamStr(cards.PKPlayer))
		if player == "" || strings.EqualFold(player, "You") {
			return ManaExpendSub, true
		}
	case cards.TriggerTapsForMana:
		if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKStatic)), "True") && manaTapFilterServable(t) {
			return TapsForManaSub, true
		}
	}
	return "", false
}

// manaTapFilterServable reports whether the mana-tap recipe can tap a source
// the trigger's ValidCard$ accepts: a land (Land, Land.Basic), a creature (a
// mana dork), or the land the source is attached to (Card.AttachedBy). An
// artifact-token source has no probe the recipe can place, so it stays a gap.
func manaTapFilterServable(t *cards.Trigger) bool {
	filter := strings.TrimSpace(t.ParamStr(cards.PKValidCard))
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKProduced)), "C") {
		return filterHasToken(filter, "Land")
	}
	return filterHasToken(filter, "Land") || filterHasToken(filter, "AttachedBy") || filterHasToken(filter, "Creature")
}
